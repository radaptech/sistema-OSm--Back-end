package router

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/controller"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/service"
	"golang.org/x/time/rate"
	"github.com/radaptech/ginmw"
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
	Recuper *controller.RecuperacaoSenhaController
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
	// Sem RESEND_API_KEY o pedido responde 200 e só o envio falha, no log -- igual o notificador sem Evolution API.
	serviceRecuperacao := service.NewRepoRecuperacaoSenha(db, service.NewEmailService(
		os.Getenv("RESEND_API_KEY"),
		os.Getenv("URL_FRONTEND_FORMATO"),
	))

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
		Recuper: controller.NewRecuperacaoSenhaController(serviceRecuperacao),
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

	// Montados uma vez só: cada um segura o segredo/lookup fechado no
	// closure, reaproveitado em toda rota que precisa -- evita reler
	// JWT_SECRET (e repanicar a checagem) uma vez por rota.
	authJWT := ginmw.JWT([]byte(os.Getenv("JWT_SECRET")))
	tenant := ginmw.Tenant(func(ctx context.Context, subdominio string) (int64, error) {
		empresa, err := c.queries.ObterEmpresaPorSubdominio(ctx, subdominio)
		return empresa.ID, err
	}, pgx.ErrNoRows)

	autenticacao := api.Group("/autenticacao")

	// Limiter antes do Tenant: força bruta barrada não paga um
	// ObterEmpresaPorSubdominio (ida ao banco) por tentativa.
	autenticacao.POST("/login", ginmw.RateLimit(rate.Every(12*time.Second), 5), tenant, c.Login.Login())
	autenticacao.POST("/logout", c.Login.Logout())
	autenticacao.GET("/sessao", authJWT, c.Login.Sessao())
	// Mais apertado que o login: cada pedido aceito dispara um e-mail real para a caixa de alguém.
	autenticacao.POST("/esqueci-senha", ginmw.RateLimit(rate.Every(time.Minute), 3), tenant, c.Recuper.EsqueciSenha())
	autenticacao.POST("/redefinir-senha", ginmw.RateLimit(rate.Every(12*time.Second), 5), tenant, c.Recuper.RedefinirSenha())

	usuarios := api.Group("/usuarios", authJWT)
	usuarios.POST("", ginmw.Require("administrador"), c.Login.Registrar())
	usuarios.GET("", ginmw.Require("administrador"), c.Login.ListarUsuarios())
	usuarios.GET("/:id", ginmw.Require("administrador"), c.Login.Obter())
	usuarios.PUT("/:id", ginmw.Require("administrador"), c.Login.Atualizar())
	usuarios.DELETE("/:id", ginmw.Require("administrador"), c.Login.Desativar())

	// Empresa não tem CRUD (o tenant nasce pela CLI de provisionamento): esta
	// rota existe só para o select de Empresa no cadastro de loja, e por isso
	// mora no controller de loja.
	api.GET("/empresas", authJWT, ginmw.Require("administrador"), c.Loja.ListarEmpresas())

	// Projeção somente-leitura sobre `usuario` -- por isso mora no LoginController,
	// como GET /empresas mora no de loja. Gestor porque é ele quem escolhe o
	// Técnico Responsável ao abrir a OS; administrador para a tela de cadastro.
	// Técnico e solicitante não têm o que fazer com a lista.
	api.GET("/tecnicos", authJWT, ginmw.Require("gestor", "administrador"), c.Login.ListarTecnicos())

	lojas := api.Group("/lojas", authJWT)
	// Listar fica sem Permitir de propósito: o gestor precisa da lista para
	// montar os blocos por loja do painel, e o solicitante/técnico veem o nome
	// da loja nas telas deles. Escrever é só do administrador.
	lojas.GET("", c.Loja.Listar())
	lojas.GET("/:id", ginmw.Require("administrador"), c.Loja.Obter())
	lojas.POST("", ginmw.Require("administrador"), c.Loja.Cadastrar())
	lojas.PUT("/:id", ginmw.Require("administrador"), c.Loja.Atualizar())
	lojas.DELETE("/:id", ginmw.Require("administrador"), c.Loja.Desativar())

	setores := api.Group("/setores", authJWT)
	// Listar sem Permitir pelo mesmo motivo de /lojas: o painel do gestor nomeia
	// os blocos por setor e o cadastro de máquina/usuário usa o select em
	// cascata. Escrever é só do administrador.
	setores.GET("", c.Setor.Listar())
	setores.GET("/:id", ginmw.Require("administrador"), c.Setor.Obter())
	setores.POST("", ginmw.Require("administrador"), c.Setor.Cadastrar())
	setores.PUT("/:id", ginmw.Require("administrador"), c.Setor.Atualizar())
	setores.DELETE("/:id", ginmw.Require("administrador"), c.Setor.Desativar())

	maquinas := api.Group("/maquinas", authJWT)
	// Listar sem Permitir pelo mesmo motivo de /lojas e /setores: o solicitante
	// escolhe a máquina do próprio setor em Nova Solicitação e o gestor lista as
	// dele no painel de indicadores -- o recorte por loja/setor é o WHERE da
	// query (?lojaId=/?setorId=), não o RBAC. Escrever é só do administrador.
	maquinas.GET("", c.Maquina.ListarMaquinas())
	// /:id é só do administrador, como em loja e setor: a única tela que lê uma
	// máquina inteira é o formulário de edição dele.
	maquinas.GET("/:id", ginmw.Require("administrador"), c.Maquina.Obter())
	maquinas.POST("", ginmw.Require("administrador"), c.Maquina.Cadastrar())
	maquinas.PUT("/:id", ginmw.Require("administrador"), c.Maquina.Atualizar())
	maquinas.DELETE("/:id", ginmw.Require("administrador"), c.Maquina.Desativar())

	preventivas := api.Group("/preventivas", authJWT)
	// Listar sem Permitir: a aba "Manutenção Prev." do painel do gestor vive
	// dela, e o escopo do gestor é o WHERE da query, não o RBAC. Escrever é só
	// do administrador -- o cadastro de preventiva é dele, junto com o da
	// máquina.
	preventivas.GET("", c.Prevent.Listar())
	preventivas.GET("/:id", ginmw.Require("administrador"), c.Prevent.Obter())
	// Este POST é só a preventiva avulsa (ModalManutencaoPreventiva). As
	// preventivas do formulário de máquina não passam por aqui: viajam dentro
	// de POST/PUT /maquinas e gravam na mesma transação da máquina.
	preventivas.POST("", ginmw.Require("administrador"), c.Prevent.Cadastrar())
	preventivas.PUT("/:id", ginmw.Require("administrador"), c.Prevent.Atualizar())
	preventivas.DELETE("/:id", ginmw.Require("administrador"), c.Prevent.Desativar())

	terceirizadas := api.Group("/empresas-terceirizadas", authJWT)
	// Listar é do TÉCNICO e do administrador: é o Técnico quem escolhe a empresa
	// no ModalAcionarTerceiro -- terceirizar é decisão dele, não do Gestor
	// (front-end/CLAUDE.md item 9). Sem escopo no WHERE: a entidade não pende de
	// loja nem setor, é do tenant inteiro. Escrever é só do administrador.
	terceirizadas.GET("", ginmw.Require("tecnico", "administrador"), c.Terceir.Listar())
	terceirizadas.GET("/:id", ginmw.Require("administrador"), c.Terceir.Obter())
	terceirizadas.POST("", ginmw.Require("administrador"), c.Terceir.Cadastrar())
	terceirizadas.PUT("/:id", ginmw.Require("administrador"), c.Terceir.Atualizar())
	terceirizadas.DELETE("/:id", ginmw.Require("administrador"), c.Terceir.Desativar())

	solicitacoes := api.Group("/solicitacoes", authJWT)
	// As duas criações são só do Solicitante -- é quem preenche NovaSolicitacao
	// no front (front-end/CLAUDE.md), a única tela que chama estas rotas.
	solicitacoes.POST("/maquinario", ginmw.Require("solicitante"), c.Solicit.CriarMaquinario())
	solicitacoes.POST("/reparo", ginmw.Require("solicitante"), c.Solicit.CriarReparo())
	// Minhas e Resumo são sempre "o que é meu" -- o service nem recebe perfil,
	// só o usuario.id do token (mesmo motivo de GET /lojas e /setores ficarem
	// sem Permitir: o recorte já está no que a query pede, não no RBAC).
	solicitacoes.GET("/minhas", c.Solicit.Minhas())
	solicitacoes.GET("/resumo", c.Solicit.Resumo())
	// A fila é do Gestor (e Administrador) -- Técnico não participa da
	// aprovação, só recebe a OS depois que ela existe.
	solicitacoes.GET("", ginmw.Require("gestor", "administrador"), c.Solicit.Listar())
	// :id é aberto a qualquer perfil autenticado, recortado pelo escopo de quem
	// chama (ver ObterSolicitacaoPorID em solicitacao_os.sql) -- o Solicitante
	// abre o próprio pedido em Minhas Solicitações, o Gestor o dele na fila.
	solicitacoes.GET("/:id", c.Solicit.Obter())
	// abrir-os/rejeitar são a decisão do Gestor sobre a fila -- mesmo RBAC de
	// GET /solicitacoes.
	solicitacoes.POST("/:id/abrir-os", ginmw.Require("gestor", "administrador"), c.Solicit.AbrirOS())
	solicitacoes.POST("/:id/rejeitar", ginmw.Require("gestor", "administrador"), c.Solicit.Rejeitar())

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
	// A OS não NASCE aqui, nasce de POST /solicitacoes/:id/abrir-os (a
	// aprovação do Gestor, logo acima) -- uq_os_solicitacao garante que toda
	// OS vem de uma solicitação, e criar direto pularia a aprovação. O ciclo
	// de vida dela (iniciar/pausar/retomar/acionar-terceiro/encerrar), sim,
	// é POST em sub-recurso, logo abaixo -- nunca PATCH de um campo `status`
	// genérico, mesmo critério do resto da API.
	api.GET("/ordens-servico", authJWT, ginmw.Require("gestor", "administrador", "tecnico"), c.OrdemOS.Listar())

	// Ciclo de vida da OS -- só o Técnico DONO (checagem no service, não
	// aqui: OS de outro técnico é 404, não 403 -- ver a nota em
	// ObterOrdemServicoPorID).
	acoesOS := api.Group("/ordens-servico/:id", authJWT, ginmw.Require("tecnico"))
	acoesOS.POST("/iniciar", c.OrdemOS.Iniciar())
	acoesOS.POST("/pausar", c.OrdemOS.Pausar())
	acoesOS.POST("/retomar", c.OrdemOS.Retomar())
	acoesOS.POST("/acionar-terceiro", c.OrdemOS.AcionarTerceiro())
	acoesOS.POST("/encerrar", c.OrdemOS.Encerrar())

	// custo é do Administrador, não do Técnico -- correção pós-encerramento
	// (AdministradorCustosPendentes), por isso fora do grupo acoesOS acima,
	// que é Permitir("tecnico"). Sem dono pra checar (diferente do resto do
	// ciclo de vida): o RBAC da rota já é toda a restrição.
	api.POST("/ordens-servico/:id/custo", authJWT, ginmw.Require("administrador"), c.OrdemOS.Custo())

	// GET /indicadores/maquinas/:id -- o Painel de Indicadores (DashboardGestor,
	// a ação rápida "Indicadores" do Painel do Gestor). O `:id` é de MÁQUINA;
	// quem responde é o OrdemServicoController porque o painel inteiro sai do
	// histórico de OS encerradas.
	//
	// Gestor e administrador, mesmo par de GET /solicitacoes: o Técnico executa
	// a OS, não acompanha indisponibilidade nem custo, e o Solicitante muito
	// menos. O recorte de loja/setor continua sendo o WHERE (aqui, o EXISTS de
	// ObterMaquinaPorID) -- máquina fora do escopo é 404, não lista vazia.
	//
	// /indicadores em vez de /maquinas/:id/indicadores porque é a URL que o
	// front já chama (servicos/servicoIndicadores.ts), e o contrato manda.
	api.GET("/indicadores/maquinas/:id", authJWT, ginmw.Require("gestor", "administrador"), c.OrdemOS.Indicadores())
}
