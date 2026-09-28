package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// O valor do request_id é aparecer igual no header da resposta, no log que o
// handler escreve e na linha do request -- se um dos três divergir, o suporte
// não consegue ir do que o cliente viu até o erro.
func TestLogRequestPropagaRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var saida bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(handlerComRequestID{slog.NewJSONHandler(&saida, nil)}))
	defer slog.SetDefault(anterior)

	r := gin.New()
	r.Use(LogRequest())
	r.GET("/x", func(c *gin.Context) {
		slog.ErrorContext(c.Request.Context(), "falhou")
		c.Status(500)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))

	id := w.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("resposta sem X-Request-ID")
	}

	linhas := strings.Split(strings.TrimSpace(saida.String()), "\n")
	if len(linhas) != 2 {
		t.Fatalf("esperava 2 linhas de log, veio %d: %s", len(linhas), saida.String())
	}
	for _, l := range linhas {
		var reg map[string]any
		if err := json.Unmarshal([]byte(l), &reg); err != nil {
			t.Fatalf("log não é JSON: %s", l)
		}
		if reg["request_id"] != id {
			t.Fatalf("request_id %v, esperado %s: %s", reg["request_id"], id, l)
		}
	}
	if !strings.Contains(linhas[1], `"level":"ERROR"`) || !strings.Contains(linhas[1], `"status":500`) {
		t.Fatalf("linha do request deveria ser ERROR com status 500: %s", linhas[1])
	}
}
