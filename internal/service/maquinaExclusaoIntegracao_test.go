package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// TestDesativarReativarExcluirMaquina cobre o card "Ativar/Desativar
// Máquinas": desativar recusa trabalho em aberto, reativar desfaz, e a
// exclusão definitiva só passa com senha e patrimônio -- e quando passa, não
// sobra linha nenhuma do histórico.
func TestDesativarReativarExcluirMaquina(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoMaquinario(pool)
	svcUsuario := NewRepoUsuario(pool)
	svcSolicitacao := NewRepoSolicitacao(pool)
	svcSolicitacao.Notificador = novoNotificadorFake()

	var tenantID, lojaID, setorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('exclusao', 'Empresa Exclusao') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaID).Scan(&setorID); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}

	tecnicoPrev := tecnicoParaPreventiva(t, ctx, pool, tenantID)
	area := "Elétrica"
	admin, err := svcUsuario.CadastrarUsuario(ctx, model.NovoUsuarioPayload{
		Nome: "Ana", Email: "ana@exclusao.com", Perfil: "administrador", Senha: "senha-forte-123",
	}, tenantID)
	if err != nil {
		t.Fatalf("erro ao cadastrar admin: %v", err)
	}
	tecnico, err := svcUsuario.CadastrarUsuario(ctx, model.NovoUsuarioPayload{
		Nome: "Eder", Email: "eder@exclusao.com", Perfil: "tecnico", Senha: "senha-forte-123",
		LojasIds: []int64{lojaID}, Area: &area,
	}, tenantID)
	if err != nil {
		t.Fatalf("erro ao cadastrar técnico: %v", err)
	}

	maquina, err := svc.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
		SetorID: setorID, Criticidade: "Alta", NumeroPatrimonio: "PAT-X", Nome: "Forno",
		Preventivas: []model.PreventivaPayload{{
			TecnicoId: tecnicoPrev, Descricao: "Revisão", IntervaloDias: 30,
			ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
		}},
	})
	if err != nil {
		t.Fatalf("erro ao criar máquina: %v", err)
	}

	// Histórico real: solicitação + OS aberta (com impacto) pelo caminho de
	// produção, e pausa/anexo na mão para cobrir as filhas que a OS direta não cria.
	os, err := svcSolicitacao.CadastrarSolicitacaoDireta(ctx, tenantID, admin.Id, "administrador", model.NovaSolicitacaoDiretaPayload{
		Tipo: "maquinario", MaquinaId: &maquina.Id, Descricao: "Vazando óleo",
		Impactos:                    []string{"Afeta Produção"},
		AberturaOrdemServicoPayload: model.AberturaOrdemServicoPayload{Urgencia: "Alta", TecnicoId: tecnico.Id},
	})
	if err != nil {
		t.Fatalf("erro ao abrir OS: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO os_pausa (ordem_servico_id, status_anterior, motivo) VALUES ($1, 'Aberta', 'peça')`, os.Id); err != nil {
		t.Fatalf("erro ao criar pausa: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO solicitacao_anexo (solicitacao_id, tipo, chave, mime_type, tamanho_bytes) VALUES ($1, 'foto', 'k/anexo.jpg', 'image/jpeg', 1)`, os.SolicitacaoId); err != nil {
		t.Fatalf("erro ao criar anexo: %v", err)
	}

	t.Run("desativar recusa máquina com OS em aberto", func(t *testing.T) {
		if err := svc.DesativarMaquina(ctx, tenantID, maquina.Id); !errors.Is(err, helper.ErrMaquinaEmUso) {
			t.Errorf("esperado ErrMaquinaEmUso, veio %v", err)
		}
	})

	t.Run("histórico conta o que vai junto", func(t *testing.T) {
		h, err := svc.HistoricoMaquina(ctx, tenantID, maquina.Id)
		if err != nil {
			t.Fatalf("erro: %v", err)
		}
		if h.Solicitacoes != 1 || h.OrdensServico != 1 || h.Preventivas != 1 || h.EmAberto != 1 {
			t.Errorf("histórico errado: %+v", h)
		}
	})

	t.Run("reativar volta para a listagem", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE maquina SET ativa = false WHERE id = $1`, maquina.Id); err != nil {
			t.Fatal(err)
		}
		inativas, err := svc.ListarMaquinasInativas(ctx, tenantID)
		if err != nil || len(inativas) != 1 || inativas[0].Id != maquina.Id {
			t.Fatalf("inativas = %+v, err %v", inativas, err)
		}
		if err := svc.ReativarMaquina(ctx, tenantID, maquina.Id); err != nil {
			t.Fatalf("erro ao reativar: %v", err)
		}
		if inativas, _ := svc.ListarMaquinasInativas(ctx, tenantID); len(inativas) != 0 {
			t.Errorf("ainda inativa depois de reativar: %+v", inativas)
		}
		if err := svc.ReativarMaquina(ctx, tenantID, 999999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("id inexistente: esperado ErrNaoEncontrado, veio %v", err)
		}
	})

	t.Run("exclusão recusa senha errada e patrimônio errado", func(t *testing.T) {
		_, _, err := svc.ExcluirMaquinaDefinitivo(ctx, tenantID, admin.Id, maquina.Id, model.ExcluirMaquinaPayload{Senha: "errada", Confirmacao: "PAT-X"})
		if !errors.Is(err, helper.ErrSenhaIncorreta) {
			t.Errorf("senha errada: esperado ErrSenhaIncorreta, veio %v", err)
		}
		_, _, err = svc.ExcluirMaquinaDefinitivo(ctx, tenantID, admin.Id, maquina.Id, model.ExcluirMaquinaPayload{Senha: "senha-forte-123", Confirmacao: "PAT-Y"})
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("patrimônio errado: esperado ErrValidacao, veio %v", err)
		}
	})

	t.Run("exclusão apaga máquina e todo o histórico", func(t *testing.T) {
		_, anexos, err := svc.ExcluirMaquinaDefinitivo(ctx, tenantID, admin.Id, maquina.Id, model.ExcluirMaquinaPayload{Senha: "senha-forte-123", Confirmacao: " PAT-X "})
		if err != nil {
			t.Fatalf("erro ao excluir: %v", err)
		}
		if len(anexos) != 1 || anexos[0] != "k/anexo.jpg" {
			t.Errorf("chaves de anexo = %v", anexos)
		}

		var sobra int
		if err := pool.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM maquina WHERE id = $1)
			     + (SELECT count(*) FROM preventiva WHERE maquina_id = $1)
			     + (SELECT count(*) FROM solicitacao_os WHERE maquina_id = $1)
			     + (SELECT count(*) FROM solicitacao_impacto WHERE solicitacao_id = $2)
			     + (SELECT count(*) FROM solicitacao_anexo WHERE solicitacao_id = $2)
			     + (SELECT count(*) FROM ordem_servico WHERE id = $3)
			     + (SELECT count(*) FROM os_pausa WHERE ordem_servico_id = $3)`,
			maquina.Id, os.SolicitacaoId, os.Id).Scan(&sobra); err != nil {
			t.Fatal(err)
		}
		if sobra != 0 {
			t.Errorf("sobraram %d linhas do histórico", sobra)
		}

		if _, err := svc.ObterMaquina(ctx, tenantID, maquina.Id); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("máquina ainda existe: %v", err)
		}
	})
}
