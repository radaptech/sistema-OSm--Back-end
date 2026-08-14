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

// serviceFake devolve sempre o mesmo erro -- é o único eixo que o teste varia.
type serviceFake struct {
	err error
}

const tokenFake = "jwt.de.mentira"

func (s serviceFake) CadastrarUsuario(context.Context, model.NovoUsuarioPayload, int64) (model.Usuario, error) {
	return model.Usuario{Id: 1}, s.err
}

func (s serviceFake) Login(context.Context, model.Login, int64) (string, model.SessaoUsuario, error) {
	if s.err != nil {
		return "", model.SessaoUsuario{}, s.err
	}
	return tokenFake, model.SessaoUsuario{Id: 1, Nome: "Davi"}, nil
}

func (s serviceFake) ObterSessao(context.Context, int64, int64) (model.SessaoUsuario, error) {
	if s.err != nil {
		return model.SessaoUsuario{}, s.err
	}
	return model.SessaoUsuario{Id: 1, Nome: "Davi"}, nil
}

// Erro de negócio virando 500 quebra o front (ele não consegue distinguir
// "e-mail já existe" de "banco caiu"), e 500 com err.Error() no corpo vaza
// nome de constraint/coluna do pgx.
func TestRegistrarMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	const corpo = `{"nome":"Davi","email":"a@b.com","telefone":"11999999999","senha":"12345678","perfil":"administrador"}`

	casos := []struct {
		nome    string
		err     error
		status  int
		vazando string // texto interno que NÃO pode aparecer na resposta
	}{
		{"sucesso", nil, http.StatusCreated, ""},
		{"validacao", fmt.Errorf("gestor precisa de ao menos 1 loja: %w", helper.ErrValidacao), http.StatusBadRequest, ""},
		{"duplicado", helper.ErrDadoDuplicado, http.StatusConflict, ""},
		{"nao encontrado", fmt.Errorf("área técnica não cadastrada: %w", helper.ErrNaoEncontrado), http.StatusUnprocessableEntity, ""},
		{"interno", fmt.Errorf(`ERROR: null value in column "senha_hash" (SQLSTATE 23502)`), http.StatusInternalServerError, "senha_hash"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/usuarios", strings.NewReader(corpo))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set(middleware.UserTenantId, int64(7))

			NewLoginController(serviceFake{err: c.err}).Registrar()(ctx)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
			if c.vazando != "" && strings.Contains(w.Body.String(), c.vazando) {
				t.Fatalf("resposta vazou detalhe interno %q: %s", c.vazando, w.Body)
			}
		})
	}
}

// O caminho de sucesso do login já esteve dentro do `if err != nil`: respondia
// 200 sem corpo e sem cookie, e o erro escrevia dois corpos. Este teste é
// exatamente esse par.
func TestLogin(t *testing.T) {

	gin.SetMode(gin.TestMode)

	const corpo = `{"perfil":"administrador","email":"a@b.com","senha":"12345678"}`

	casos := []struct {
		nome      string
		err       error
		status    int
		temCookie bool
	}{
		{"sucesso", nil, http.StatusOK, true},
		{"credenciais invalidas", helper.ErrCredenciaisInvalidas, http.StatusUnauthorized, false},
		{"erro interno", fmt.Errorf("conexão recusada"), http.StatusInternalServerError, false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/autenticacao/login", strings.NewReader(corpo))
			ctx.Request.Header.Set("Content-Type", "application/json")
			// Tenant do header: no login ainda não existe token.
			ctx.Set(middleware.TenantId, int64(7))

			NewLoginController(serviceFake{err: c.err}).Login()(ctx)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}

			cookie := w.Header().Get("Set-Cookie")
			if c.temCookie {
				// O nome tem que bater com o que AutenticacaoJwt lê.
				if !strings.HasPrefix(cookie, "token="+tokenFake) {
					t.Fatalf("cookie de sessão ausente ou com nome errado: %q", cookie)
				}
				for _, flag := range []string{"HttpOnly", "Secure", "Max-Age=86400"} {
					if !strings.Contains(cookie, flag) {
						t.Errorf("cookie sem %s: %q", flag, cookie)
					}
				}
				if w.Body.Len() == 0 {
					t.Error("sucesso sem corpo: o front espera a SessaoUsuario")
				}
			} else if cookie != "" {
				t.Fatalf("erro não pode emitir cookie de sessão: %q", cookie)
			}
		})
	}
}

func TestSessao(t *testing.T) {

	gin.SetMode(gin.TestMode)

	casos := []struct {
		nome        string
		err         error
		status      int
		limpaCookie bool
	}{
		{"sucesso", nil, http.StatusOK, false},
		// Usuário desativado com token ainda válido: 401 desloga o front, e o
		// cookie morto evita que ele fique batendo com a mesma sessão morta.
		{"sessao expirada", helper.ErrSessaoExpirada, http.StatusUnauthorized, true},
		{"erro interno", fmt.Errorf("conexão recusada"), http.StatusInternalServerError, false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/autenticacao/sessao", nil)
			ctx.Set(middleware.UserId, int64(1))
			ctx.Set(middleware.UserTenantId, int64(7))

			NewLoginController(serviceFake{err: c.err}).Sessao()(ctx)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
			if got := strings.HasPrefix(w.Header().Get("Set-Cookie"), "token=;"); got != c.limpaCookie {
				t.Errorf("limpou cookie = %v, esperado %v", got, c.limpaCookie)
			}
		})
	}
}

// Sem os claims no contexto (AutenticacaoJwt não rodou) a sessão não pode ser
// montada com id zero.
func TestSessaoSemClaims(t *testing.T) {

	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/autenticacao/sessao", nil)

	NewLoginController(serviceFake{}).Sessao()(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500", w.Code)
	}
}

// O Set-Cookie de remoção só apaga se casar com o de criação, então os dois
// saem da mesma função -- este teste é quem cobra isso.
func TestLogoutApagaCookieCompativelComOLogin(t *testing.T) {

	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/autenticacao/logout", nil)

	NewLoginController(serviceFake{}).Logout()(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", w.Code)
	}

	cookie := w.Header().Get("Set-Cookie")
	if !strings.HasPrefix(cookie, "token=;") {
		t.Fatalf("logout não limpou o cookie token: %q", cookie)
	}
	// Max-Age=0 é como o net/http serializa maxAge negativo: expira já.
	for _, flag := range []string{"Max-Age=0", "Path=/", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(cookie, flag) {
			t.Errorf("cookie de logout sem %s (não casa com o do login): %q", flag, cookie)
		}
	}
}

// Sem tenant no contexto (AutenticacaoJwt não rodou) o handler não pode
// escrever no banco com tenant zero.
func TestRegistrarSemTenantNaoChamaService(t *testing.T) {

	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/usuarios",
		strings.NewReader(`{"nome":"Davi","email":"a@b.com","telefone":"11999999999","senha":"12345678","perfil":"administrador"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	NewLoginController(serviceFake{}).Registrar()(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500", w.Code)
	}
}
