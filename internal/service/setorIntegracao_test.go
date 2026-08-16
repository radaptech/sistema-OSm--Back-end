package service

import (
	"context"
	"errors"
	"testing"

	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

func TestSetorCrud(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcLoja := NewRepoLojas(pool)
	svc := NewRepoSetor(pool)

	var tenantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('setores', 'Empresa Setores') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	outroTenant := tenantID + 1000

	lojaA, err := svcLoja.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja A"})
	if err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	lojaB, err := svcLoja.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja B"})
	if err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}

	t.Run("cadastra devolvendo a linha gravada, não o payload", func(t *testing.T) {
		setor, err := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "  Padaria  ", LojaId: lojaA.Id})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		// Sem o id nada funciona: é o que o escopo, a máquina e a OS referenciam.
		if setor.Id == 0 || setor.Nome != "Padaria" || setor.LojaId != lojaA.Id || !setor.Ativo {
			t.Fatalf("setor = %+v", setor)
		}
	})

	t.Run("nome vazio é recusado", func(t *testing.T) {
		if _, err := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "   ", LojaId: lojaA.Id}); !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("esperado ErrValidacao, veio %v", err)
		}
	})

	t.Run("mesmo nome em lojas diferentes passa, na mesma loja não", func(t *testing.T) {
		if _, err := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "Padaria", LojaId: lojaB.Id}); err != nil {
			t.Errorf("Padaria na Loja B devia passar: %v", err)
		}
		if _, err := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "Padaria", LojaId: lojaA.Id}); !errors.Is(err, helper.ErrDadoDuplicado) {
			t.Errorf("esperado ErrDadoDuplicado, veio %v", err)
		}
	})

	t.Run("loja inexistente ou de outro tenant é conflito, não 500", func(t *testing.T) {
		if _, err := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "X", LojaId: 999999}); !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("loja inexistente: veio %v", err)
		}
		if _, err := svc.CadastrarSetor(ctx, outroTenant, model.NovoSetorPayload{Nome: "X", LojaId: lojaA.Id}); !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("loja de outro tenant: veio %v", err)
		}
	})

	// O buraco que existia: DesativarLoja exige zero setores ativos, mas nada
	// impedia criar um setor DEPOIS, pendurando setor ativo em loja inativa.
	t.Run("não cria setor em loja desativada", func(t *testing.T) {
		loja, err := svcLoja.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja Fechada"})
		if err != nil {
			t.Fatalf("erro ao criar loja: %v", err)
		}
		if err := svcLoja.DesativarLoja(ctx, tenantID, loja.Id); err != nil {
			t.Fatalf("erro ao desativar loja: %v", err)
		}

		if _, err := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "Órfão", LojaId: loja.Id}); !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Fatalf("esperado ErrConflitoIntegridade, veio %v", err)
		}
		// E a transação voltou: nada foi gravado.
		setores, err := svc.ListarSetores(ctx, tenantID, &loja.Id)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(setores) != 0 {
			t.Errorf("nenhum setor devia ter entrado: %+v", setores)
		}
	})

	t.Run("listar com e sem filtro de loja", func(t *testing.T) {
		daLojaA, err := svc.ListarSetores(ctx, tenantID, &lojaA.Id)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(daLojaA) != 1 || daLojaA[0].LojaId != lojaA.Id {
			t.Errorf("setores da Loja A: %+v", daLojaA)
		}

		todos, err := svc.ListarSetores(ctx, tenantID, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(todos) != 2 { // Padaria na A e na B
			t.Errorf("todos os setores: %+v", todos)
		}

		vazio, err := svc.ListarSetores(ctx, outroTenant, nil)
		if err != nil || vazio == nil || len(vazio) != 0 {
			t.Errorf("tenant sem setor devia dar slice vazio não-nulo: %v %+v", err, vazio)
		}
	})

	t.Run("obter, atualizar e desativar", func(t *testing.T) {
		setor, err := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "Açougue", LojaId: lojaA.Id})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}

		obtido, err := svc.ObterSetor(ctx, tenantID, setor.Id)
		if err != nil || obtido != setor {
			t.Errorf("obter = %+v (%v), esperado %+v", obtido, err, setor)
		}

		// lojaId no payload é ignorado: o setor não muda de loja.
		atualizado, err := svc.AtualizarSetor(ctx, tenantID, setor.Id, model.NovoSetorPayload{Nome: " Açougue Novo ", LojaId: lojaB.Id})
		if err != nil {
			t.Fatalf("erro ao atualizar: %v", err)
		}
		if atualizado.Nome != "Açougue Novo" || atualizado.LojaId != lojaA.Id {
			t.Errorf("atualizado = %+v (lojaId devia continuar %d)", atualizado, lojaA.Id)
		}

		if err := svc.DesativarSetor(ctx, tenantID, setor.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}
		desativado, err := svc.ObterSetor(ctx, tenantID, setor.Id)
		if err != nil || desativado.Ativo {
			t.Errorf("devia estar legível e inativo: %+v %v", desativado, err)
		}
		if err := svc.DesativarSetor(ctx, tenantID, setor.Id); err != nil {
			t.Errorf("desativar de novo devia ser idempotente: %v", err)
		}
	})

	t.Run("id inexistente e de outro tenant são não encontrado", func(t *testing.T) {
		setor, _ := svc.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "Peixaria", LojaId: lojaA.Id})
		payload := model.NovoSetorPayload{Nome: "Qualquer", LojaId: lojaA.Id}

		if _, err := svc.ObterSetor(ctx, tenantID, 999999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("obter inexistente: %v", err)
		}
		if _, err := svc.ObterSetor(ctx, outroTenant, setor.Id); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("obter de outro tenant: %v", err)
		}
		if _, err := svc.AtualizarSetor(ctx, outroTenant, setor.Id, payload); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("atualizar de outro tenant: %v", err)
		}
		if err := svc.DesativarSetor(ctx, outroTenant, setor.Id); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("desativar de outro tenant: %v", err)
		}
	})
}
