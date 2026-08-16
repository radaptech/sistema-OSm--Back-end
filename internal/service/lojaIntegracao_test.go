package service

import (
	"context"
	"errors"
	"testing"

	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// O que não aparece sem banco: a unicidade por tenant, o ErrNoRows virando
// ErrNaoEncontrado (e não 500) e a recusa de desativar loja com setor ativo.
func TestLojaCrud(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoLojas(pool)

	var tenantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('lojas', 'Empresa Lojas') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	outroTenant := tenantID + 1000

	t.Run("empresa do tenant vem como lista de um item", func(t *testing.T) {
		empresas, err := svc.ListarEmpresas(ctx, tenantID)
		if err != nil {
			t.Fatalf("erro ao listar empresas: %v", err)
		}
		// O select do front itera Empresa[], então tem que ser lista mesmo
		// existindo só uma -- e o id é o próprio tenant.
		if len(empresas) != 1 || empresas[0].Id != tenantID || empresas[0].Nome != "Empresa Lojas" {
			t.Fatalf("empresas = %+v", empresas)
		}
		// Tenant que não existe não é erro de servidor: é lista vazia.
		vazio, err := svc.ListarEmpresas(ctx, outroTenant)
		if err != nil || vazio == nil || len(vazio) != 0 {
			t.Errorf("tenant inexistente devia dar lista vazia: %v %+v", err, vazio)
		}
	})

	t.Run("loja carrega o empresaId = tenant", func(t *testing.T) {
		loja, err := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja Com Empresa"})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		// O front filtra "lojas dessa empresa" comparando loja.empresaId com o
		// value do select -- se vier 0, a lista da tela fica sempre vazia.
		if loja.EmpresaId != tenantID {
			t.Errorf("empresaId = %d, esperado %d", loja.EmpresaId, tenantID)
		}
		obtida, err := svc.ObterLoja(ctx, tenantID, loja.Id)
		if err != nil || obtida.EmpresaId != tenantID {
			t.Errorf("obter perdeu o empresaId: %+v %v", obtida, err)
		}
	})

	t.Run("cadastra devolvendo id e ativa", func(t *testing.T) {
		loja, err := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "  Loja Norte  "})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		// Sem o Id a listagem não serve pra nada: é o value do select e o :id da edição.
		if loja.Id == 0 {
			t.Error("id não voltou")
		}
		if loja.Nome != "Loja Norte" {
			t.Errorf("nome devia vir aparado: %q", loja.Nome)
		}
		if !loja.Ativa {
			t.Error("loja nasce ativa")
		}
	})

	t.Run("nome vazio ou só espaços é recusado", func(t *testing.T) {
		for _, nome := range []string{"", "   ", "\t\n"} {
			if _, err := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: nome}); !errors.Is(err, helper.ErrValidacao) {
				t.Errorf("nome %q: esperado ErrValidacao, veio %v", nome, err)
			}
		}
	})

	t.Run("nome repetido no mesmo tenant é duplicado, em outro tenant passa", func(t *testing.T) {
		if _, err := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja Norte"}); !errors.Is(err, helper.ErrDadoDuplicado) {
			t.Errorf("esperado ErrDadoDuplicado, veio %v", err)
		}
		var vizinho int64
		if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('vizinho', 'Vizinho') RETURNING id`).Scan(&vizinho); err != nil {
			t.Fatalf("erro ao criar empresa vizinha: %v", err)
		}
		if _, err := svc.CadastrarLoja(ctx, vizinho, model.NovaLojaPayload{Nome: "Loja Norte"}); err != nil {
			t.Errorf("mesmo nome em outro tenant devia passar: %v", err)
		}
	})

	t.Run("obter e listar", func(t *testing.T) {
		criada, err := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja Sul"})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}

		obtida, err := svc.ObterLoja(ctx, tenantID, criada.Id)
		if err != nil {
			t.Fatalf("erro ao obter: %v", err)
		}
		if obtida != criada {
			t.Errorf("obter devolveu %+v, esperado %+v", obtida, criada)
		}

		lojas, err := svc.ListarLojas(ctx, tenantID)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(lojas) != 3 { // Com Empresa, Norte e Sul
			t.Errorf("esperado 2 lojas, veio %d: %+v", len(lojas), lojas)
		}
		// Não-nil mesmo sem resultado: o front tipa Loja[] e null quebra o .map.
		vazio, err := svc.ListarLojas(ctx, outroTenant)
		if err != nil || vazio == nil || len(vazio) != 0 {
			t.Errorf("tenant sem loja devia dar slice vazio não-nulo: %v %+v", err, vazio)
		}
	})

	t.Run("id inexistente e id de outro tenant são não encontrado", func(t *testing.T) {
		criada, _ := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja Leste"})

		// ErrNoRows não passa pelo TraduzErroPostgres: sem o guarda, viraria
		// erro genérico e o controller responderia 500 no lugar de 404.
		if _, err := svc.ObterLoja(ctx, tenantID, 999999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("obter inexistente: veio %v", err)
		}
		if _, err := svc.ObterLoja(ctx, outroTenant, criada.Id); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("obter de outro tenant: veio %v", err)
		}
		payload := model.NovaLojaPayload{Nome: "Qualquer"}
		if _, err := svc.AtualizarLoja(ctx, tenantID, 999999, payload); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("atualizar inexistente: veio %v", err)
		}
		if _, err := svc.AtualizarLoja(ctx, outroTenant, criada.Id, payload); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("atualizar de outro tenant: veio %v", err)
		}
		if err := svc.DesativarLoja(ctx, tenantID, 999999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("desativar inexistente: veio %v", err)
		}
	})

	t.Run("atualizar troca o nome e mantém o id", func(t *testing.T) {
		criada, err := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja Oeste"})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		atualizada, err := svc.AtualizarLoja(ctx, tenantID, criada.Id, model.NovaLojaPayload{Nome: " Loja Oeste II "})
		if err != nil {
			t.Fatalf("erro ao atualizar: %v", err)
		}
		if atualizada.Id != criada.Id || atualizada.Nome != "Loja Oeste II" {
			t.Errorf("atualizada = %+v", atualizada)
		}
	})

	t.Run("desativar recusa enquanto houver setor ativo", func(t *testing.T) {
		loja, err := svc.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja Com Setor"})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		var setorID int64
		if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, loja.Id).Scan(&setorID); err != nil {
			t.Fatalf("erro ao criar setor: %v", err)
		}

		if err := svc.DesativarLoja(ctx, tenantID, loja.Id); !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Fatalf("esperado ErrConflitoIntegridade, veio %v", err)
		}
		// A transação inteira volta: a recusa não pode ter desativado a loja.
		aindaAtiva, err := svc.ObterLoja(ctx, tenantID, loja.Id)
		if err != nil || !aindaAtiva.Ativa {
			t.Errorf("loja devia continuar ativa: %+v %v", aindaAtiva, err)
		}

		// Setor fora do caminho, agora passa.
		if _, err := pool.Exec(ctx, `UPDATE setor SET ativo = false WHERE id = $1`, setorID); err != nil {
			t.Fatalf("erro ao desativar setor: %v", err)
		}
		if err := svc.DesativarLoja(ctx, tenantID, loja.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}

		desativada, err := svc.ObterLoja(ctx, tenantID, loja.Id)
		if err != nil {
			t.Fatalf("desativada devia continuar legível: %v", err)
		}
		if desativada.Ativa {
			t.Error("ativa devia ser false")
		}
		// Some da listagem, mas a linha fica (soft delete).
		for _, item := range listarOuFalhar(t, svc, ctx, tenantID) {
			if item.Id == loja.Id {
				t.Errorf("desativada não pode aparecer na listagem: %+v", item)
			}
		}

		if err := svc.DesativarLoja(ctx, tenantID, loja.Id); err != nil {
			t.Errorf("desativar de novo devia ser idempotente: %v", err)
		}
	})
}

func listarOuFalhar(t *testing.T, svc *LojaService, ctx context.Context, tenantID int64) []model.Loja {
	t.Helper()
	lojas, err := svc.ListarLojas(ctx, tenantID)
	if err != nil {
		t.Fatalf("erro ao listar: %v", err)
	}
	return lojas
}
