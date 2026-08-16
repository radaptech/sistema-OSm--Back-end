package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

// lojaFake varia só o erro, como serviceFake -- e grava o que recebeu, porque
// tenant vindo do lugar errado é o bug que não aparece no status.
type lojaFake struct {
	err      error
	recebido *struct {
		tenantId int64
		id       int64
		nome     string
	}
}

func (s lojaFake) anota(tenantId, id int64, nome string) {
	if s.recebido != nil {
		s.recebido.tenantId, s.recebido.id, s.recebido.nome = tenantId, id, nome
	}
}

func (s lojaFake) CadastrarLoja(_ context.Context, tenantID int64, p model.NovaLojaPayload) (model.Loja, error) {
	s.anota(tenantID, 0, p.Nome)
	return model.Loja{Id: 1, Nome: p.Nome, Ativa: true}, s.err
}

func (s lojaFake) ObterLoja(_ context.Context, tenantId, id int64) (model.Loja, error) {
	s.anota(tenantId, id, "")
	if s.err != nil {
		return model.Loja{}, s.err
	}
	return model.Loja{Id: id, Nome: "Loja Norte", Ativa: true}, nil
}

func (s lojaFake) ListarLojas(_ context.Context, tenantID int64) ([]model.Loja, error) {
	s.anota(tenantID, 0, "")
	if s.err != nil {
		return nil, s.err
	}
	return []model.Loja{}, nil
}

func (s lojaFake) AtualizarLoja(_ context.Context, tenantID, id int64, p model.NovaLojaPayload) (model.Loja, error) {
	s.anota(tenantID, id, p.Nome)
	return model.Loja{Id: id, Nome: p.Nome, Ativa: true}, s.err
}

func (s lojaFake) ListarEmpresas(_ context.Context, tenantID int64) ([]model.Empresa, error) {
	s.anota(tenantID, 0, "")
	if s.err != nil {
		return nil, s.err
	}
	return []model.Empresa{{Id: tenantID, Nome: "Empresa Teste"}}, nil
}

func (s lojaFake) DesativarLoja(_ context.Context, tenantID, id int64) error {
	s.anota(tenantID, id, "")
	return s.err
}

// requisicaoLoja monta o contexto como o router monta: :id na URL e tenant no
// contexto, vindo do token.
func requisicaoLoja(metodo, id, corpo string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, "/lojas/"+id, strings.NewReader(corpo))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	ctx.Set(middleware.UserTenantId, int64(7))
	return w, ctx
}

func TestLojaMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	const corpo = `{"nome":"Loja Norte"}`

	// O 422 do desativar é o caso real "loja ainda tem setor ativo".
	semSetores := fmt.Errorf("%w: a loja ainda tem 3 setor(es) ativo(s), desative-os antes", helper.ErrConflitoIntegridade)

	casos := []struct {
		nome    string
		acao    string
		id      string
		err     error
		status  int
		vazando string
	}{
		{"cadastrar sucesso", "cadastrar", "", nil, http.StatusCreated, ""},
		{"cadastrar nome vazio", "cadastrar", "", fmt.Errorf("%w: nome da loja é obrigatório", helper.ErrValidacao), http.StatusBadRequest, ""},
		{"cadastrar duplicado", "cadastrar", "", helper.ErrDadoDuplicado, http.StatusConflict, ""},
		{"cadastrar interno", "cadastrar", "", fmt.Errorf(`ERROR: duplicate key "uq_loja_tenant_nome" (SQLSTATE 23505)`), http.StatusInternalServerError, "uq_loja_tenant_nome"},

		{"obter sucesso", "obter", "3", nil, http.StatusOK, ""},
		{"obter id malformado", "obter", "abc", nil, http.StatusBadRequest, ""},
		{"obter inexistente", "obter", "999", helper.ErrNaoEncontrado, http.StatusNotFound, ""},

		{"listar sucesso", "listar", "", nil, http.StatusOK, ""},
		{"listar interno", "listar", "", fmt.Errorf("conexão recusada"), http.StatusInternalServerError, ""},

		{"atualizar sucesso", "atualizar", "3", nil, http.StatusOK, ""},
		{"atualizar inexistente é 404", "atualizar", "999", helper.ErrNaoEncontrado, http.StatusNotFound, ""},
		{"atualizar duplicado", "atualizar", "3", helper.ErrDadoDuplicado, http.StatusConflict, ""},
		{"atualizar id malformado", "atualizar", "abc", nil, http.StatusBadRequest, ""},

		{"desativar sucesso", "desativar", "3", nil, http.StatusOK, ""},
		{"desativar inexistente", "desativar", "999", helper.ErrNaoEncontrado, http.StatusNotFound, ""},
		{"desativar com setor ativo", "desativar", "3", semSetores, http.StatusUnprocessableEntity, ""},
		{"desativar interno", "desativar", "3", fmt.Errorf("conexão recusada"), http.StatusInternalServerError, ""},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			ctrl := NewLojaController(lojaFake{err: c.err})

			var w *httptest.ResponseRecorder
			var ctx *gin.Context

			switch c.acao {
			case "cadastrar":
				w, ctx = requisicaoLoja(http.MethodPost, "", corpo)
				ctrl.Cadastrar()(ctx)
			case "obter":
				w, ctx = requisicaoLoja(http.MethodGet, c.id, "")
				ctrl.Obter()(ctx)
			case "listar":
				w, ctx = requisicaoLoja(http.MethodGet, "", "")
				ctrl.Listar()(ctx)
			case "atualizar":
				w, ctx = requisicaoLoja(http.MethodPut, c.id, corpo)
				ctrl.Atualizar()(ctx)
			case "desativar":
				w, ctx = requisicaoLoja(http.MethodDelete, c.id, "")
				ctrl.Desativar()(ctx)
			}

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
			if c.vazando != "" && strings.Contains(w.Body.String(), c.vazando) {
				t.Fatalf("resposta vazou detalhe interno %q: %s", c.vazando, w.Body)
			}
		})
	}

	// A mensagem do 422 diz quantos setores faltam -- é ela que vira o toast
	// no front, então não pode ser trocada por texto genérico.
	t.Run("422 de setor ativo preserva a contagem na mensagem", func(t *testing.T) {
		w, ctx := requisicaoLoja(http.MethodDelete, "3", "")
		NewLojaController(lojaFake{err: semSetores}).Desativar()(ctx)
		if !strings.Contains(w.Body.String(), "3 setor") {
			t.Errorf("mensagem perdeu a contagem: %s", w.Body)
		}
	})

	t.Run("corpo sem nome não chega no service", func(t *testing.T) {
		w, ctx := requisicaoLoja(http.MethodPost, "", `{}`)
		NewLojaController(lojaFake{}).Cadastrar()(ctx)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
		}
	})

	t.Run("tenant vem do token e id da rota", func(t *testing.T) {
		var recebido struct {
			tenantId int64
			id       int64
			nome     string
		}
		w, ctx := requisicaoLoja(http.MethodPut, "42", `{"nome":"Loja Sul"}`)
		// Header de outro tenant não pode influenciar: rota autenticada.
		ctx.Request.Header.Set("X-tenant-ID", "outro")
		NewLojaController(lojaFake{recebido: &recebido}).Atualizar()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, esperado 200", w.Code)
		}
		if recebido.tenantId != 7 || recebido.id != 42 || recebido.nome != "Loja Sul" {
			t.Errorf("service recebeu %+v, esperado {7 42 Loja Sul}", recebido)
		}
	})

	t.Run("sem tenant no contexto é 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/lojas", nil)

		NewLojaController(lojaFake{}).Listar()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
	})
}
