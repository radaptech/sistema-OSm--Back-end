package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// O valor do middleware é o prazo chegar no ctx que o controller repassa pro
// pgx. Se alguém trocar por c.Set(...) ou esquecer o WithContext, isso quebra.
func TestTimeoutChegaNoContextoDoHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var prazo time.Duration
	var temPrazo bool

	r := gin.New()
	r.Use(Timeout(30 * time.Second))
	r.GET("/", func(c *gin.Context) {
		var limite time.Time
		limite, temPrazo = c.Request.Context().Deadline()
		prazo = time.Until(limite)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if !temPrazo {
		t.Fatal("handler recebeu contexto sem deadline")
	}
	if prazo <= 29*time.Second || prazo > 30*time.Second {
		t.Fatalf("deadline de %v, esperado ~30s", prazo)
	}
}

// O 500 do handler vira 504 quando o prazo estourou -- e continua 500 quando é
// bug de verdade. Se o writer parar de ser embrulhado, os dois casos colapsam
// em 500 de novo e ninguém percebe até o gráfico de erros mentir.
func TestTimeoutTraduz500Em504(t *testing.T) {
	gin.SetMode(gin.TestMode)

	casos := []struct {
		nome     string
		espera   time.Duration
		status   int
		corpo    string
		esperado int
	}{
		{"prazo estourado vira 504", 20 * time.Millisecond, 504, `{"error":"tempo de resposta esgotado"}`, 504},
		{"erro real segue 500", 0, 500, `{"error":"erro ao logar usuario"}`, 500},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			r := gin.New()
			r.Use(Timeout(10 * time.Millisecond))
			r.GET("/", func(c *gin.Context) {
				time.Sleep(caso.espera)
				c.JSON(500, gin.H{"error": "erro ao logar usuario"})
			})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

			if w.Code != caso.esperado {
				t.Errorf("status %d, esperado %d", w.Code, caso.esperado)
			}
			if w.Body.String() != caso.corpo {
				t.Errorf("corpo %q, esperado %q", w.Body.String(), caso.corpo)
			}
		})
	}
}
