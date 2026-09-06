package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestAbrirSolicitacoesDePreventivasVencidas cobre o job que abre a Solicitação
// e a Ordem de Serviço a partir de preventiva vencida. Integração e não
// unitário porque tudo que pode dar errado aqui é do banco: os CHECKs que
// definem a forma da solicitação automática (ck_origem, ck_solicitacao_alvo), o
// trigger DEFERRABLE que exigia foto até a migration 000005, e o
// aberta_por_id nullable da migration 000008.
//
// ⚠️ Desde a migration 000008 a preventiva NÃO passa mais pela fila do Gestor:
// a solicitação nasce 'Convertida' com a OS junto, atribuída ao técnico que a
// própria preventiva carrega. Os subtestes abaixo trancam as duas metades
// disso -- a forma das linhas e as recusas quando o técnico não serve.
//
// Os subtestes compartilham estado de propósito e rodam em ordem: "não duplica"
// só faz sentido depois de "abre", e as recusas do fim mexem no técnico, o que
// invalidaria os anteriores.
func TestAbrirSolicitacoesDePreventivasVencidas(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoPreventiva(pool)

	// Tenant com duas máquinas -- uma ativa e uma desativada -- para provar que
	// desativar máquina para o job. DesativarMaquina não desativa as preventivas
	// dela, então sem o m.ativa da query a máquina morta abriria solicitação a
	// cada ciclo, para sempre, sem jeito de parar pela API.
	var tenantID, lojaID, setorID, maquinaAtiva, maquinaInativa int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('teste', 'Empresa Teste') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}

	tecnicoPrev := tecnicoParaPreventiva(t, ctx, pool, tenantID)

	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja A') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaID).Scan(&setorID); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}
	for _, m := range []struct {
		patrimonio string
		ativa      bool
		dest       *int64
	}{{"P-001", true, &maquinaAtiva}, {"P-002", false, &maquinaInativa}} {
		if err := pool.QueryRow(ctx,
			`INSERT INTO maquina (tenant_id, setor_id, numero_patrimonio, nome, criticidade, ativa)
			 VALUES ($1, $2, $3, $3, 'Alta', $4) RETURNING id`,
			tenantID, setorID, m.patrimonio, m.ativa).Scan(m.dest); err != nil {
			t.Fatalf("erro ao criar máquina %s: %v", m.patrimonio, err)
		}
	}

	// criarPreventiva insere direto no banco -- o service exige técnico válido
	// e é justamente isso que os dois últimos subtestes precisam furar.
	criarPreventiva := func(descricao string, maquina int64, diasFrente int, ativa bool, tecnico *int64) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO preventiva (tenant_id, maquina_id, descricao, intervalo_dias, proxima_data, ativa, tecnico_id)
			 VALUES ($1, $2, $3, $4, CURRENT_DATE + $5::int, $6, $7) RETURNING id`,
			tenantID, maquina, descricao, intervalo, diasFrente, ativa, tecnico).Scan(&id); err != nil {
			t.Fatalf("erro ao criar preventiva %q: %v", descricao, err)
		}
		return id
	}

	// Só a primeira deve gerar OS. As outras três são cada um dos filtros da
	// query, uma por motivo.
	vencida := criarPreventiva("Troca de óleo", maquinaAtiva, -diasAtraso, true, &tecnicoPrev)
	criarPreventiva("Ainda não venceu", maquinaAtiva, +1, true, &tecnicoPrev)
	criarPreventiva("Vencida mas desabilitada", maquinaAtiva, -diasAtraso, false, &tecnicoPrev)
	criarPreventiva("Vencida em máquina desativada", maquinaInativa, -diasAtraso, true, &tecnicoPrev)

	contar := func(tabela string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+tabela).Scan(&n); err != nil {
			t.Fatalf("erro ao contar %s: %v", tabela, err)
		}
		return n
	}

	t.Run("abre solicitação e OS só para a preventiva vencida de máquina ativa", func(t *testing.T) {
		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err != nil {
			t.Fatalf("job devolveu erro: %v", err)
		}
		if criadas != 1 {
			t.Fatalf("criadas = %d, esperado 1 (futura, desabilitada e de máquina inativa não podem gerar)", criadas)
		}
		if n := contar("solicitacao_os"); n != 1 {
			t.Fatalf("solicitações no banco = %d, esperado 1", n)
		}
		// A metade nova: antes da migration 000008 o job parava na solicitação
		// e a OS só nascia quando o Gestor aprovava.
		if n := contar("ordem_servico"); n != 1 {
			t.Fatalf("ordens de serviço no banco = %d, esperado 1 -- a preventiva não passa mais pelo Gestor", n)
		}
	})

	t.Run("a solicitação nasce Convertida, sem passar pela fila do Gestor", func(t *testing.T) {
		var (
			tipo, status, origem, descricao string
			solicitanteID, itemDescricao    *string
			maquinaID, setorLinha           int64
			preventivaID                    int64
			anexos                          int
		)
		if err := pool.QueryRow(ctx, `
			SELECT s.tipo, s.status, s.origem, s.descricao, s.solicitante_id::text, s.item_descricao,
			       s.maquina_id, s.setor_id, s.preventiva_id,
			       (SELECT count(*) FROM solicitacao_anexo a WHERE a.solicitacao_id = s.id)
			  FROM solicitacao_os s`).
			Scan(&tipo, &status, &origem, &descricao, &solicitanteID, &itemDescricao,
				&maquinaID, &setorLinha, &preventivaID, &anexos); err != nil {
			t.Fatalf("erro ao ler solicitação: %v", err)
		}

		// origem/solicitante_id e tipo/item_descricao não são preferência: são
		// ck_origem e ck_solicitacao_alvo. O banco recusaria qualquer outra
		// combinação -- o que este subteste tranca é o job não ter parado de
		// mandar a combinação certa.
		if tipo != "maquinario" || itemDescricao != nil {
			t.Errorf("tipo = %q, item_descricao = %v; esperado maquinario sem texto livre", tipo, itemDescricao)
		}
		if origem != "preventiva" || solicitanteID != nil {
			t.Errorf("origem = %q, solicitante_id = %v; esperado preventiva sem solicitante", origem, solicitanteID)
		}
		// O coração da mudança: 'Pendente' aqui significaria que a preventiva
		// voltou a esperar aprovação do Gestor.
		if status != "Convertida" {
			t.Errorf("status = %q, esperado Convertida -- a OS nasce junto, sem aprovação", status)
		}
		if maquinaID != maquinaAtiva || preventivaID != vencida {
			t.Errorf("apontou para máquina %d / preventiva %d, esperado %d / %d", maquinaID, preventivaID, maquinaAtiva, vencida)
		}
		// setor_id é NOT NULL e a solicitação não guarda loja -- ela sai via setor.
		if setorLinha != setorID {
			t.Errorf("setor_id = %d, esperado %d (o setor da máquina)", setorLinha, setorID)
		}
		if descricao != "Manutenção preventiva: Troca de óleo" {
			t.Errorf("descricao = %q", descricao)
		}
		// Migration 000005: sem ela o COMMIT falharia aqui, não o INSERT.
		if anexos != 0 {
			t.Errorf("anexos = %d, esperado 0 -- ninguém fotografou nada", anexos)
		}
	})

	t.Run("a OS nasce no técnico da preventiva, urgência Baixa e sem autor", func(t *testing.T) {
		var (
			tipo, urgencia, status string
			tecnicoID              int64
			abertaPor              *int64
			afetaProducao          bool
			solicitacaoID          int64
		)
		if err := pool.QueryRow(ctx, `
			SELECT tipo, urgencia, status, tecnico_id, aberta_por_id, afeta_producao, solicitacao_id
			  FROM ordem_servico`).
			Scan(&tipo, &urgencia, &status, &tecnicoID, &abertaPor, &afetaProducao, &solicitacaoID); err != nil {
			t.Fatalf("erro ao ler ordem de serviço: %v", err)
		}

		if tecnicoID != tecnicoPrev {
			t.Errorf("tecnico_id = %d, esperado %d -- o técnico vem da preventiva, o job não escolhe", tecnicoID, tecnicoPrev)
		}
		// aberta_por_id NULL é o motivo de a coluna ter perdido o NOT NULL na
		// migration 000008: não houve ator, foi a data.
		if abertaPor != nil {
			t.Errorf("aberta_por_id = %v, esperado NULL -- ninguém abriu esta OS", *abertaPor)
		}
		// Preventiva é trabalho planejado com data marcada: se fosse urgente
		// não teria esperado o calendário.
		if urgencia != "Baixa" {
			t.Errorf("urgencia = %q, esperado Baixa", urgencia)
		}
		if tipo != "maquinario" {
			t.Errorf("tipo = %q, esperado maquinario", tipo)
		}
		if status != "Aberta" {
			t.Errorf("status = %q, esperado Aberta -- o Técnico ainda não iniciou", status)
		}
		// Sem Solicitante não há quem marque impacto, então o relógio de
		// máquina parada não roda (a tela escreve "Não se aplica").
		if afetaProducao {
			t.Error("afeta_producao = true, esperado false -- preventiva não tem marcador de impacto")
		}
		// A solicitação continua existindo e sendo a origem: é dela que
		// vw_os_horas tira o início do relógio de parada.
		var origem string
		if err := pool.QueryRow(ctx, `SELECT origem FROM solicitacao_os WHERE id = $1`, solicitacaoID).Scan(&origem); err != nil {
			t.Fatalf("erro ao ler solicitação de origem: %v", err)
		}
		if origem != "preventiva" {
			t.Errorf("a OS aponta para uma solicitação de origem %q", origem)
		}
	})

	t.Run("proxima_data avança a partir da data vencida, não de hoje", func(t *testing.T) {
		var proxima time.Time
		if err := pool.QueryRow(ctx, `SELECT proxima_data FROM preventiva WHERE id = $1`, vencida).Scan(&proxima); err != nil {
			t.Fatalf("erro ao ler proxima_data: %v", err)
		}

		// Vencida há 5 dias com intervalo de 30 vai para hoje+25, não hoje+30:
		// contar a partir de hoje faria um ciclo processado com atraso arrastar
		// todos os seguintes.
		var esperado time.Time
		if err := pool.QueryRow(ctx, `SELECT CURRENT_DATE + $1::int`, intervalo-diasAtraso).Scan(&esperado); err != nil {
			t.Fatalf("erro ao calcular data esperada: %v", err)
		}
		if !proxima.Equal(esperado) {
			t.Errorf("proxima_data = %s, esperado %s (hoje+%d, não hoje+%d)",
				proxima.Format("2006-01-02"), esperado.Format("2006-01-02"), intervalo-diasAtraso, intervalo)
		}
	})

	t.Run("segunda execução não duplica", func(t *testing.T) {
		// Rodar o cron duas vezes (duas réplicas, dois disparos colados) não
		// pode render duas OS. Quem segura é a data já avançada pela execução
		// anterior, relida sob lock em ObterPreventivaVencidaParaAbertura.
		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err != nil {
			t.Fatalf("job devolveu erro na segunda execução: %v", err)
		}
		if criadas != 0 {
			t.Errorf("criadas = %d, esperado 0", criadas)
		}
		if n := contar("ordem_servico"); n != 1 {
			t.Errorf("ordens de serviço = %d, esperado continuar 1", n)
		}
	})

	t.Run("ciclo seguinte abre outra OS", func(t *testing.T) {
		// A preventiva gera uma OS POR CICLO -- e agora sem depender de o
		// Gestor ter resolvido a anterior, que era a trava do modelo antigo.
		// Duas OS abertas da mesma preventiva ao mesmo tempo passaram a ser
		// possíveis: é a consequência assumida de tirar o freio humano.
		if _, err := pool.Exec(ctx, `UPDATE preventiva SET proxima_data = CURRENT_DATE - 1 WHERE id = $1`, vencida); err != nil {
			t.Fatalf("erro ao regredir proxima_data: %v", err)
		}
		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err != nil {
			t.Fatalf("job devolveu erro: %v", err)
		}
		if criadas != 1 {
			t.Errorf("criadas = %d, esperado 1 -- a preventiva gera uma OS por ciclo", criadas)
		}
		if n := contar("ordem_servico"); n != 2 {
			t.Errorf("ordens de serviço = %d, esperado 2", n)
		}
	})

	t.Run("preventiva sem técnico falha alto, não some do laço", func(t *testing.T) {
		// A coluna é nullable só para a migration 000008 não quebrar com dado
		// dentro. Pular a linha em silêncio faria a máquina parar de ser
		// mantida sem ninguém notar; o erro sobe no errors.Join e o cron sai
		// com código != 0.
		orfa := criarPreventiva("Sem técnico", maquinaAtiva, -1, true, nil)

		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err == nil {
			t.Fatal("job não devolveu erro para preventiva sem técnico")
		}
		if !strings.Contains(err.Error(), "não tem técnico responsável") {
			t.Errorf("erro = %v; esperado dizer que falta o técnico", err)
		}
		if criadas != 0 || contar("ordem_servico") != 2 {
			t.Errorf("criadas = %d, OS = %d; esperado 0 e 2 -- nada pode ter sido escrito", criadas, contar("ordem_servico"))
		}

		// Sai do caminho dos próximos subtestes.
		if _, err := pool.Exec(ctx, `UPDATE preventiva SET ativa = false WHERE id = $1`, orfa); err != nil {
			t.Fatalf("erro ao desativar a preventiva órfã: %v", err)
		}
	})

	t.Run("técnico que deixou de ser técnico é recusado", func(t *testing.T) {
		// A FK garante que é usuário do tenant, não que ainda é técnico:
		// AtualizarUsuario promove um técnico a gestor sem tocar nas
		// preventivas dele (e zera area_tecnico_id, por ck_usuario_area_tecnico).
		if _, err := pool.Exec(ctx, `UPDATE usuario SET perfil = 'gestor', area_tecnico_id = NULL WHERE id = $1`, tecnicoPrev); err != nil {
			t.Fatalf("erro ao promover o técnico: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE preventiva SET proxima_data = CURRENT_DATE - 1 WHERE id = $1`, vencida); err != nil {
			t.Fatalf("erro ao regredir proxima_data: %v", err)
		}

		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err == nil {
			t.Fatal("job não devolveu erro para técnico que virou gestor")
		}
		if !strings.Contains(err.Error(), "não é mais um técnico ativo") {
			t.Errorf("erro = %v; esperado dizer que o técnico não serve mais", err)
		}
		if criadas != 0 || contar("ordem_servico") != 2 {
			t.Errorf("criadas = %d, OS = %d; esperado 0 e 2", criadas, contar("ordem_servico"))
		}
	})
}

// intervalo e diasAtraso ficam fora da função porque criarPreventiva (closure)
// e os asserts de proxima_data usam os dois.
const (
	intervalo  = 30
	diasAtraso = 5
)
