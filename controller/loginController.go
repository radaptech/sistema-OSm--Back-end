package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

type LoginServiceInterface interface {
	CadastrarUsuario(ctx context.Context, modelUser model.NovoUsuarioPayload, TenantID int64) (model.Usuario, error)
	Login(ctx context.Context, loginModel model.Login, tenantId int64) (string, model.SessaoUsuario, error)
	ObterSessao(ctx context.Context, userId, tenantId int64) (model.SessaoUsuario, error)
	ListarUsuarios(ctx context.Context, tenantId int64, pagina int32, perfil, busca *string, lojaId *int64) (model.RespostaPaginada[model.Usuario], error)
	ObterUsuario(ctx context.Context, id, tenantId int64) (model.Usuario, error)
	AtualizarUsuario(ctx context.Context, id int64, payload model.AtualizarUsuarioPayload, tenantId int64) (model.Usuario, error)
	DesativarUsuario(ctx context.Context, id, tenantId, atorId int64) error
}

type LoginController struct {
	service LoginServiceInterface
}

func NewLoginController(service LoginServiceInterface) *LoginController {

	return &LoginController{
		service: service,
	}
}

// cookieSessao escreve o cookie que middleware.AutenticacaoJwt lê. Login e
// logout passam por aqui de propósito: o browser só apaga um cookie se o
// Set-Cookie de remoção casar com o de criação, e um Set-Cookie sem Secure
// vindo de origem insegura nem sobrescreve um cookie Secure.
//
// SameSite Lax: front e API ficam sob o mesmo domínio registrável
// (*.radaptech.com.br, localhost em dev), então o cookie viaja. Só vira
// SameSiteNone (que obriga Secure) se o front sair pra outro domínio.
// maxAge 86400 (24h) é maior que o exp do JWT (8h, auth.GerarJwt): passado o
// exp o middleware rejeita e o 401 do /sessao apaga o cookie. -1 apaga.
func cookieSessao(ctx *gin.Context, token string, maxAge int) {
	ctx.SetSameSite(http.SameSiteLaxMode)
	ctx.SetCookie("token", token, maxAge, "/", "", true, true)
}

func (l *LoginController) Registrar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		var input model.NovoUsuarioPayload

		if err := ctx.ShouldBindJSON(&input); err != nil {

			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":    "dados invalidos",
				"detalhes": err.Error(),
			})
			return
		}

		// Tenant do token, não do header: rota autenticada.
		tenantID, ok := middleware.GetTenantIDToken(ctx)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno de tenant"})
			return
		}

		user, err := l.service.CadastrarUsuario(ctx.Request.Context(), input, tenantID)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "e-mail já cadastrado"})
			case errors.Is(err, helper.ErrNaoEncontrado), errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				// Erro interno vai pro log, não pra resposta: o erro cru do pgx
				// carrega nome de constraint/coluna e às vezes o SQL.
				log.Printf("cadastrar usuario tenant=%d: %v", tenantID, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao registrar usuario"})
			}
			return
		}

		ctx.JSON(http.StatusCreated, user)
	}
}

func (l *LoginController) Login() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		var input model.Login

		if err := ctx.ShouldBindJSON(&input); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":    "dados invalidos",
				"detalhes": err.Error(),
			})
			return
		}

		// Único endpoint em que o tenant vem do header: ainda não existe token.
		tenantId, ok := middleware.GetTenantID(ctx)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
			return
		}

		token, user, err := l.service.Login(ctx.Request.Context(), input, tenantId)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrCredenciaisInvalidas):
				ctx.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			default:
				log.Printf("login tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao logar usuario"})
			}
			return
		}

		cookieSessao(ctx, token, 86400)

		ctx.JSON(http.StatusOK, user)
	}
}

func (l *LoginController) Logout() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		cookieSessao(ctx, "", -1)

		ctx.JSON(http.StatusOK, gin.H{"message": "Logout realizado com sucesso"})
	}
}

// Sessao é GET /autenticacao/sessao: o front chama no boot pra saber quem
// está logado sem guardar nada em localStorage. Roda atrás de
// AutenticacaoJwt, então usuário e tenant vêm do token.
func (l *LoginController) Sessao() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		userId, okUser := middleware.GetUserID(ctx)
		tenantId, okTenant := middleware.GetTenantIDToken(ctx)
		if !okUser || !okTenant {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de sessao"})
			return
		}

		user, err := l.service.ObterSessao(ctx.Request.Context(), userId, tenantId)
		if err != nil {

			switch {
			// Token assinado mas o dono sumiu/foi desativado: sessão morta.
			// 401 aqui é o que faz o front deslogar sozinho.
			case errors.Is(err, helper.ErrSessaoExpirada):
				cookieSessao(ctx, "", -1)
				ctx.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			default:
				log.Printf("obter sessao usuario=%d tenant=%d: %v", userId, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter sessao"})
			}
			return
		}

		ctx.JSON(http.StatusOK, user)
	}
}

// perfisValidos é o ENUM perfil_usuario do banco. O filtro entra num cast
// ::perfil_usuario dentro da query, então valor fora da lista vira erro 22P02
// do Postgres -- 500 para o que é erro do cliente. Barra aqui e responde 400.
var perfisValidos = []string{"solicitante", "tecnico", "gestor", "administrador"}

// ListarUsuarios é GET /usuarios: a listagem paginada do Administrador
// (front-end/CLAUDE.md item 12), 10 por página no servidor.
//
// Filtros opcionais na query string -- ?perfil=&lojaId=&busca=&pagina= --, os
// mesmos nomes de ParametrosListagemUsuarios no front. Ausente ou vazio = não
// filtra: montarQuery já descarta undefined/null/” do lado de lá, então
// string vazia aqui é o mesmo que não mandar.
//
// Sem switch de erro como os outros handlers: listagem não tem erro de
// negócio. Ou os filtros são inválidos (400, resolvido antes de chegar no
// service) ou o banco falhou (500). Página sem resultado é 200 com dados: [].
func (l *LoginController) ListarUsuarios() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		// Tenant do token, não do header: rota autenticada. Com GetTenantID
		// aqui, um administrador do tenant A lista o tenant B só trocando o
		// X-tenant-ID -- o banco aceita calado, é só um int64.
		tenantId, ok := middleware.GetTenantIDToken(ctx)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
			return
		}

		pagina := int32(1)
		if bruto := ctx.Query("pagina"); bruto != "" {
			n, err := strconv.Atoi(bruto)
			if err != nil || n < 1 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "pagina inválida"})
				return
			}
			pagina = int32(n)
		}

		var lojaId *int64
		if bruto := ctx.Query("lojaId"); bruto != "" {
			n, err := strconv.ParseInt(bruto, 10, 64)
			if err != nil || n < 1 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "lojaId inválido"})
				return
			}
			lojaId = &n
		}

		var perfil *string
		if bruto := ctx.Query("perfil"); bruto != "" {
			if !slices.Contains(perfisValidos, bruto) {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "perfil inválido"})
				return
			}
			perfil = &bruto
		}

		var busca *string
		if bruto := ctx.Query("busca"); bruto != "" {
			busca = &bruto
		}

		usuariosPaginados, err := l.service.ListarUsuarios(ctx.Request.Context(), tenantId, pagina, perfil, busca, lojaId)
		if err != nil {
			log.Printf("listar usuarios tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar usuarios"})
			return
		}

		ctx.JSON(http.StatusOK, usuariosPaginados)
	}
}

// idDaRota lê o :id da URL. Erro aqui é 400 e não 404 de propósito: "/abc" não
// é um id que não existe, é um id malformado -- quem não distingue os dois
// acaba respondendo 404 pra bug de cliente.
func idDaRota(ctx *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || id < 1 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": helper.ErrId.Error()})
		return 0, false
	}
	return id, true
}

// Atualizar é PUT /usuarios/:id. Mesmo payload de Registrar com a senha
// opcional, então o mapa de erro é o mesmo -- inclusive ErrNaoEncontrado em
// 422, que aqui cobre tanto o usuário quanto a área técnica citada no corpo.
func (l *LoginController) Atualizar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		var input model.AtualizarUsuarioPayload

		if err := ctx.ShouldBindJSON(&input); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":    "dados invalidos",
				"detalhes": err.Error(),
			})
			return
		}

		// Tenant do token, não do header: rota autenticada.
		tenantId, okTenant := middleware.GetTenantIDToken(ctx)
		if !okTenant {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
			return
		}

		user, err := l.service.AtualizarUsuario(ctx.Request.Context(), id, input, tenantId)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "e-mail já cadastrado"})
			case errors.Is(err, helper.ErrNaoEncontrado), errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("atualizar usuario id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar usuario"})
			}
			return
		}

		ctx.JSON(http.StatusOK, user)
	}
}

// Desativar é DELETE /usuarios/:id -- soft delete (ativo = false), nunca DELETE
// de verdade: o histórico de OS aponta pro usuário.
//
// Passa o userId do token como ator porque o service recusa auto-desativação:
// é o que garante que sobra ao menos um administrador ativo no tenant. Aqui
// ErrNaoEncontrado é 404 e não 422 como no Atualizar -- neste handler o id da
// rota é a única coisa que pode não existir, então é o recurso endereçado que
// não está lá.
func (l *LoginController) Desativar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		atorId, okUser := middleware.GetUserID(ctx)
		tenantId, okTenant := middleware.GetTenantIDToken(ctx)
		if !okUser || !okTenant {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de sessao"})
			return
		}

		if err := l.service.DesativarUsuario(ctx.Request.Context(), id, tenantId, atorId); err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("desativar usuario id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao desativar usuario"})
			}
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"message": "usuário desativado"})
	}
}

// Obter é GET /usuarios/:id. ErrNaoEncontrado é 404 (e não o 422 do Atualizar):
// aqui o id da rota é a única coisa que pode faltar, então é o recurso
// endereçado que não está lá.
func (l *LoginController) Obter() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		// Tenant do token, não do header: rota autenticada.
		tenantId, okTenant := middleware.GetTenantIDToken(ctx)
		if !okTenant {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
			return
		}

		user, err := l.service.ObterUsuario(ctx.Request.Context(), id, tenantId)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				log.Printf("obter usuario id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter usuario"})
			}
			return
		}

		ctx.JSON(http.StatusOK, user)
	}
}
