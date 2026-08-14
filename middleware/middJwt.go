package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Chaves que AutenticacaoJwt injeta no contexto do Gin. Vêm do token, então
// são as autoritativas depois do login -- TenantId (header X-tenant-ID) só
// vale em POST /autenticacao/login, antes de existir token.
const (
	UserId       = "userId"
	UserPerfil   = "user_perfil"
	UserTenantId = "user_TenantId"
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

		ctx.Set(UserId, int64(userId))
		ctx.Set(UserPerfil, perfil)
		ctx.Set(UserTenantId, int64(tenantId))

		ctx.Next()
	}
}

// GetTenantIDToken devolve o tenant do JWT -- o autoritativo em qualquer rota
// autenticada. Usar GetTenantID (header) aqui deixaria um administrador do
// tenant A escrever no tenant B só trocando o X-tenant-ID.
func GetTenantIDToken(c *gin.Context) (int64, bool) {
	val, exists := c.Get(UserTenantId)
	if !exists {
		return 0, false
	}
	id, ok := val.(int64)
	return id, ok
}

// GetUserID devolve o usuario.id do claim `sub`.
func GetUserID(c *gin.Context) (int64, bool) {
	val, exists := c.Get(UserId)
	if !exists {
		return 0, false
	}
	id, ok := val.(int64)
	return id, ok
}
