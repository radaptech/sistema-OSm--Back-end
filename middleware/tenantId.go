package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

const TenantId = "tenantId"

func TenantMiddleware(querie *repository.Queries) gin.HandlerFunc {

	return func(c *gin.Context) {

		subdominio := strings.TrimSpace(c.GetHeader("X-tenant-ID"))

		if subdominio == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Tenant não informado (X-Tenant-ID ausente)"})
			return
		}

		// 3. Ignora o'www' ou 'api' se vierem no header
		if subdominio == "www" || subdominio == "api" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Subdomínio reservado"})
			return
		}

		// Busca no banco (usando o Context da request para cancelamento/timeout)
		empresa, err := querie.ObterEmpresaPorSubdominio(c.Request.Context(), subdominio)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Empresa não encontrada"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Erro interno ao validar empresa"})
			return
		}
		// SUCESSO: Salva o ID dentro do contexto do GIN
		// O Gin tem um mapa chave/valor interno otimizado (c.Set/c.Get)
		c.Set(TenantId, empresa.ID)

		// Passa para o próximo handler
		c.Next()
	}
}

// Helper para pegar o ID dentro dos Controllers de forma tipada
func GetTenantID(c *gin.Context) (int64, bool) {
	val, exists := c.Get(TenantId)
	if !exists {
		return 0, false
	}
	id, ok := val.(int64)
	return id, ok
}
