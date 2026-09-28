package middleware

import (
	"context"
	"crypto/rand"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type chaveRequestID struct{}

// NovoLogger é o logger JSON da API. Qualquer slog.XxxContext(ctx, ...) com o
// ctx de um request sai com o request_id dele, sem ninguém precisar repassar.
func NovoLogger() *slog.Logger {
	return slog.New(handlerComRequestID{slog.NewJSONHandler(os.Stdout, nil)})
}

type handlerComRequestID struct{ slog.Handler }

func (h handlerComRequestID) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := ctx.Value(chaveRequestID{}).(string); ok {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h handlerComRequestID) WithAttrs(attrs []slog.Attr) slog.Handler {
	return handlerComRequestID{h.Handler.WithAttrs(attrs)}
}

func (h handlerComRequestID) WithGroup(nome string) slog.Handler {
	return handlerComRequestID{h.Handler.WithGroup(nome)}
}

// LogRequest substitui o logger texto do gin.Default: uma linha JSON por request.
// O X-Request-ID volta na resposta para o suporte achar o request pelo que o
// cliente viu. Tenant e usuário só existem depois do JWT, por isso são lidos no fim.
func LogRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		inicio := time.Now()
		id := rand.Text()
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), chaveRequestID{}, id))
		c.Header("X-Request-ID", id)

		c.Next()

		status := c.Writer.Status()
		nivel := slog.LevelInfo
		if status >= 500 {
			nivel = slog.LevelError
		}

		attrs := []slog.Attr{
			slog.String("metodo", c.Request.Method),
			slog.String("rota", c.FullPath()),
			slog.Int("status", status),
			slog.Int64("duracao_ms", time.Since(inicio).Milliseconds()),
			slog.String("ip", c.ClientIP()),
		}
		if tenant, ok := GetTenantIDToken(c); ok {
			attrs = append(attrs, slog.Int64("tenant", tenant))
		}
		if usuario, ok := GetUserID(c); ok {
			attrs = append(attrs, slog.Int64("usuario", usuario))
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("erros", c.Errors.String()))
		}

		slog.LogAttrs(c.Request.Context(), nivel, "request", attrs...)
	}
}
