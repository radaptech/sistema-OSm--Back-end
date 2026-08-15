package controller

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

type LoginServiceInterface interface {
	CadastrarUsuario(ctx context.Context, modelUser model.NovoUsuarioPayload, TenantID int64) (model.Usuario, error)
	Login(ctx context.Context, loginModel model.Login, tenantId int64) (string, model.SessaoUsuario, error)
	ObterSessao(ctx context.Context, userId, tenantId int64) (model.SessaoUsuario, error)
}

type LoginController struct {
	service LoginServiceInterface
}

func NewLoginController(service LoginServiceInterface) *LoginController {

	return &LoginController{
		service: service,
	}
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
