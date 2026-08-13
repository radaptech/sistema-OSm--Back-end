package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func AutenticacaoJwt() gin.HandlerFunc {

	jwtsecret := []byte(os.Getenv("JWT_SECRET"))

	if len(jwtsecret) == 0 {

		panic("JWT_SECRET não configurada")
	}

	return func(ctx *gin.Context) {

		var TokenString string

		cookieToken, err := ctx.Cookie("token")
		if err == nil && cookieToken != "" {
			TokenString = cookieToken
		} else {

			const portador = "Bearer " 
			header := ctx.GetHeader("Authorization")

			if header != "" && strings.HasPrefix(header, portador) {

				TokenString = strings.TrimPrefix(header, portador)
			}
		}

		if TokenString == "" {

			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Acesso negado, token ausente ou formato invalido"})
			return
		}

		token, err := jwt.Parse(TokenString, func(t *jwt.Token) (any, error) {

			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("método de assinatura inesperado: %v", t.Header["alg"])
			}
			return jwtsecret, nil
		})

		if err != nil || !token.Valid {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token inválido ou expirado"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Falha ao processar permissões"})
			return
		}

		// Token sem sub/perfil/tenantId é inútil pra qualquer rota autenticada --
		// falha fechada aqui, não deixa o handler seguir com contexto incompleto.
		userId, ok := claims["sub"].(float64)
		if !ok {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token não contém identificação do usuário"})
			return
		}

		perfil, ok := claims["perfil"].(string)
		if !ok {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token não contém perfil"})
			return
		}

		tenantId, ok := claims["tenantId"].(float64)
		if !ok {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token não contém tenant"})
			return
		}

		ctx.Set("userId", int64(userId))
		ctx.Set("user_perfil", perfil)
		ctx.Set("user_TenantId", int64(tenantId))

		ctx.Next()
	}
}
