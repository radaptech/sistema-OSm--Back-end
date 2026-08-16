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
	err        error
	recebido   *filtrosRecebidos // opcional: só ListarUsuarios grava aqui
	desativado *[2]int64         // opcional: {id alvo, ator} de DesativarUsuario
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

func (s serviceFake) ObterUsuario(_ context.Context, id, _ int64) (model.Usuario, error) {
	if s.err != nil {
		return model.Usuario{}, s.err
	}
	return model.Usuario{Id: id, Nome: "Davi"}, nil
}

func (s serviceFake) AtualizarUsuario(_ context.Context, id int64, p model.AtualizarUsuarioPayload, _ int64) (model.Usuario, error) {
	return model.Usuario{Id: id, Nome: p.Nome}, s.err
}

func (s serviceFake) DesativarUsuario(_ context.Context, id, _, atorId int64) error {
	if s.desativado != nil {
		*s.desativado = [2]int64{id, atorId}
	}
	return s.err
}

// filtrosRecebidos guarda o que o handler traduziu da query string -- é o que
// TestListarUsuariosFiltros verifica. O service em si tem teste próprio.
type filtrosRecebidos struct {
	tenantId int64
	pagina   int32
	perfil   *string
	busca    *string
	lojaId   *int64
}

func (s serviceFake) ListarUsuarios(_ context.Context, tenantId int64, pagina int32, perfil, busca *string, lojaId *int64) (model.RespostaPaginada[model.Usuario], error) {
	if s.recebido != nil {
		*s.recebido = filtrosRecebidos{tenantId, pagina, perfil, busca, lojaId}
	}
	if s.err != nil {
		return model.RespostaPaginada[model.Usuario]{}, s.err
	}
	return model.RespostaPaginada[model.Usuario]{Dados: []model.Usuario{}, Pagina: pagina}, nil
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
		{"validacao", fmt.Errorf("%w: gestor precisa de ao menos 1 loja", helper.ErrValidacao), http.StatusBadRequest, ""},
		{"duplicado", helper.ErrDadoDuplicado, http.StatusConflict, ""},
		{"nao encontrado", fmt.Errorf("%w: área técnica não cadastrada", helper.ErrNaoEncontrado), http.StatusUnprocessableEntity, ""},
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

// A query string é a única entrada não validada de ListarUsuarios: `perfil`
// vai parar num cast ::perfil_usuario, e `pagina`/`lojaId` viram int. Lixo
// aqui tem que virar 400 antes do banco, não 500 depois dele. E filtro
// ausente tem que chegar nil no service -- string vazia filtraria por "".
func TestListarUsuariosFiltros(t *testing.T) {

	gin.SetMode(gin.TestMode)

	texto := func(s string) *string { return &s }
	numero := func(n int64) *int64 { return &n }

	casos := []struct {
		nome     string
		query    string
		status   int
		esperado filtrosRecebidos // só conferido quando status == 200
	}{
		{
			nome:  "sem filtro nenhum: tudo nil, página 1",
			query: "", status: http.StatusOK,
			esperado: filtrosRecebidos{tenantId: 7, pagina: 1},
		},
		{
			nome:  "todos os filtros juntos",
			query: "?pagina=3&perfil=gestor&lojaId=42&busca=ana", status: http.StatusOK,
			esperado: filtrosRecebidos{tenantId: 7, pagina: 3, perfil: texto("gestor"), busca: texto("ana"), lojaId: numero(42)},
		},
		{
			// montarQuery no front já descarta vazio, mas ?busca= chega assim
			// se alguém montar a URL na mão -- tem que ser "não filtra".
			nome:  "parâmetro vazio é o mesmo que ausente",
			query: "?busca=&perfil=&lojaId=&pagina=", status: http.StatusOK,
			esperado: filtrosRecebidos{tenantId: 7, pagina: 1},
		},
		{nome: "perfil fora do ENUM", query: "?perfil=chefe", status: http.StatusBadRequest},
		{nome: "lojaId não numérico", query: "?lojaId=abc", status: http.StatusBadRequest},
		{nome: "lojaId zero", query: "?lojaId=0", status: http.StatusBadRequest},
		{nome: "pagina não numérica", query: "?pagina=x", status: http.StatusBadRequest},
		{nome: "pagina zero", query: "?pagina=0", status: http.StatusBadRequest},
		{nome: "pagina negativa", query: "?pagina=-1", status: http.StatusBadRequest},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			var recebido filtrosRecebidos
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/usuarios"+c.query, nil)
			// Tenant do token: rota autenticada. Se o handler ler o header,
			// não acha nada aqui e responde 500 -- é o que trava a regressão.
			ctx.Set(middleware.UserTenantId, int64(7))

			NewLoginController(serviceFake{recebido: &recebido}).ListarUsuarios()(ctx)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
			if c.status != http.StatusOK {
				// Filtro recusado não pode ter chegado no banco.
				if recebido != (filtrosRecebidos{}) {
					t.Errorf("service foi chamado com filtro inválido: %+v", recebido)
				}
				return
			}
			if !filtrosIguais(recebido, c.esperado) {
				t.Errorf("filtros = %s, esperado %s", formatarFiltros(recebido), formatarFiltros(c.esperado))
			}
		})
	}

	t.Run("erro do banco é 500 sem vazar o pgx", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/usuarios", nil)
		ctx.Set(middleware.UserTenantId, int64(7))

		erroCru := fmt.Errorf(`ERROR: relation "usuario_escopo" does not exist (SQLSTATE 42P01)`)
		NewLoginController(serviceFake{err: erroCru}).ListarUsuarios()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
		if strings.Contains(w.Body.String(), "usuario_escopo") {
			t.Errorf("resposta vazou detalhe interno: %s", w.Body)
		}
	})

	t.Run("sem tenant no contexto é 500, nunca listagem do tenant errado", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/usuarios", nil)

		NewLoginController(serviceFake{}).ListarUsuarios()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
	})
}

func filtrosIguais(a, b filtrosRecebidos) bool {
	iguaisTexto := func(x, y *string) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	iguaisNum := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.tenantId == b.tenantId && a.pagina == b.pagina &&
		iguaisTexto(a.perfil, b.perfil) && iguaisTexto(a.busca, b.busca) && iguaisNum(a.lojaId, b.lojaId)
}

func formatarFiltros(f filtrosRecebidos) string {
	texto := func(p *string) string {
		if p == nil {
			return "nil"
		}
		return fmt.Sprintf("%q", *p)
	}
	numero := func(p *int64) string {
		if p == nil {
			return "nil"
		}
		return fmt.Sprint(*p)
	}
	return fmt.Sprintf("{tenant:%d pagina:%d perfil:%s busca:%s loja:%s}",
		f.tenantId, f.pagina, texto(f.perfil), texto(f.busca), numero(f.lojaId))
}

// requisicaoComId monta o contexto do jeito que o router monta: :id na URL,
// tenant e usuário vindos do token.
func requisicaoComId(metodo, id, corpo string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, "/usuarios/"+id, strings.NewReader(corpo))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: id}}
	ctx.Set(middleware.UserTenantId, int64(7))
	ctx.Set(middleware.UserId, int64(42))
	return w, ctx
}

// Mesmo mapa de erro do Registrar: erro de negócio virando 500 deixa o front
// sem saber se foi e-mail duplicado ou banco caído, e 500 com o erro cru vaza
// nome de constraint do pgx.
func TestAtualizarMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	const corpo = `{"nome":"Davi","email":"a@b.com","perfil":"administrador"}`

	casos := []struct {
		nome    string
		id      string
		err     error
		status  int
		vazando string
	}{
		{"sucesso", "3", nil, http.StatusOK, ""},
		{"id malformado não é 404", "abc", nil, http.StatusBadRequest, ""},
		{"id zero", "0", nil, http.StatusBadRequest, ""},
		{"validacao", "3", fmt.Errorf("%w: gestor precisa de 1 loja", helper.ErrValidacao), http.StatusBadRequest, ""},
		{"duplicado", "3", helper.ErrDadoDuplicado, http.StatusConflict, ""},
		{"nao encontrado", "3", fmt.Errorf("%w: área técnica não cadastrada", helper.ErrNaoEncontrado), http.StatusUnprocessableEntity, ""},
		{"interno", "3", fmt.Errorf(`ERROR: duplicate key "uq_usuario_email" (SQLSTATE 23505)`), http.StatusInternalServerError, "uq_usuario_email"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			w, ctx := requisicaoComId(http.MethodPut, c.id, corpo)
			NewLoginController(serviceFake{err: c.err}).Atualizar()(ctx)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
			if c.vazando != "" && strings.Contains(w.Body.String(), c.vazando) {
				t.Fatalf("resposta vazou detalhe interno %q: %s", c.vazando, w.Body)
			}
		})
	}

	t.Run("corpo inválido não chega no service", func(t *testing.T) {
		w, ctx := requisicaoComId(http.MethodPut, "3", `{"nome":"Davi"}`) // sem email/perfil
		NewLoginController(serviceFake{}).Atualizar()(ctx)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
		}
	})
}

func TestDesativarMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	casos := []struct {
		nome   string
		id     string
		err    error
		status int
	}{
		{"sucesso", "3", nil, http.StatusOK},
		{"id malformado", "abc", nil, http.StatusBadRequest},
		// Auto-desativação: o service recusa, e 400 é o que diz ao front que o
		// pedido está errado -- 403 seria "você não pode", e ele pode, só não em si.
		{"desativar a si mesmo", "42", fmt.Errorf("%w: não pode desativar a si mesmo", helper.ErrValidacao), http.StatusBadRequest},
		{"id inexistente", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"integridade", "3", helper.ErrConflitoIntegridade, http.StatusUnprocessableEntity},
		{"interno", "3", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			w, ctx := requisicaoComId(http.MethodDelete, c.id, "")
			NewLoginController(serviceFake{err: c.err}).Desativar()(ctx)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
		})
	}

	// O ator TEM que vir do token. Lido da rota ou do corpo, o administrador
	// escolheria quem é ele e a trava de auto-desativação viraria decoração.
	t.Run("alvo vem da rota, ator vem do token", func(t *testing.T) {
		var desativado [2]int64
		w, ctx := requisicaoComId(http.MethodDelete, "3", "")
		NewLoginController(serviceFake{desativado: &desativado}).Desativar()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, esperado 200", w.Code)
		}
		if desativado != [2]int64{3, 42} {
			t.Errorf("service recebeu {alvo, ator} = %v, esperado {3 42}", desativado)
		}
	})

	t.Run("sem usuário no contexto é 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodDelete, "/usuarios/3", nil)
		ctx.Params = gin.Params{{Key: "id", Value: "3"}}
		ctx.Set(middleware.UserTenantId, int64(7)) // tenant sim, usuário não

		NewLoginController(serviceFake{}).Desativar()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
	})
}

func TestObterMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	casos := []struct {
		nome   string
		id     string
		err    error
		status int
	}{
		{"sucesso", "3", nil, http.StatusOK},
		{"id malformado é 400, não 404", "abc", nil, http.StatusBadRequest},
		{"id inexistente", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"interno", "3", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			w, ctx := requisicaoComId(http.MethodGet, c.id, "")
			NewLoginController(serviceFake{err: c.err}).Obter()(ctx)

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
		})
	}
}
