package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// visitante guarda o limiter individual de um IP e quando ele foi usado
// pela última vez, para permitir a limpeza de entradas antigas do mapa.
type visitante struct {
	limiter   *rate.Limiter
	ultimoUso time.Time
}

// rateLimiter mantém um limiter (token bucket) por IP.
type rateLimiter struct {
	mu         sync.Mutex
	visitantes map[string]*visitante
	taxa       rate.Limit
	burst      int
}

func novoRateLimiter(taxa rate.Limit, burst int) *rateLimiter {
	rl := &rateLimiter{
		visitantes: make(map[string]*visitante),
		taxa:       taxa,
		burst:      burst,
	}
	go rl.limparInativos()
	return rl
}

func (rl *rateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, existe := rl.visitantes[ip]
	if !existe {
		limiter := rate.NewLimiter(rl.taxa, rl.burst)
		rl.visitantes[ip] = &visitante{limiter: limiter, ultimoUso: time.Now()}
		return limiter
	}

	v.ultimoUso = time.Now()
	return v.limiter
}

// limparInativos evita que o mapa cresça indefinidamente com IPs que
// pararam de fazer requisições.
func (rl *rateLimiter) limparInativos() {
	for {
		time.Sleep(5 * time.Minute)

		rl.mu.Lock()
		for ip, v := range rl.visitantes {
			if time.Since(v.ultimoUso) > 10*time.Minute {
				delete(rl.visitantes, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// LimitarPorIP limita a quantidade de requisições por IP usando um token
// bucket: "taxa" é a reposição de tokens (ex: rate.Every(12*time.Second))
// e "burst" é o número de tentativas permitidas de uma vez antes de
// começar a bloquear. Pensado para proteger rotas sensíveis a força
// bruta ou abuso, como login e recuperação de senha.
func LimitarPorIP(taxa rate.Limit, burst int) gin.HandlerFunc {
	rl := novoRateLimiter(taxa, burst)

	return func(c *gin.Context) {
		limiter := rl.getLimiter(c.ClientIP())

		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Muitas tentativas. Aguarde um momento antes de tentar novamente.",
			})
			return
		}

		c.Next()
	}
}
