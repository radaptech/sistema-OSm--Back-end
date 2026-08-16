package router

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/controller"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/service"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
	"golang.org/x/time/rate"
)

type Container struct {
	queries *repository.Queries
	Login   *controller.LoginController
}

func NewContainer(db *pgxpool.Pool) *Container {

	serviceLogin := service.NewRepoUsuario(db)

	return &Container{
		Login:   controller.NewLoginController(serviceLogin),
		queries: repository.New(db),
	}
}

func ConfigurarRotas(r *gin.Engine, c *Container) {

	api := r.Group("/api")

	api.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "online",
			"message": "API operando normalmente. Acesse a documentação do Swagger para ver as rotas.",
		})
	})

	autenticacao := api.Group("/autenticacao")

	// Limiter antes do TenantMiddleware: força bruta barrada não paga um
	// ObterEmpresaPorSubdominio (ida ao banco) por tentativa.
	autenticacao.POST("/login", middleware.LimitarPorIP(rate.Every(12*time.Second), 5), middleware.TenantMiddleware(c.queries), c.Login.Login())
	autenticacao.POST("/logout", c.Login.Logout())
	autenticacao.GET("/sessao", middleware.AutenticacaoJwt(), c.Login.Sessao())

	usuarios := api.Group("/usuarios", middleware.AutenticacaoJwt())
	usuarios.POST("", middleware.Permitir("administrador"), c.Login.Registrar())
	usuarios.GET("", middleware.Permitir("administrador"), c.Login.ListarUsuarios())
	usuarios.GET("/:id", middleware.Permitir("administrador"), c.Login.Obter())
	usuarios.PUT("/:id", middleware.Permitir("administrador"), c.Login.Atualizar())
	usuarios.DELETE("/:id", middleware.Permitir("administrador"), c.Login.Desativar())
}
