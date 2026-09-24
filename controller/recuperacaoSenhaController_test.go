package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/ginmw"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
)

// recuperacaoFake grava o que recebeu: tenant ou subdomínio vindo do lugar errado não muda o status.
type recuperacaoFake struct {
	err      error
	chamado  bool
	tenantId int64
	args     [2]string
}

func (r *recuperacaoFake) SolicitarRecuperacao(_ context.Context, tenantId int64, subdominio, email string) error {
	r.chamado, r.tenantId, r.args = true, tenantId, [2]string{subdominio, email}
	return r.err
}

func (r *recuperacaoFake) RedefinirSenha(_ context.Context, tenantId int64, token, novaSenha string) error {
	r.chamado, r.tenantId, r.args = true, tenantId, [2]string{token, novaSenha}
	return r.err
}

func requisicaoRecuperacao(handler func(*RecuperacaoSenhaController) gin.HandlerFunc, fake *recuperacaoFake, corpo string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/autenticacao/x", strings.NewReader(corpo))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("X-tenant-ID", " acme ")
	ctx.Set(ginmw.TenantIDHeaderKey, int64(7))
	handler(NewRecuperacaoSenhaController(fake))(ctx)
	return w
}

func TestEsqueciSenha(t *testing.T) {

	casos := []struct {
		nome       string
		corpo      string
		err        error
		status     int
		deveChamar bool
		naoContem  string
	}{
		{"sucesso", `{"email":"ana@acme.com"}`, nil, http.StatusOK, true, ""},
		{"e-mail invalido nao chega no service", `{"email":"ana"}`, nil, http.StatusBadRequest, false, ""},
		{"corpo vazio nao chega no service", `{}`, nil, http.StatusBadRequest, false, ""},
		{"erro interno nao vaza no corpo", `{"email":"ana@acme.com"}`, errors.New("pg: coluna token_recuperacao_hash"), http.StatusInternalServerError, true, "token_recuperacao_hash"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			fake := &recuperacaoFake{err: c.err}
			w := requisicaoRecuperacao((*RecuperacaoSenhaController).EsqueciSenha, fake, c.corpo)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperava %d (corpo: %s)", w.Code, c.status, w.Body)
			}
			if fake.chamado != c.deveChamar {
				t.Fatalf("service chamado = %v, esperava %v", fake.chamado, c.deveChamar)
			}
			if c.naoContem != "" && strings.Contains(w.Body.String(), c.naoContem) {
				t.Fatalf("erro cru vazou na resposta: %s", w.Body)
			}
			if c.status == http.StatusOK && (fake.tenantId != 7 || fake.args != [2]string{"acme", "ana@acme.com"}) {
				t.Fatalf("service recebeu tenant=%d args=%q", fake.tenantId, fake.args)
			}
		})
	}
}

func TestRedefinirSenha(t *testing.T) {

	casos := []struct {
		nome       string
		corpo      string
		err        error
		status     int
		deveChamar bool
	}{
		{"sucesso", `{"token":"ABC","senha":"123456"}`, nil, http.StatusOK, true},
		{"senha curta nao chega no service", `{"token":"ABC","senha":"123"}`, nil, http.StatusBadRequest, false},
		{"sem token nao chega no service", `{"senha":"123456"}`, nil, http.StatusBadRequest, false},
		// 400 e não 401: 401 fora do /login desloga o front.
		{"token invalido", `{"token":"ABC","senha":"123456"}`, helper.ErrTokenRecuperacaoInvalido, http.StatusBadRequest, true},
		{"erro interno", `{"token":"ABC","senha":"123456"}`, errors.New("pg caiu"), http.StatusInternalServerError, true},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			fake := &recuperacaoFake{err: c.err}
			w := requisicaoRecuperacao((*RecuperacaoSenhaController).RedefinirSenha, fake, c.corpo)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperava %d (corpo: %s)", w.Code, c.status, w.Body)
			}
			if fake.chamado != c.deveChamar {
				t.Fatalf("service chamado = %v, esperava %v", fake.chamado, c.deveChamar)
			}
			if c.status == http.StatusOK && (fake.tenantId != 7 || fake.args != [2]string{"ABC", "123456"}) {
				t.Fatalf("service recebeu tenant=%d args=%q", fake.tenantId, fake.args)
			}

			var corpo map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &corpo); err != nil {
				t.Fatalf("corpo não é JSON: %s", w.Body)
			}
			if c.err != nil && c.status == http.StatusInternalServerError && strings.Contains(corpo["error"], "pg caiu") {
				t.Fatalf("erro cru vazou na resposta: %s", w.Body)
			}
			if errors.Is(c.err, helper.ErrTokenRecuperacaoInvalido) && corpo["error"] != helper.ErrTokenRecuperacaoInvalido.Error() {
				t.Fatalf("mensagem do token inválido não chegou ao toast: %s", w.Body)
			}
		})
	}
}
