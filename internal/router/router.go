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
	Loja    *controller.LojaController
	Setor   *controller.SetorController
}

func NewContainer(db *pgxpool.Pool) *Container {

	serviceLogin := service.NewRepoUsuario(db)
	serviceLoja := service.NewRepoLojas(db)
	serviceSetor := service.NewRepoSetor(db)

	return &Container{
		Login:   controller.NewLoginController(serviceLogin),
		Loja:    controller.NewLojaController(serviceLoja),
		Setor:   controller.NewSetorController(serviceSetor),
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

	// Empresa não tem CRUD (o tenant nasce pela CLI de provisionamento): esta
	// rota existe só para o select de Empresa no cadastro de loja, e por isso
	// mora no controller de loja.
	api.GET("/empresas", middleware.AutenticacaoJwt(), middleware.Permitir("administrador"), c.Loja.ListarEmpresas())

	lojas := api.Group("/lojas", middleware.AutenticacaoJwt())
	// Listar fica sem Permitir de propósito: o gestor precisa da lista para
	// montar os blocos por loja do painel, e o solicitante/técnico veem o nome
	// da loja nas telas deles. Escrever é só do administrador.
	lojas.GET("", c.Loja.Listar())
	lojas.GET("/:id", middleware.Permitir("administrador"), c.Loja.Obter())
	lojas.POST("", middleware.Permitir("administrador"), c.Loja.Cadastrar())
	lojas.PUT("/:id", middleware.Permitir("administrador"), c.Loja.Atualizar())
	lojas.DELETE("/:id", middleware.Permitir("administrador"), c.Loja.Desativar())

	setores := api.Group("/setores", middleware.AutenticacaoJwt())
	// Listar sem Permitir pelo mesmo motivo de /lojas: o painel do gestor nomeia
	// os blocos por setor e o cadastro de máquina/usuário usa o select em
	// cascata. Escrever é só do administrador.
	setores.GET("", c.Setor.Listar())
	setores.GET("/:id", middleware.Permitir("administrador"), c.Setor.Obter())
	setores.POST("", middleware.Permitir("administrador"), c.Setor.Cadastrar())
	setores.PUT("/:id", middleware.Permitir("administrador"), c.Setor.Atualizar())
	setores.DELETE("/:id", middleware.Permitir("administrador"), c.Setor.Desativar())
}
