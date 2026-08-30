package service

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func strPtr(s string) *string { return &s }

// Puro, sem banco nem rede: as duas formas de mensagem que o Gestor recebe.
func TestMontarTexto(t *testing.T) {

	t.Run("com solicitante -- template de pedido humano", func(t *testing.T) {
		texto := montarTexto(DadosNotificacao{
			Alvo: "Forno · PAT-001", Descricao: "Barulho estranho",
			LojaNome: "Loja Norte", SetorNome: "Padaria",
			SolicitanteNome: strPtr("Bruno"),
		})
		if !strings.Contains(texto, "Nova solicitação") {
			t.Errorf("template errado (esperava o de solicitante): %q", texto)
		}
		for _, trecho := range []string{"Loja Norte", "Padaria", "Forno · PAT-001", "Bruno", "Barulho estranho"} {
			if !strings.Contains(texto, trecho) {
				t.Errorf("texto sem %q: %q", trecho, texto)
			}
		}
	})

	t.Run("sem solicitante -- template de preventiva vencida", func(t *testing.T) {
		texto := montarTexto(DadosNotificacao{
			Alvo: "Câmara Fria · PAT-002", Descricao: "Revisão trimestral",
			LojaNome: "Loja Sul", SetorNome: "Açougue",
			SolicitanteNome: nil,
		})
		if !strings.Contains(texto, "Preventiva vencida") {
			t.Errorf("template errado (esperava o de preventiva): %q", texto)
		}
		if strings.Contains(texto, "Solicitante:") {
			t.Errorf("preventiva não tem solicitante, mas o texto menciona um: %q", texto)
		}
	})
}

// Puro, sem banco nem rede: usuario.telefone é texto livre, sem máscara
// nenhuma (ver internal/model/usuarios.go) -- o que chega aqui é qualquer
// coisa que um humano tenha digitado.
func TestNormalizarTelefone(t *testing.T) {

	casos := map[string]string{
		"11999990001":       "5511999990001",
		"(11) 99999-0001":   "5511999990001",
		"11 99999-0001":     "5511999990001",
		"+55 11 99999-0001": "5511999990001",
		"5511999990001":     "5511999990001",
	}

	for entrada, esperado := range casos {
		if got := normalizarTelefone(entrada); got != esperado {
			t.Errorf("normalizarTelefone(%q) = %q, esperado %q", entrada, got, esperado)
		}
	}
}

// evolutionDeTeste confirma a Evolution API alcançável antes do teste de
// integração -- mesmo critério de bancoDeTeste pro Postgres: sem ela
// alcançável, t.Skip (não falha), porque nem todo mundo vai ter o compose
// da Evolution API de pé pra rodar os testes do resto do projeto.
func evolutionDeTeste(t *testing.T) (url, apiKey, instancia string) {
	t.Helper()

	url = os.Getenv("EVOLUTION_API_URL_TESTE")
	if url == "" {
		url = "http://localhost:8092"
	}
	apiKey = "evolution-dev-key-troque-em-producao"
	instancia = "sistema-os-notificacoes"

	cliente := &http.Client{Timeout: 2 * time.Second}
	resp, err := cliente.Get(url)
	if err != nil {
		t.Skipf("Evolution API indisponível em %s (%v) -- suba o compose (evolution-api) ou exporte EVOLUTION_API_URL_TESTE", url, err)
	}
	resp.Body.Close()

	return url, apiKey, instancia
}

// TestNotificarNovaSolicitacao cobre o caminho de verdade: busca os gestores
// do setor (mesma query e mesmos cenários do smoke da tarefa 2) e tenta
// mandar via Evolution API real. Sem WhatsApp pareado ainda (nenhum chip
// comprado), o envio em si falha -- o que este teste prova é que a busca dos
// gestores certos, a montagem da mensagem e a chamada HTTP acontecem e o
// erro volta agregado e identificando quem falhou, não um pânico ou um erro
// genérico.
func TestNotificarNovaSolicitacao(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	url, apiKey, instancia := evolutionDeTeste(t)
	svc := NewRepoNotificacao(pool, url, apiKey, instancia)

	var tenantID, lojaID, setorComGestor, setorSemGestor int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('notificacao', 'Empresa Notificacao') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja A') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	for _, s := range []struct {
		nome string
		dest *int64
	}{{"Padaria", &setorComGestor}, {"Açougue", &setorSemGestor}} {
		if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, $3) RETURNING id`, tenantID, lojaID, s.nome).Scan(s.dest); err != nil {
			t.Fatalf("erro ao criar setor: %v", err)
		}
	}

	var gestorID, escopoID int64
	telefone := "11999990001"
	if err := pool.QueryRow(ctx,
		`INSERT INTO usuario (tenant_id, perfil, nome, email, senha_hash, telefone, ativo)
		 VALUES ($1, 'gestor', 'Gestor Teste', 'gestor@notificacao.com', 'x', $2, true) RETURNING id`,
		tenantID, telefone).Scan(&gestorID); err != nil {
		t.Fatalf("erro ao criar gestor: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO usuario_escopo (usuario_id, loja_id, acesso_total_setores) VALUES ($1, $2, false) RETURNING id`,
		gestorID, lojaID).Scan(&escopoID); err != nil {
		t.Fatalf("erro ao criar escopo: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO usuario_escopo_setor (escopo_id, setor_id) VALUES ($1, $2)`,
		escopoID, setorComGestor); err != nil {
		t.Fatalf("erro ao criar escopo de setor: %v", err)
	}

	t.Run("setor sem gestor nenhum -- nil, sem tentar nada", func(t *testing.T) {
		err := svc.NotificarNovaSolicitacao(ctx, tenantID, setorSemGestor, DadosNotificacao{
			Alvo: "Forno · PAT-001", Descricao: "teste", LojaNome: "Loja A", SetorNome: "Açougue",
			SolicitanteNome: strPtr("Bruno"),
		})
		if err != nil {
			t.Errorf("esperava nil (sem gestor pra notificar), veio: %v", err)
		}
	})

	t.Run("setor com gestor -- tenta enviar, erro identifica quem falhou", func(t *testing.T) {
		err := svc.NotificarNovaSolicitacao(ctx, tenantID, setorComGestor, DadosNotificacao{
			Alvo: "Forno · PAT-001", Descricao: "teste", LojaNome: "Loja A", SetorNome: "Padaria",
			SolicitanteNome: strPtr("Bruno"),
		})
		// Sem WhatsApp pareado (nenhum chip comprado ainda), a Evolution API
		// não consegue enviar de verdade -- o que importa aqui é que o erro
		// existe, não é um pânico, e nomeia o gestor certo (prova que achou o
		// destinatário certo e tentou, não que "deu erro genérico em algum
		// lugar").
		if err == nil {
			t.Fatal("esperava erro (WhatsApp não pareado ainda), veio nil")
		}
		if !strings.Contains(err.Error(), "Gestor Teste") {
			t.Errorf("erro não identifica o gestor que falhou: %v", err)
		}
	})
}
