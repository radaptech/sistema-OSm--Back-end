package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPermitir(t *testing.T) {

	gin.SetMode(gin.TestMode)

	casos := []struct {
		nome   string
		perfis []string
		atual  string // "" = AutenticacaoJwt não rodou
		passa  bool
	}{
		{"perfil unico bate", []string{"administrador"}, "administrador", true},
		{"perfil unico nao bate", []string{"administrador"}, "solicitante", false},
		{"um de varios", []string{"gestor", "tecnico", "administrador"}, "tecnico", true},
		{"nenhum de varios", []string{"gestor", "tecnico"}, "solicitante", false},
		// Falha fechada: middleware montado na ordem errada nega em vez de liberar.
		{"sem perfil no contexto", []string{"administrador"}, "", false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if c.atual != "" {
				ctx.Set(UserPerfil, c.atual)
			}

			Permitir(c.perfis...)(ctx)

			if c.passa {
				if ctx.IsAborted() {
					t.Fatalf("perfil %q devia passar em %v, status %d", c.atual, c.perfis, w.Code)
				}
				return
			}
			if !ctx.IsAborted() {
				t.Fatalf("perfil %q não devia passar em %v", c.atual, c.perfis)
			}
			// 403 e não 401: 401 desloga o usuário no front.
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, esperado 403", w.Code)
			}
		})
	}
}
