package middleware

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
)

// Permitir barra a rota pra quem não tem um dos perfis listados. Roda sempre
// depois de AutenticacaoJwt, que é quem põe UserPerfil no contexto.
//
// 403, nunca 401: 401 é só "sem sessão" e o front desloga o usuário em
// qualquer 401 fora do login -- um técnico clicando onde não devia seria
// expulso do sistema em vez de ver "sem permissão".
//
// Falha fechada: sem perfil no contexto (AutenticacaoJwt não rodou antes)
// nenhum perfil casa e a rota nega.
func Permitir(perfis ...string) gin.HandlerFunc {

	return func(ctx *gin.Context) {

		if !slices.Contains(perfis, ctx.GetString(UserPerfil)) {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Acesso negado: privilégios insuficientes",
			})
			return
		}

		ctx.Next()
	}
}
