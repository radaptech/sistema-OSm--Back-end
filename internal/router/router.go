package router

import (
	"net/http"
	"os"
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
	Maquina *controller.MaquinaController
	Prevent *controller.PreventivaController
	Terceir *controller.EmpresaTerceirizadaController
}

func NewContainer(db *pgxpool.Pool) *Container {

	serviceLogin := service.NewRepoUsuario(db)
	serviceLoja := service.NewRepoLojas(db)
	serviceSetor := service.NewRepoSetor(db)
	serviceMaquina := service.NewRepoMaquinario(db)
	servicePreventiva := service.NewRepoPreventiva(db)
	serviceTerceirizada := service.NewRepoEmpresaTerceirizada(db)

	// O bucket do R2 é escolhido aqui, no wiring, e não guardado por linha: cada
	// tipo de anexo tem o seu (ver .env-example) e não existe coluna `bucket`.
	// Vazio só quebra o upload da foto -- o CRUD de máquina segue funcionando.
	bucketMaquinas := os.Getenv("R2_BUCKET_NAME_MAQUINARIO")

	return &Container{
		Login:   controller.NewLoginController(serviceLogin),
		Loja:    controller.NewLojaController(serviceLoja),
		Setor:   controller.NewSetorController(serviceSetor),
		Maquina: controller.NewMaquinaController(serviceMaquina, bucketMaquinas),
		Prevent: controller.NewPreventivaController(servicePreventiva),
		Terceir: controller.NewEmpresaTerceirizadaController(serviceTerceirizada),
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

	// Projeção somente-leitura sobre `usuario` -- por isso mora no LoginController,
	// como GET /empresas mora no de loja. Gestor porque é ele quem escolhe o
	// Técnico Responsável ao abrir a OS; administrador para a tela de cadastro.
	// Técnico e solicitante não têm o que fazer com a lista.
	api.GET("/tecnicos", middleware.AutenticacaoJwt(), middleware.Permitir("gestor", "administrador"), c.Login.ListarTecnicos())

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

	maquinas := api.Group("/maquinas", middleware.AutenticacaoJwt())
	// Listar sem Permitir pelo mesmo motivo de /lojas e /setores: o solicitante
	// escolhe a máquina do próprio setor em Nova Solicitação e o gestor lista as
	// dele no painel de indicadores -- o recorte por loja/setor é o WHERE da
	// query (?lojaId=/?setorId=), não o RBAC. Escrever é só do administrador.
	maquinas.GET("", c.Maquina.ListarMaquinas())
	// /:id é só do administrador, como em loja e setor: a única tela que lê uma
	// máquina inteira é o formulário de edição dele.
	maquinas.GET("/:id", middleware.Permitir("administrador"), c.Maquina.Obter())
	maquinas.POST("", middleware.Permitir("administrador"), c.Maquina.Cadastrar())
	maquinas.PUT("/:id", middleware.Permitir("administrador"), c.Maquina.Atualizar())
	maquinas.DELETE("/:id", middleware.Permitir("administrador"), c.Maquina.Desativar())

	preventivas := api.Group("/preventivas", middleware.AutenticacaoJwt())
	// Listar sem Permitir: a aba "Manutenção Prev." do painel do gestor vive
	// dela, e o escopo do gestor é o WHERE da query, não o RBAC. Escrever é só
	// do administrador -- o cadastro de preventiva é dele, junto com o da
	// máquina.
	preventivas.GET("", c.Prevent.Listar())
	preventivas.GET("/:id", middleware.Permitir("administrador"), c.Prevent.Obter())
	// Este POST é só a preventiva avulsa (ModalManutencaoPreventiva). As
	// preventivas do formulário de máquina não passam por aqui: viajam dentro
	// de POST/PUT /maquinas e gravam na mesma transação da máquina.
	preventivas.POST("", middleware.Permitir("administrador"), c.Prevent.Cadastrar())
	preventivas.PUT("/:id", middleware.Permitir("administrador"), c.Prevent.Atualizar())
	preventivas.DELETE("/:id", middleware.Permitir("administrador"), c.Prevent.Desativar())

	terceirizadas := api.Group("/empresas-terceirizadas", middleware.AutenticacaoJwt())
	// Listar é do TÉCNICO e do administrador: é o Técnico quem escolhe a empresa
	// no ModalAcionarTerceiro -- terceirizar é decisão dele, não do Gestor
	// (front-end/CLAUDE.md item 9). Sem escopo no WHERE: a entidade não pende de
	// loja nem setor, é do tenant inteiro. Escrever é só do administrador.
	terceirizadas.GET("", middleware.Permitir("tecnico", "administrador"), c.Terceir.Listar())
	terceirizadas.GET("/:id", middleware.Permitir("administrador"), c.Terceir.Obter())
	terceirizadas.POST("", middleware.Permitir("administrador"), c.Terceir.Cadastrar())
	terceirizadas.PUT("/:id", middleware.Permitir("administrador"), c.Terceir.Atualizar())
	terceirizadas.DELETE("/:id", middleware.Permitir("administrador"), c.Terceir.Desativar())
}
