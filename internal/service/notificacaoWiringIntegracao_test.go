package service

import (
	"context"
	"testing"
	"time"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// notificadorFake grava o que recebeu em vez de bater na Evolution API de
// verdade -- é o próprio motivo de NotificadorInterface existir (ver
// SolicitacaoService.Notificador). `chamado` sincroniza o teste com a
// goroutine: sem isso, o teste terminaria (e o assert rodaria) antes da
// goroutine sequer começar.
type notificadorFake struct {
	chamado           chan struct{}
	tenantId, setorId int64
	dados             DadosNotificacao
}

func novoNotificadorFake() *notificadorFake {
	return &notificadorFake{chamado: make(chan struct{})}
}

func (n *notificadorFake) NotificarNovaSolicitacao(_ context.Context, tenantId, setorId int64, dados DadosNotificacao) error {
	n.tenantId, n.setorId, n.dados = tenantId, setorId, dados
	close(n.chamado)
	return nil
}

func (n *notificadorFake) espera(t *testing.T) {
	t.Helper()
	select {
	case <-n.chamado:
	case <-time.After(2 * time.Second):
		t.Fatal("notificador não foi chamado a tempo -- a goroutine não disparou?")
	}
}

// TestSolicitacaoNotificaAoCriar prova o que a tarefa 4 pluga de verdade: as
// duas criações humanas chamam o Notificador com o setor certo e o texto já
// formatado (Alvo com o patrimônio, SolicitanteNome preenchido) -- e fazem
// isso em goroutine, sem atrasar a resposta (o método já retornou quando o
// teste chama espera()).
func TestSolicitacaoNotificaAoCriar(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svc := NewRepoSolicitacao(pool)

	var tenantID, lojaID, setorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('notif-wiring', 'Empresa Notif Wiring') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja A') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaID).Scan(&setorID); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}

	maquina, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
		SetorID: setorID, Criticidade: "Alta", NumeroPatrimonio: "PAT-77", Nome: "Forno",
		Preventivas: []model.PreventivaPayload{{
			Descricao: "Revisão", IntervaloDias: 30,
			ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
		}},
	})
	if err != nil {
		t.Fatalf("erro ao criar máquina: %v", err)
	}

	solicitante, err := svcUsuario.CadastrarUsuario(ctx, model.NovoUsuarioPayload{
		Nome: "Bruno", Email: "bruno@notif-wiring.com", Perfil: "solicitante", Senha: "senha-forte-123",
		LojasIds: []int64{lojaID}, SetoresIds: []int64{setorID},
	}, tenantID)
	if err != nil {
		t.Fatalf("erro ao criar solicitante: %v", err)
	}

	t.Run("maquinário -- Alvo com patrimônio, solicitante preenchido", func(t *testing.T) {
		fake := novoNotificadorFake()
		svc.Notificador = fake

		_, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoMaquinarioPayload{
			MaquinaId: maquina.Id, Descricao: "Barulho estranho", Impactos: []string{"Afeta Produção"},
			FotoChave: "tenant/x/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 111,
		})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}

		fake.espera(t)

		if fake.tenantId != tenantID || fake.setorId != setorID {
			t.Errorf("tenantId/setorId = %d/%d, esperado %d/%d", fake.tenantId, fake.setorId, tenantID, setorID)
		}
		if fake.dados.Alvo != "Forno · PAT-77" {
			t.Errorf("Alvo = %q, esperado %q", fake.dados.Alvo, "Forno · PAT-77")
		}
		if fake.dados.SolicitanteNome == nil || *fake.dados.SolicitanteNome != "Bruno" {
			t.Errorf("SolicitanteNome = %v, esperado Bruno", fake.dados.SolicitanteNome)
		}
		if fake.dados.LojaNome != "Loja A" || fake.dados.SetorNome != "Padaria" {
			t.Errorf("loja/setor = %q/%q", fake.dados.LojaNome, fake.dados.SetorNome)
		}
	})

	t.Run("reparo -- Alvo é o item, sem máquina", func(t *testing.T) {
		fake := novoNotificadorFake()
		svc.Notificador = fake

		_, err := svc.CadastrarSolicitacaoReparo(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoReparoPayload{
			Item: "Lâmpada queimada", Descricao: "Corredor principal",
			FotoChave: "tenant/x/reparo.jpg", FotoMime: "image/jpeg", FotoTamanho: 22,
		})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}

		fake.espera(t)

		if fake.dados.Alvo != "Lâmpada queimada" {
			t.Errorf("Alvo = %q, esperado o item do reparo", fake.dados.Alvo)
		}
		if fake.dados.SolicitanteNome == nil || *fake.dados.SolicitanteNome != "Bruno" {
			t.Errorf("SolicitanteNome = %v, esperado Bruno", fake.dados.SolicitanteNome)
		}
	})

	t.Run("Notificador nil -- não tenta nada, não panica", func(t *testing.T) {
		svc.Notificador = nil

		_, err := svc.CadastrarSolicitacaoReparo(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoReparoPayload{
			Item: "Torneira pingando", Descricao: "Banheiro",
			FotoChave: "tenant/x/reparo2.jpg", FotoMime: "image/jpeg", FotoTamanho: 33,
		})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
	})
}

// TestPreventivaNotificaAoAbrirSolicitacao é o mesmo critério, do lado do job:
// abrirSolicitacaoDaPreventiva relê a máquina (ListarPreventivasVencidasRow
// não carrega os nomes) e notifica sem SolicitanteNome -- é o que faz o
// texto sair como "preventiva vencida", não "nova solicitação".
func TestPreventivaNotificaAoAbrirSolicitacao(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcMaquina := NewRepoMaquinario(pool)
	svc := NewRepoPreventiva(pool)

	var tenantID, lojaID, setorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('notif-job', 'Empresa Notif Job') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja B') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Açougue') RETURNING id`, tenantID, lojaID).Scan(&setorID); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}

	maquina, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
		SetorID: setorID, Criticidade: "Alta", NumeroPatrimonio: "PAT-88", Nome: "Câmara Fria",
		Preventivas: []model.PreventivaPayload{{
			// Vencida: ontem.
			Descricao: "Revisão trimestral", IntervaloDias: 30,
			ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, -1)), Ativa: true,
		}},
	})
	if err != nil {
		t.Fatalf("erro ao criar máquina: %v", err)
	}

	fake := novoNotificadorFake()
	svc.Notificador = fake

	criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
	if err != nil {
		t.Fatalf("erro no job: %v", err)
	}
	if criadas != 1 {
		t.Fatalf("criadas = %d, esperado 1 (a preventiva de %q)", criadas, maquina.Nome)
	}

	fake.espera(t)

	if fake.tenantId != tenantID || fake.setorId != setorID {
		t.Errorf("tenantId/setorId = %d/%d, esperado %d/%d", fake.tenantId, fake.setorId, tenantID, setorID)
	}
	if fake.dados.Alvo != "Câmara Fria · PAT-88" {
		t.Errorf("Alvo = %q, esperado %q", fake.dados.Alvo, "Câmara Fria · PAT-88")
	}
	if fake.dados.SolicitanteNome != nil {
		t.Errorf("SolicitanteNome deveria ser nil (origem preventiva, ninguém por trás), veio %v", *fake.dados.SolicitanteNome)
	}
	if fake.dados.LojaNome != "Loja B" || fake.dados.SetorNome != "Açougue" {
		t.Errorf("loja/setor = %q/%q", fake.dados.LojaNome, fake.dados.SetorNome)
	}
}
