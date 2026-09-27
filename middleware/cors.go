package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CorsConfig() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			origin = strings.TrimSuffix(origin, "/")

			// Libera localhost (ex: http://localhost:3000, http://localhost)
			if origin == "http://localhost" || strings.HasPrefix(origin, "http://localhost:") {
				return true
			}

			// Libera subdomínios .localhost (ex: http://app.localhost, http://api.localhost)
			if strings.HasSuffix(origin, ".localhost") || strings.Contains(origin, ".localhost:") {
				return true
			}

			// Libera o domínio de produção/homologação
			if origin == "https://radaptech.com.br" ||
				strings.HasSuffix(origin, ".radaptech.com.br") ||
				strings.Contains(origin, ".radaptech.com.br:") {
				return true
			}

			return false
		},

		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},

		// 👉 ADICIONADOS: Headers em minúsculo/maiúsculo e wildcard de content-type
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Content-Length",
			"Accept",
			"Authorization",
			"X-Requested-With",
			"X-Tenant-ID",
			"X-tenant-id",
			"X-tenant-ID",
			"x-tenant-id",
		},

		ExposeHeaders: []string{"Content-Length", "Content-Disposition", "Set-Cookie"},

		AllowCredentials: true,
		// Garanta que o preflight permita repassar os headers na requisição OPTIONS
		OptionsResponseStatusCode: http.StatusNoContent, // 204
		MaxAge:                    12 * time.Hour,
	})
}
