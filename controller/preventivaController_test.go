package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

type preventivaFake struct {
	err       error
	recebido  *model.PreventivaPayload
	maquinaId **int64
	ator      *string
}

func (p preventivaFake) resposta(id int64) model.Preventiva {
	return model.Preventiva{
		Id: id, MaquinaId: 3, MaquinaNome: "Forno", Descricao: "Limpeza",
		IntervaloDias: 30, ProximaData: config.NewDataBrPtr(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		Ativa: true, SetorId: 9, SetorNome: "Padaria", LojaId: 2, LojaNome: "Loja Sul",
	}
}

func (p preventivaFake) CadastrarPreventiva(_ context.Context, _ int64, payload model.PreventivaPayload) (model.Preventiva, error) {
	if p.recebido != nil {
		*p.recebido = payload
	}
	if p.err != nil {
		return model.Preventiva{}, p.err
	}
	return p.resposta(1), nil
}

func (p preventivaFake) ListarPreventivas(_ context.Context, _, usuarioID int64, perfil string, maquinaID *int64) ([]model.Preventiva, error) {
	if p.ator != nil {
		*p.ator = fmt.Sprintf("%d/%s", usuarioID, perfil)
	}
	if p.maquinaId != nil {
		*p.maquinaId = maquinaID
	}
	if p.err != nil {
		return nil, p.err
	}
	return []model.Preventiva{}, nil
}

func (p preventivaFake) ObterPreventiva(_ context.Context, _, id int64) (model.Preventiva, error) {
	if p.err != nil {
		return model.Preventiva{}, p.err
	}
	return p.resposta(id), nil
}

func (p preventivaFake) AtualizarPreventiva(_ context.Context, _, id int64, payload model.PreventivaPayload) (model.Preventiva, error) {
	if p.recebido != nil {
		*p.recebido = payload
	}
	if p.err != nil {
		return model.Preventiva{}, p.err
	}
	return p.resposta(id), nil
}

func (p preventivaFake) DesativarPreventiva(_ context.Context, _, _ int64) error {
	return p.err
}

const corpoPreventiva = `{"maquinaId":3,"descricao":"Limpeza","intervaloDias":30,"proximaData":"01/09/2026","ativa":true}`

func requisicaoPreventiva(metodo, id, query, corpo string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, "/preventivas/"+id+query, strings.NewReader(corpo))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	ctx.Set(middleware.UserTenantId, int64(7))
	// Ator autenticado por padrão: toda rota daqui roda atrás do AutenticacaoJwt.
	// Quem testa a ausência das claims monta o contexto na mão.
	ctx.Set(middleware.UserId, int64(1))
	ctx.Set(middleware.UserPerfil, "administrador")
	return w, ctx
}

func TestPreventivaMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	// O 422 real: maquinaId que existe como número mas não neste tenant.
	maquinaDeFora := fmt.Errorf("%w: máquina %d não existe neste tenant", helper.ErrConflitoIntegridade, 9999)
	descricaoVazia := fmt.Errorf("%w: a descrição da preventiva não pode ficar em branco", helper.ErrValidacao)

	casos := []struct {
		nome   string
		acao   string
		id     string
		err    error
		status int
	}{
		{"cadastrar sucesso", "cadastrar", "", nil, http.StatusCreated},
		{"cadastrar descrição em branco", "cadastrar", "", descricaoVazia, http.StatusBadRequest},
		{"cadastrar máquina de outro tenant", "cadastrar", "", maquinaDeFora, http.StatusUnprocessableEntity},
		{"cadastrar interno", "cadastrar", "", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},

		{"obter sucesso", "obter", "3", nil, http.StatusOK},
		{"obter id malformado", "obter", "abc", nil, http.StatusBadRequest},
		{"obter inexistente", "obter", "999", helper.ErrNaoEncontrado, http.StatusNotFound},

		{"atualizar sucesso", "atualizar", "3", nil, http.StatusOK},
		{"atualizar inexistente", "atualizar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"atualizar descrição em branco", "atualizar", "3", descricaoVazia, http.StatusBadRequest},

		{"desativar sucesso", "desativar", "3", nil, http.StatusOK},
		{"desativar inexistente", "desativar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"desativar id malformado", "desativar", "abc", nil, http.StatusBadRequest},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			ctrl := NewPreventivaController(preventivaFake{err: c.err})

			var w *httptest.ResponseRecorder
			var ctx *gin.Context

			switch c.acao {
			case "cadastrar":
				w, ctx = requisicaoPreventiva(http.MethodPost, "", "", corpoPreventiva)
				ctrl.Cadastrar()(ctx)
			case "obter":
				w, ctx = requisicaoPreventiva(http.MethodGet, c.id, "", "")
				ctrl.Obter()(ctx)
			case "atualizar":
				w, ctx = requisicaoPreventiva(http.MethodPut, c.id, "", corpoPreventiva)
				ctrl.Atualizar()(ctx)
			case "desativar":
				w, ctx = requisicaoPreventiva(http.MethodDelete, c.id, "", "")
				ctrl.Desativar()(ctx)
			}

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
		})
	}
}

// maquinaId não pode virar binding:"required" na struct: PreventivaPayload é a
// mesma dos itens que viajam dentro de POST /maquinas, onde o campo é ignorado
// (a máquina ainda não tem id e o front manda 0). Por isso o cheque vive no
// controller, e é aqui que ele fica trancado.
func TestPreventivaMaquinaIdObrigatorioSoNoPost(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("POST sem maquinaId é 400 e não vai ao banco", func(t *testing.T) {
		for _, ruim := range []string{
			`{"descricao":"Limpeza","intervaloDias":30,"proximaData":"01/09/2026"}`,
			`{"maquinaId":0,"descricao":"Limpeza","intervaloDias":30,"proximaData":"01/09/2026"}`,
			`{"maquinaId":-3,"descricao":"Limpeza","intervaloDias":30,"proximaData":"01/09/2026"}`,
		} {
			var recebido model.PreventivaPayload
			w, ctx := requisicaoPreventiva(http.MethodPost, "", "", ruim)
			NewPreventivaController(preventivaFake{recebido: &recebido}).Cadastrar()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Errorf("corpo %s: status = %d, esperado 400", ruim, w.Code)
			}
			if recebido.Descricao != "" {
				t.Errorf("corpo %s chegou no service", ruim)
			}
		}
	})

	// No PUT o campo é ignorado pelo service (mover a preventiva de máquina
	// deixaria as solicitações já geradas apontando para outra), então a
	// ausência dele não pode virar 400.
	t.Run("PUT sem maquinaId passa", func(t *testing.T) {
		w, ctx := requisicaoPreventiva(http.MethodPut, "3", "",
			`{"descricao":"Limpeza","intervaloDias":30,"proximaData":"01/09/2026"}`)
		NewPreventivaController(preventivaFake{}).Atualizar()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, esperado 200 (corpo: %s)", w.Code, w.Body)
		}
	})
}

// Estes três são barrados pelo binding do Gin (corpoJSON) e pelo UnmarshalJSON
// do config.DataBr, antes de qualquer ida ao banco.
func TestPreventivaCorpoInvalido(t *testing.T) {

	gin.SetMode(gin.TestMode)

	casos := map[string]string{
		"sem descrição":   `{"maquinaId":3,"intervaloDias":30,"proximaData":"01/09/2026"}`,
		"intervalo zero":  `{"maquinaId":3,"descricao":"Limpeza","intervaloDias":0,"proximaData":"01/09/2026"}`,
		"sem proximaData": `{"maquinaId":3,"descricao":"Limpeza","intervaloDias":30}`,
		"data em ISO":     `{"maquinaId":3,"descricao":"Limpeza","intervaloDias":30,"proximaData":"2026-09-01"}`,
		"não é json":      `nao sou json`,
	}

	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			var recebido model.PreventivaPayload
			w, ctx := requisicaoPreventiva(http.MethodPost, "", "", corpo)
			NewPreventivaController(preventivaFake{recebido: &recebido}).Cadastrar()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
			}
			if recebido.Descricao != "" || recebido.MaquinaId != 0 {
				t.Errorf("payload inválido chegou no service: %+v", recebido)
			}
		})
	}

	// A data tem que voltar no formato do contrato, não em RFC3339.
	t.Run("resposta traz a data em dd/mm/yyyy", func(t *testing.T) {
		w, ctx := requisicaoPreventiva(http.MethodGet, "3", "", "")
		NewPreventivaController(preventivaFake{}).Obter()(ctx)

		if !strings.Contains(w.Body.String(), `"proximaData":"`) || strings.Contains(w.Body.String(), "T00:00:00Z") {
			t.Errorf("data fora do contrato: %s", w.Body)
		}
	})
}

// ?maquinaId= é o que a tela de edição de máquina usa para listar só as dela;
// sem filtro, a aba "Manutenção Prev." do gestor lista o escopo inteiro.
func TestPreventivaListarFiltroDeMaquina(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("sem filtro chega nil", func(t *testing.T) {
		var recebido *int64
		w, ctx := requisicaoPreventiva(http.MethodGet, "", "", "")
		NewPreventivaController(preventivaFake{maquinaId: &recebido}).Listar()(ctx)

		if w.Code != http.StatusOK || recebido != nil {
			t.Fatalf("status %d, maquinaId %v", w.Code, recebido)
		}
	})

	t.Run("maquinaId válido chega no service", func(t *testing.T) {
		var recebido *int64
		w, ctx := requisicaoPreventiva(http.MethodGet, "", "?maquinaId=3", "")
		NewPreventivaController(preventivaFake{maquinaId: &recebido}).Listar()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if recebido == nil || *recebido != 3 {
			t.Fatalf("maquinaId = %v, esperado 3", recebido)
		}
	})

	t.Run("maquinaId inválido é 400 e não vai ao banco", func(t *testing.T) {
		for _, ruim := range []string{"abc", "0", "-3"} {
			var recebido *int64
			w, ctx := requisicaoPreventiva(http.MethodGet, "", "?maquinaId="+ruim, "")
			NewPreventivaController(preventivaFake{maquinaId: &recebido}).Listar()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Errorf("maquinaId=%s: status = %d, esperado 400", ruim, w.Code)
			}
			if recebido != nil {
				t.Errorf("maquinaId=%s chegou no service", ruim)
			}
		}
	})

	// Lista vazia tem que sair como [] e não null: o front tipa
	// PreventivaListada[] e null quebra o .map.
	t.Run("lista vazia sai como array", func(t *testing.T) {
		w, ctx := requisicaoPreventiva(http.MethodGet, "", "", "")
		NewPreventivaController(preventivaFake{}).Listar()(ctx)

		if corpo := strings.TrimSpace(w.Body.String()); corpo != "[]" {
			t.Errorf("corpo = %s, esperado []", corpo)
		}
	})

	t.Run("sem tenant no contexto é 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/preventivas", nil)

		NewPreventivaController(preventivaFake{}).Listar()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
	})

	// Mesmo motivo de /maquinas: é o ator do token que recorta a aba do gestor.
	t.Run("ator vem do token, não da query", func(t *testing.T) {
		var recebido string
		w, ctx := requisicaoPreventiva(http.MethodGet, "", "?usuarioId=999&perfil=administrador", "")
		ctx.Set(middleware.UserId, int64(42))
		ctx.Set(middleware.UserPerfil, "gestor")

		NewPreventivaController(preventivaFake{ator: &recebido}).Listar()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if recebido != "42/gestor" {
			t.Fatalf("ator = %q, esperado 42/gestor", recebido)
		}
	})

	t.Run("sem as claims do ator é 500 e não lista nada", func(t *testing.T) {
		var recebido string
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/preventivas", nil)
		ctx.Set(middleware.UserTenantId, int64(7)) // tenant ok, ator faltando

		NewPreventivaController(preventivaFake{ator: &recebido}).Listar()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
		if recebido != "" {
			t.Errorf("listou sem saber quem é o ator: %q", recebido)
		}
	})
}
