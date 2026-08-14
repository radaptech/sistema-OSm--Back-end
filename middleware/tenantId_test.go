package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Só os ramos que abortam antes de tocar no banco + o cast de GetTenantID
// (que estava em int32 e nunca casava com o bigint do empresa.id).
func TestTenantMiddlewareHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	casos := []struct {
		header string
		status int
	}{
		{"", http.StatusBadRequest},
		{"  ", http.StatusBadRequest},
		{"www", http.StatusForbidden},
		{"api", http.StatusForbidden},
	}

	for _, c := range casos {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		ctx.Request.Header.Set("X-tenant-ID", c.header)

		TenantMiddleware(nil)(ctx)

		if w.Code != c.status {
			t.Errorf("header %q: status %d, esperado %d", c.header, w.Code, c.status)
		}
	}
}

func TestGetTenantID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	if _, ok := GetTenantID(ctx); ok {
		t.Error("sem tenant no contexto deveria devolver false")
	}

	ctx.Set(TenantId, int64(42))
	if id, ok := GetTenantID(ctx); !ok || id != 42 {
		t.Errorf("GetTenantID = (%d, %v), esperado (42, true)", id, ok)
	}
}
