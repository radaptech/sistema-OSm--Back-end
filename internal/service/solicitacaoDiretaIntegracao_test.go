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

// TestSolicitacaoDireta cobre POST /solicitacoes/direta (origem 'direta',
// migration 000014): Gestor/Administrador abrindo solicitação e OS juntas,
// sem foto, sem fila e sem WhatsApp -- e a regra que veio junto, valendo
// também para o abrir-os da fila: o técnico tem que atender a loja.
func TestSolicitacaoDireta(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svc := NewRepoSolicitacao(pool)
	notificador := novoNotificadorFake()
	svc.Notificador = notificador

	var tenantID, lojaA, lojaB, setorA1, setorA2, setorB1 int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('direta', 'Empresa Direta') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	for _, l := range []struct {
		nome string
		dest *int64
	}{{"Loja A", &lojaA}, {"Loja B", &lojaB}} {
		if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, $2) RETURNING id`, tenantID, l.nome).Scan(l.dest); err != nil {
			t.Fatalf("erro ao criar loja: %v", err)
		}
	}
	for _, s := range []struct {
		loja int64
		nome string
		dest *int64
	}{{lojaA, "Padaria", &setorA1}, {lojaA, "Açougue", &setorA2}, {lojaB, "Padaria", &setorB1}} {
		if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, $3) RETURNING id`, tenantID, s.loja, s.nome).Scan(s.dest); err != nil {
			t.Fatalf("erro ao criar setor: %v", err)
		}
	}

	tecnicoPrev := tecnicoParaPreventiva(t, ctx, pool, tenantID)
	maquinaEm := func(setor int64, patrimonio string) int64 {
		t.Helper()
		m, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
			SetorID: setor, Criticidade: "Alta", NumeroPatrimonio: patrimonio, Nome: "Forno",
			Preventivas: []model.PreventivaPayload{{
				TecnicoId: tecnicoPrev, Descricao: "Revisão", IntervaloDias: 30,
				ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
			}},
		})
		if err != nil {
			t.Fatalf("erro ao criar máquina: %v", err)
		}
		return m.Id
	}
	maquinaA1 := maquinaEm(setorA1, "PAT-A1")
	maquinaA2 := maquinaEm(setorA2, "PAT-A2")
	maquinaB1 := maquinaEm(setorB1, "PAT-B1")

	area := "Elétrica"
	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) model.Usuario {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, "senha-forte-123"
		u, err := svcUsuario.CadastrarUsuario(ctx, p, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
		return u
	}
	admin := cadastrar("Ana", "ana@direta.com", "administrador", model.NovoUsuarioPayload{})
	gestor := cadastrar("Carla", "carla@direta.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA1},
	})
	tecnicoA := cadastrar("Eder", "eder@direta.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, Area: &area,
	})
	tecnicoB := cadastrar("Gil", "gil@direta.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaB}, Area: &area,
	})
	solicitante := cadastrar("Bruno", "bruno@direta.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA1},
	})

	maquinario := func(maquina, tecnico int64) model.NovaSolicitacaoDiretaPayload {
		return model.NovaSolicitacaoDiretaPayload{
			Tipo: "maquinario", MaquinaId: &maquina, Descricao: "Vazando óleo",
			Impactos:                    []string{"Afeta Produção"},
			AberturaOrdemServicoPayload: model.AberturaOrdemServicoPayload{Urgencia: "Alta", TecnicoId: tecnico},
		}
	}
	contarSolicitacoes := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM solicitacao_os WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
			t.Fatalf("erro ao contar: %v", err)
		}
		return n
	}

	t.Run("gestor abre OS de maquinário no escopo, sem foto", func(t *testing.T) {
		os, err := svc.CadastrarSolicitacaoDireta(ctx, tenantID, gestor.Id, "gestor", maquinario(maquinaA1, tecnicoA.Id))
		if err != nil {
			t.Fatalf("erro ao abrir OS direta: %v", err)
		}
		if os.TecnicoId != tecnicoA.Id || os.Urgencia != "Alta" || !os.AfetaProducao || os.StatusExecucao != "Aberta" {
			t.Errorf("OS com forma errada: %+v", os)
		}

		sol, err := svc.ObterSolicitacao(ctx, tenantID, admin.Id, "administrador", os.SolicitacaoId)
		if err != nil {
			t.Fatalf("erro ao reler solicitação: %v", err)
		}
		if sol.Status != "Convertida" || sol.Origem != "direta" || len(sol.Anexos) != 0 {
			t.Errorf("status/origem/anexos = %s/%s/%d, esperado Convertida/direta/0", sol.Status, sol.Origem, len(sol.Anexos))
		}
		if sol.SolicitanteId == nil || *sol.SolicitanteId != gestor.Id {
			t.Errorf("solicitanteId = %v, esperado o gestor (%d)", sol.SolicitanteId, gestor.Id)
		}
	})

	t.Run("não notifica ninguém", func(t *testing.T) {
		select {
		case <-notificador.chamado:
			t.Error("OS direta não deveria disparar WhatsApp")
		case <-time.After(200 * time.Millisecond):
		}
	})

	t.Run("gestor abre reparo escolhendo o setor", func(t *testing.T) {
		item := "Lâmpada queimada"
		os, err := svc.CadastrarSolicitacaoDireta(ctx, tenantID, gestor.Id, "gestor", model.NovaSolicitacaoDiretaPayload{
			Tipo: "reparo", Item: &item, SetorId: &setorA1, Descricao: "Corredor",
			AberturaOrdemServicoPayload: model.AberturaOrdemServicoPayload{Urgencia: "Baixa", TecnicoId: tecnicoA.Id},
		})
		if err != nil {
			t.Fatalf("erro ao abrir reparo direto: %v", err)
		}
		if os.Tipo != "reparo" || os.SetorId != setorA1 {
			t.Errorf("tipo/setor = %s/%d, esperado reparo/%d", os.Tipo, os.SetorId, setorA1)
		}
	})

	t.Run("gestor fora do escopo é recusado e nada fica gravado", func(t *testing.T) {
		antes := contarSolicitacoes()
		_, err := svc.CadastrarSolicitacaoDireta(ctx, tenantID, gestor.Id, "gestor", maquinario(maquinaA2, tecnicoA.Id))
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("erro = %v, esperado ErrValidacao", err)
		}
		if depois := contarSolicitacoes(); depois != antes {
			t.Errorf("solicitações %d -> %d, o rollback devia levar o INSERT junto", antes, depois)
		}
	})

	t.Run("técnico de outra loja é recusado e nada fica gravado", func(t *testing.T) {
		antes := contarSolicitacoes()
		_, err := svc.CadastrarSolicitacaoDireta(ctx, tenantID, gestor.Id, "gestor", maquinario(maquinaA1, tecnicoB.Id))
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("erro = %v, esperado ErrValidacao", err)
		}
		if depois := contarSolicitacoes(); depois != antes {
			t.Errorf("solicitações %d -> %d, o rollback devia levar o INSERT junto", antes, depois)
		}
	})

	t.Run("reparo sem setor é recusado", func(t *testing.T) {
		item := "Vidro"
		_, err := svc.CadastrarSolicitacaoDireta(ctx, tenantID, gestor.Id, "gestor", model.NovaSolicitacaoDiretaPayload{
			Tipo: "reparo", Item: &item, Descricao: "Trincado",
			AberturaOrdemServicoPayload: model.AberturaOrdemServicoPayload{Urgencia: "Baixa", TecnicoId: tecnicoA.Id},
		})
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("erro = %v, esperado ErrValidacao", err)
		}
	})

	t.Run("administrador abre em qualquer loja", func(t *testing.T) {
		os, err := svc.CadastrarSolicitacaoDireta(ctx, tenantID, admin.Id, "administrador", maquinario(maquinaB1, tecnicoB.Id))
		if err != nil {
			t.Fatalf("erro ao abrir OS direta como admin: %v", err)
		}
		if os.LojaId != lojaB {
			t.Errorf("lojaId = %d, esperado %d", os.LojaId, lojaB)
		}
	})

	t.Run("abrir-os da fila também recusa técnico de outra loja", func(t *testing.T) {
		pendente, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoMaquinarioPayload{
			MaquinaId: maquinaA1, Descricao: "Barulho",
			FotoChave: "tenant/1/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 123,
		})
		if err != nil {
			t.Fatalf("erro ao criar solicitação: %v", err)
		}
		_, err = svc.AbrirOS(ctx, tenantID, gestor.Id, "gestor", pendente.Id, model.AberturaOrdemServicoPayload{Urgencia: "Alta", TecnicoId: tecnicoB.Id})
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("erro = %v, esperado ErrValidacao", err)
		}
	})
}
