package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
)

type validadorFake struct {
	ator     Ator
	err      error
	recebido *string
}

func (v validadorFake) ValidarSessao(_ context.Context, segredo string) (Ator, error) {
	if v.recebido != nil {
		*v.recebido = segredo
	}
	return v.ator, v.err
}

// O contrato com o front: sem sessão é 401 mudo; sessão que existia e morreu é
// 401 com codigo (a tela de login explica) e o cookie apagado; banco fora é
// 500 (um 401 deslogaria todo mundo por um soluço do Postgres).
func TestAutenticacao(t *testing.T) {

	gin.SetMode(gin.TestMode)

	rodar := func(v validadorFake, preparar func(*http.Request)) (*httptest.ResponseRecorder, *gin.Context) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		if preparar != nil {
			preparar(ctx.Request)
		}
		Autenticacao(v)(ctx)
		return w, ctx
	}
	comCookie := func(valor string) func(*http.Request) {
		return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: NomeCookieSessao, Value: valor}) }
	}
	codigo := func(t *testing.T, w *httptest.ResponseRecorder) string {
		t.Helper()
		var corpo map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &corpo)
		return corpo["codigo"]
	}

	t.Run("sessão válida passa e põe o dono no contexto", func(t *testing.T) {
		var recebido string
		w, ctx := rodar(validadorFake{ator: Ator{UsuarioId: 3, TenantId: 1, Perfil: "gestor"}, recebido: &recebido}, comCookie("segredo"))
		if ctx.IsAborted() || w.Code != http.StatusOK || recebido != "segredo" {
			t.Fatalf("abortou = %v, status %d, segredo %q", ctx.IsAborted(), w.Code, recebido)
		}
		usuario, _ := GetUserID(ctx)
		tenant, _ := GetTenantIDToken(ctx)
		perfil, _ := GetUserPerfil(ctx)
		if usuario != 3 || tenant != 1 || perfil != "gestor" {
			t.Errorf("contexto = %d/%d/%q", usuario, tenant, perfil)
		}
	})

	t.Run("Bearer também serve, para clientes fora do navegador", func(t *testing.T) {
		var recebido string
		_, ctx := rodar(validadorFake{recebido: &recebido}, func(r *http.Request) { r.Header.Set("Authorization", "Bearer abc") })
		if ctx.IsAborted() || recebido != "abc" {
			t.Errorf("abortou = %v, segredo %q", ctx.IsAborted(), recebido)
		}
	})

	t.Run("sem sessão é 401 sem código e não consulta o banco", func(t *testing.T) {
		recebido := "não chamado"
		w, _ := rodar(validadorFake{recebido: &recebido}, nil)
		if w.Code != http.StatusUnauthorized || codigo(t, w) != "" || recebido != "não chamado" {
			t.Errorf("status %d, codigo %q, validador chamado com %q", w.Code, codigo(t, w), recebido)
		}
	})

	t.Run("sessão expirada é 401 com código e apaga o cookie", func(t *testing.T) {
		w, _ := rodar(validadorFake{err: helper.ErrSessaoExpirada}, comCookie("velho"))
		if w.Code != http.StatusUnauthorized || codigo(t, w) != CodigoSessaoExpirada {
			t.Errorf("status %d, codigo %q", w.Code, codigo(t, w))
		}
		cookie := w.Header().Get("Set-Cookie")
		if !strings.HasPrefix(cookie, NomeCookieSessao+"=;") || !strings.Contains(cookie, "Max-Age=0") {
			t.Errorf("cookie não foi apagado: %q", cookie)
		}
	})

	t.Run("falha do banco é 500, não 401", func(t *testing.T) {
		w, _ := rodar(validadorFake{err: errors.New("conexão recusada")}, comCookie("segredo"))
		if w.Code != http.StatusInternalServerError || w.Header().Get("Set-Cookie") != "" {
			t.Errorf("status %d, Set-Cookie %q", w.Code, w.Header().Get("Set-Cookie"))
		}
	})
}
