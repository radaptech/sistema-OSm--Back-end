package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/ginmw"
)

type RecuperacaoSenhaServiceInterface interface {
	SolicitarRecuperacao(ctx context.Context, tenantId int64, subdominio, email string) error
	RedefinirSenha(ctx context.Context, tenantId int64, token, novaSenha string) error
}

type RecuperacaoSenhaController struct {
	service RecuperacaoSenhaServiceInterface
}

func NewRecuperacaoSenhaController(service RecuperacaoSenhaServiceInterface) *RecuperacaoSenhaController {

	return &RecuperacaoSenhaController{
		service: service,
	}
}

// EsqueciSenha é POST /autenticacao/esqueci-senha. Responde a mesma coisa exista o e-mail ou não.
func (r *RecuperacaoSenhaController) EsqueciSenha() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		input, ok := corpoJSON[model.SolicitarRecuperacaoSenha](ctx)
		if !ok {
			return
		}

		// Rota pública: tenant do header, como no login (TenantMiddleware já validou o subdomínio no banco).
		tenantId, ok := ginmw.TenantIDFromHeader(ctx)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
			return
		}
		subdominio := strings.TrimSpace(ctx.GetHeader("X-tenant-ID"))

		if err := r.service.SolicitarRecuperacao(ctx.Request.Context(), tenantId, subdominio, input.Email); err != nil {
			log.Printf("solicitar recuperação de senha tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao solicitar recuperação de senha"})
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"message": "Se o e-mail estiver cadastrado, você receberá um link para redefinir a senha."})
	}
}

// RedefinirSenha é POST /autenticacao/redefinir-senha. Token ruim é 400 e não 401: 401 desloga o front.
func (r *RecuperacaoSenhaController) RedefinirSenha() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		input, ok := corpoJSON[model.RedefinirSenha](ctx)
		if !ok {
			return
		}

		tenantId, ok := ginmw.TenantIDFromHeader(ctx)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
			return
		}

		err := r.service.RedefinirSenha(ctx.Request.Context(), tenantId, input.Token, input.Senha)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrTokenRecuperacaoInvalido):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				log.Printf("redefinir senha tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao redefinir senha"})
			}
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"message": "Senha redefinida com sucesso."})
	}
}
