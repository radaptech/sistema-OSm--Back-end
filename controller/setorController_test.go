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

type setorFake struct {
	err error
	// filtro recebido em ListarSetores: nil = sem ?lojaId=
	lojaId **int64
}

func (s setorFake) CadastrarSetor(_ context.Context, _ int64, p model.NovoSetorPayload) (model.Setor, error) {
	return model.Setor{Id: 1, Nome: p.Nome, LojaId: p.LojaId, Ativo: true}, s.err
}

func (s setorFake) ObterSetor(_ context.Context, _, id int64) (model.Setor, error) {
	if s.err != nil {
		return model.Setor{}, s.err
	}
	return model.Setor{Id: id, Nome: "Padaria", LojaId: 1, Ativo: true}, nil
}

func (s setorFake) ListarSetores(_ context.Context, _ int64, idLoja *int64) ([]model.Setor, error) {
	if s.lojaId != nil {
		*s.lojaId = idLoja
	}
	if s.err != nil {
		return nil, s.err
	}
	return []model.Setor{}, nil
}

func (s setorFake) AtualizarSetor(_ context.Context, _, id int64, p model.NovoSetorPayload) (model.Setor, error) {
	return model.Setor{Id: id, Nome: p.Nome, LojaId: p.LojaId, Ativo: true}, s.err
}

func (s setorFake) DesativarSetor(_ context.Context, _, _ int64) error {
	return s.err
}

func requisicaoSetor(metodo, id, query, corpo string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, "/setores/"+id+query, strings.NewReader(corpo))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	ctx.Set(middleware.UserTenantId, int64(7))
	return w, ctx
}

func TestSetorMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	const corpo = `{"nome":"Padaria","lojaId":1}`

	// O 422 do cadastrar é o caso real "loja desativada", barrado no service.
	lojaFechada := fmt.Errorf("%w: a loja %q está desativada", helper.ErrConflitoIntegridade, "Loja Sul")

	casos := []struct {
		nome   string
		acao   string
		id     string
		err    error
		status int
	}{
		{"cadastrar sucesso", "cadastrar", "", nil, http.StatusCreated},
		{"cadastrar nome vazio", "cadastrar", "", fmt.Errorf("%w: nome da loja é obrigatório", helper.ErrValidacao), http.StatusBadRequest},
		{"cadastrar duplicado na loja", "cadastrar", "", helper.ErrDadoDuplicado, http.StatusConflict},
		{"cadastrar em loja desativada", "cadastrar", "", lojaFechada, http.StatusUnprocessableEntity},
		{"cadastrar interno", "cadastrar", "", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},

		{"obter sucesso", "obter", "3", nil, http.StatusOK},
		{"obter id malformado", "obter", "abc", nil, http.StatusBadRequest},
		{"obter inexistente", "obter", "999", helper.ErrNaoEncontrado, http.StatusNotFound},

		{"atualizar sucesso", "atualizar", "3", nil, http.StatusOK},
		{"atualizar inexistente", "atualizar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"atualizar duplicado", "atualizar", "3", helper.ErrDadoDuplicado, http.StatusConflict},

		{"desativar sucesso", "desativar", "3", nil, http.StatusOK},
		{"desativar inexistente", "desativar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			ctrl := NewSetorController(setorFake{err: c.err})

			var w *httptest.ResponseRecorder
			var ctx *gin.Context

			switch c.acao {
			case "cadastrar":
				w, ctx = requisicaoSetor(http.MethodPost, "", "", corpo)
				ctrl.Cadastrar()(ctx)
			case "obter":
				w, ctx = requisicaoSetor(http.MethodGet, c.id, "", "")
				ctrl.Obter()(ctx)
			case "atualizar":
				w, ctx = requisicaoSetor(http.MethodPut, c.id, "", corpo)
				ctrl.Atualizar()(ctx)
			case "desativar":
				w, ctx = requisicaoSetor(http.MethodDelete, c.id, "", "")
				ctrl.Desativar()(ctx)
			}

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
		})
	}

	// A mensagem do 422 diz qual loja está fechada -- vira o toast no front.
	t.Run("422 preserva o motivo", func(t *testing.T) {
		w, ctx := requisicaoSetor(http.MethodPost, "", "", corpo)
		NewSetorController(setorFake{err: lojaFechada}).Cadastrar()(ctx)
		if !strings.Contains(w.Body.String(), "desativada") {
			t.Errorf("mensagem perdeu o motivo: %s", w.Body)
		}
	})

	t.Run("corpo sem lojaId não chega no service", func(t *testing.T) {
		for _, ruim := range []string{`{"nome":"Padaria"}`, `{"nome":"Padaria","lojaId":0}`, `{"lojaId":1}`} {
			w, ctx := requisicaoSetor(http.MethodPost, "", "", ruim)
			NewSetorController(setorFake{}).Cadastrar()(ctx)
			if w.Code != http.StatusBadRequest {
				t.Errorf("corpo %s: status = %d, esperado 400", ruim, w.Code)
			}
		}
	})
}

// ?lojaId= é o que faz o select em cascata mostrar só os setores da loja
// escolhida: se chegar nil no service, o select lista o tenant inteiro.
func TestSetorListarFiltroDeLoja(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("sem filtro chega nil", func(t *testing.T) {
		var recebido *int64
		w, ctx := requisicaoSetor(http.MethodGet, "", "", "")
		NewSetorController(setorFake{lojaId: &recebido}).Listar()(ctx)

		if w.Code != http.StatusOK || recebido != nil {
			t.Fatalf("status %d, lojaId %v (esperado 200 e nil)", w.Code, recebido)
		}
	})

	t.Run("lojaId vazio é o mesmo que ausente", func(t *testing.T) {
		var recebido *int64
		w, ctx := requisicaoSetor(http.MethodGet, "", "?lojaId=", "")
		NewSetorController(setorFake{lojaId: &recebido}).Listar()(ctx)

		if w.Code != http.StatusOK || recebido != nil {
			t.Fatalf("status %d, lojaId %v (esperado 200 e nil)", w.Code, recebido)
		}
	})

	t.Run("lojaId válido chega no service", func(t *testing.T) {
		var recebido *int64
		w, ctx := requisicaoSetor(http.MethodGet, "", "?lojaId=42", "")
		NewSetorController(setorFake{lojaId: &recebido}).Listar()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if recebido == nil || *recebido != 42 {
			t.Fatalf("lojaId = %v, esperado 42", recebido)
		}
	})

	t.Run("lojaId inválido é 400 e não chega no service", func(t *testing.T) {
		for _, ruim := range []string{"abc", "0", "-3"} {
			var recebido *int64
			w, ctx := requisicaoSetor(http.MethodGet, "", "?lojaId="+ruim, "")
			NewSetorController(setorFake{lojaId: &recebido}).Listar()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Errorf("lojaId=%s: status = %d, esperado 400", ruim, w.Code)
			}
			// Filtro recusado não pode ter ido ao banco.
			if recebido != nil {
				t.Errorf("lojaId=%s chegou no service: %v", ruim, *recebido)
			}
		}
	})

	t.Run("sem tenant no contexto é 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/setores", nil)

		NewSetorController(setorFake{}).Listar()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
	})
}
