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

type terceirizadaFake struct {
	err error
	// payload recebido: os campos opcionais chegam como "" e é o service que
	// normaliza -- o controller não pode comer nem alterar isso no caminho.
	recebido *model.NovaEmpresaTerceirizadaPayload
}

func (t terceirizadaFake) resposta() model.EmpresaTerceirizada {
	return model.EmpresaTerceirizada{Id: 1, Nome: "RefriService", Ativa: true}
}

func (t terceirizadaFake) CadastrarEmpresaTerceirizada(_ context.Context, _ int64, p model.NovaEmpresaTerceirizadaPayload) (model.EmpresaTerceirizada, error) {
	if t.recebido != nil {
		*t.recebido = p
	}
	if t.err != nil {
		return model.EmpresaTerceirizada{}, t.err
	}
	return t.resposta(), nil
}

func (t terceirizadaFake) ObterEmpresaTerceirizada(_ context.Context, _, _ int64) (model.EmpresaTerceirizada, error) {
	if t.err != nil {
		return model.EmpresaTerceirizada{}, t.err
	}
	return t.resposta(), nil
}

func (t terceirizadaFake) ListarEmpresasTerceirizadas(_ context.Context, _ int64) ([]model.EmpresaTerceirizada, error) {
	if t.err != nil {
		return nil, t.err
	}
	return []model.EmpresaTerceirizada{}, nil
}

func (t terceirizadaFake) AtualizarEmpresaTerceirizada(_ context.Context, _, _ int64, p model.NovaEmpresaTerceirizadaPayload) (model.EmpresaTerceirizada, error) {
	if t.recebido != nil {
		*t.recebido = p
	}
	if t.err != nil {
		return model.EmpresaTerceirizada{}, t.err
	}
	return t.resposta(), nil
}

func (t terceirizadaFake) DesativarEmpresaTerceirizada(_ context.Context, _, _ int64) error {
	return t.err
}

func requisicaoTerceirizada(metodo, id, corpo string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, "/empresas-terceirizadas/"+id, strings.NewReader(corpo))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	ctx.Set(middleware.UserTenantId, int64(7))
	return w, ctx
}

func TestEmpresaTerceirizadaMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	const corpo = `{"nome":"RefriService","especialidade":"Refrigeração","telefone":"(11) 4002-8922"}`

	casos := []struct {
		nome   string
		acao   string
		id     string
		err    error
		status int
	}{
		{"cadastrar sucesso", "cadastrar", "", nil, http.StatusCreated},
		{"cadastrar nome em branco", "cadastrar", "", fmt.Errorf("%w: nome é obrigatório", helper.ErrValidacao), http.StatusBadRequest},
		{"cadastrar nome duplicado", "cadastrar", "", helper.ErrDadoDuplicado, http.StatusConflict},
		{"cadastrar interno", "cadastrar", "", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},

		{"obter sucesso", "obter", "3", nil, http.StatusOK},
		{"obter id malformado", "obter", "abc", nil, http.StatusBadRequest},
		{"obter inexistente", "obter", "999", helper.ErrNaoEncontrado, http.StatusNotFound},

		{"atualizar sucesso", "atualizar", "3", nil, http.StatusOK},
		{"atualizar inexistente", "atualizar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"atualizar duplicado", "atualizar", "3", helper.ErrDadoDuplicado, http.StatusConflict},

		{"desativar sucesso", "desativar", "3", nil, http.StatusOK},
		{"desativar inexistente", "desativar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"desativar id malformado", "desativar", "abc", nil, http.StatusBadRequest},

		{"listar sucesso", "listar", "", nil, http.StatusOK},
		{"listar interno", "listar", "", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			ctrl := NewEmpresaTerceirizadaController(terceirizadaFake{err: c.err})

			var w *httptest.ResponseRecorder
			var ctx *gin.Context

			switch c.acao {
			case "cadastrar":
				w, ctx = requisicaoTerceirizada(http.MethodPost, "", corpo)
				ctrl.Cadastrar()(ctx)
			case "obter":
				w, ctx = requisicaoTerceirizada(http.MethodGet, c.id, "")
				ctrl.Obter()(ctx)
			case "atualizar":
				w, ctx = requisicaoTerceirizada(http.MethodPut, c.id, corpo)
				ctrl.Atualizar()(ctx)
			case "desativar":
				w, ctx = requisicaoTerceirizada(http.MethodDelete, c.id, "")
				ctrl.Desativar()(ctx)
			case "listar":
				w, ctx = requisicaoTerceirizada(http.MethodGet, "", "")
				ctrl.Listar()(ctx)
			}

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
		})
	}
}

func TestEmpresaTerceirizadaCorpo(t *testing.T) {

	gin.SetMode(gin.TestMode)

	// Os opcionais podem vir vazios ou nem vir -- nenhum dos dois é erro de
	// binding. Quem transforma em NULL é o textoOuNil do service; o controller
	// só não pode barrar antes nem desreferenciar o ponteiro nil no caminho.
	t.Run("opcionais vazios ou ausentes passam", func(t *testing.T) {
		for _, corpo := range []string{
			`{"nome":"RefriService"}`,
			`{"nome":"RefriService","especialidade":"","telefone":""}`,
			`{"nome":"RefriService","especialidade":null,"telefone":null}`,
		} {
			var recebido model.NovaEmpresaTerceirizadaPayload
			w, ctx := requisicaoTerceirizada(http.MethodPost, "", corpo)
			NewEmpresaTerceirizadaController(terceirizadaFake{recebido: &recebido}).Cadastrar()(ctx)

			if w.Code != http.StatusCreated {
				t.Errorf("corpo %s: status = %d, esperado 201 (%s)", corpo, w.Code, w.Body)
			}
			if recebido.Nome != "RefriService" {
				t.Errorf("corpo %s: nome não chegou no service", corpo)
			}
		}
	})

	// O que chega vazio tem que chegar como veio: normalizar aqui esconderia do
	// service a diferença entre "" e ausente, que é dele resolver.
	t.Run("especialidade preenchida chega intacta", func(t *testing.T) {
		var recebido model.NovaEmpresaTerceirizadaPayload
		_, ctx := requisicaoTerceirizada(http.MethodPost, "", `{"nome":"X","especialidade":"Refrigeração"}`)
		NewEmpresaTerceirizadaController(terceirizadaFake{recebido: &recebido}).Cadastrar()(ctx)

		if recebido.Especialidade == nil || *recebido.Especialidade != "Refrigeração" {
			t.Fatalf("especialidade = %v", recebido.Especialidade)
		}
	})

	t.Run("sem nome é 400 e não chega no service", func(t *testing.T) {
		for _, ruim := range []string{`{"especialidade":"Refrigeração"}`, `{}`, `nao sou json`} {
			var recebido model.NovaEmpresaTerceirizadaPayload
			w, ctx := requisicaoTerceirizada(http.MethodPost, "", ruim)
			NewEmpresaTerceirizadaController(terceirizadaFake{recebido: &recebido}).Cadastrar()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Errorf("corpo %s: status = %d, esperado 400", ruim, w.Code)
			}
			if recebido.Nome != "" {
				t.Errorf("corpo %s chegou no service", ruim)
			}
		}
	})

	// Lista vazia sai como [] e não null: o select do Técnico faz .map direto.
	t.Run("lista vazia sai como array", func(t *testing.T) {
		w, ctx := requisicaoTerceirizada(http.MethodGet, "", "")
		NewEmpresaTerceirizadaController(terceirizadaFake{}).Listar()(ctx)

		if corpo := strings.TrimSpace(w.Body.String()); corpo != "[]" {
			t.Errorf("corpo = %s, esperado []", corpo)
		}
	})

	t.Run("sem tenant no contexto é 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/empresas-terceirizadas", nil)

		NewEmpresaTerceirizadaController(terceirizadaFake{}).Listar()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
	})
}
