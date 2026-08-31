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
	Solicit *controller.SolicitacaoController
	OrdemOS *controller.OrdemServicoController
}

func NewContainer(db *pgxpool.Pool) *Container {

	serviceLogin := service.NewRepoUsuario(db)
	serviceLoja := service.NewRepoLojas(db)
	serviceSetor := service.NewRepoSetor(db)
	serviceMaquina := service.NewRepoMaquinario(db)
	servicePreventiva := service.NewRepoPreventiva(db)
	serviceTerceirizada := service.NewRepoEmpresaTerceirizada(db)
	serviceSolicitacao := service.NewRepoSolicitacao(db)
	serviceOrdemServico := service.NewRepoOrdemServico(db)

	// Notificador é opcional (campo público, não parâmetro de construtor -- ver
	// o comentário em SolicitacaoService/PreventivaService): URL vazia faz
	// NotificacaoService tentar chamar "" e falhar por request, exatamente como
	// bucketMaquinas vazio só quebra o upload sem derrubar o resto. Ver
	// CLAUDE.md, "Notificação de solicitação por WhatsApp".
	notificador := service.NewRepoNotificacao(db,
		os.Getenv("EVOLUTION_API_URL"),
		os.Getenv("EVOLUTION_API_KEY"),
		os.Getenv("EVOLUTION_INSTANCE_NAME"),
	)
	serviceSolicitacao.Notificador = notificador
	servicePreventiva.Notificador = notificador

	// O bucket do R2 é escolhido aqui, no wiring, e não guardado por linha: cada
	// tipo de anexo tem o seu (ver .env-example) e não existe coluna `bucket`.
	// Vazio só quebra o upload da foto -- o CRUD de máquina segue funcionando.
	bucketMaquinas := os.Getenv("R2_BUCKET_NAME_MAQUINARIO")
	// Anexo de solicitação de maquinário vira OS (bucket "de Serviço"); anexo de
	// pequeno reparo tem o dele -- os dois vazios têm o mesmo efeito de
	// bucketMaquinas vazio: só o upload quebra, o resto do CRUD segue.
	bucketOsServico := os.Getenv("R2_BUCKET_NAME_OS_SERVICO")
	bucketPequenosReparos := os.Getenv("R2_BUCKET_NAME_PEQUENOS_REPAROS")

	return &Container{
		Login:   controller.NewLoginController(serviceLogin),
		Loja:    controller.NewLojaController(serviceLoja),
		Setor:   controller.NewSetorController(serviceSetor),
		Maquina: controller.NewMaquinaController(serviceMaquina, bucketMaquinas),
		Prevent: controller.NewPreventivaController(servicePreventiva),
		Terceir: controller.NewEmpresaTerceirizadaController(serviceTerceirizada),
		Solicit: controller.NewSolicitacaoController(serviceSolicitacao, bucketOsServico, bucketPequenosReparos, bucketMaquinas),
		OrdemOS: controller.NewOrdemServicoController(serviceOrdemServico),
		queries: repository.New(db),
	}
}

func ConfigurarRotas(r *gin.Engine, c *Container) {

	api := r.Group("/api")

	api.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "online",
			"message": "API operando normalmente. Acesse a documentação do Swagger para ver as rotas.",
			"version": "v1",
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

	solicitacoes := api.Group("/solicitacoes", middleware.AutenticacaoJwt())
	// As duas criações são só do Solicitante -- é quem preenche NovaSolicitacao
	// no front (front-end/CLAUDE.md), a única tela que chama estas rotas.
	solicitacoes.POST("/maquinario", middleware.Permitir("solicitante"), c.Solicit.CriarMaquinario())
	solicitacoes.POST("/reparo", middleware.Permitir("solicitante"), c.Solicit.CriarReparo())
	// Minhas e Resumo são sempre "o que é meu" -- o service nem recebe perfil,
	// só o usuario.id do token (mesmo motivo de GET /lojas e /setores ficarem
	// sem Permitir: o recorte já está no que a query pede, não no RBAC).
	solicitacoes.GET("/minhas", c.Solicit.Minhas())
	solicitacoes.GET("/resumo", c.Solicit.Resumo())
	// A fila é do Gestor (e Administrador) -- Técnico não participa da
	// aprovação, só recebe a OS depois que ela existe.
	solicitacoes.GET("", middleware.Permitir("gestor", "administrador"), c.Solicit.Listar())
	// :id é aberto a qualquer perfil autenticado, recortado pelo escopo de quem
	// chama (ver ObterSolicitacaoPorID em solicitacao_os.sql) -- o Solicitante
	// abre o próprio pedido em Minhas Solicitações, o Gestor o dele na fila.
	solicitacoes.GET("/:id", c.Solicit.Obter())
	// abrir-os/rejeitar são a decisão do Gestor sobre a fila -- mesmo RBAC de
	// GET /solicitacoes.
	solicitacoes.POST("/:id/abrir-os", middleware.Permitir("gestor", "administrador"), c.Solicit.AbrirOS())
	solicitacoes.POST("/:id/rejeitar", middleware.Permitir("gestor", "administrador"), c.Solicit.Rejeitar())

	// GET /ordens-servico serve os TRÊS painéis, e o que muda é o filtro que
	// cada um manda: o Gestor acompanha as OS do escopo dele (abas "OS em
	// Andamento"/"OS Finalizadas"), o Técnico as dele (?tecnicoId=), o
	// Administrador as do tenant (?status=Concluída em Custos Pendentes,
	// ?finalizada=true em OS Finalizadas). Solicitante fica de fora: ele
	// acompanha o pedido dele em /solicitacoes/minhas, não a execução.
	//
	// O recorte de loja/setor é o WHERE da query (escopoDe + EXISTS sobre
	// usuario_escopo), não este Permitir -- ele só decide QUEM entra, nunca O
	// QUE cada um vê. Mesmo desenho de /solicitacoes, /maquinas e /preventivas.
	//
	// Sem POST: a OS não nasce aqui, nasce de POST /solicitacoes/:id/abrir-os
	// (a aprovação do Gestor, logo acima) -- uq_os_solicitacao garante que toda
	// OS vem de uma solicitação, e criar direto pularia a aprovação. O ciclo de
	// vida (iniciar/pausar/retomar/acionar-terceiro/encerrar/custo) é a fase 2.
	api.GET("/ordens-servico", middleware.AutenticacaoJwt(), middleware.Permitir("gestor", "administrador", "tecnico"), c.OrdemOS.Listar())
}
