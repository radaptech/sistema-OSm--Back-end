package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	bucketr2 "github.com/radaptech/sistema-OSm--Back-end/bucketR2"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// ttlFotoMaquina é a validade da URL assinada que sai na resposta. Curto de
// propósito: a URL é gerada a cada leitura e nunca guardada, então só precisa
// durar o tempo de a tela carregar a imagem.
const ttlFotoMaquina = 15 * time.Minute

type MaquinaServicesInterface interface {
	CadastrarMaquina(ctx context.Context, tenantId int64, payload model.MaquinarioInsert) (model.Maquinario, error)
	ListarMaquinario(ctx context.Context, tenantId, usuarioId int64, perfil string, lojaId, setorId *int64) ([]model.Maquinario, error)
	ObterMaquina(ctx context.Context, tenantID, id int64) (model.Maquinario, error)
	AtualizarMaquina(ctx context.Context, tenantId, id int64, payload model.AtualizarMaquina) (model.Maquinario, error)
	DesativarMaquina(ctx context.Context, tenantId, id int64) error
}

// bucketFotos é o bucket do R2 onde a foto da máquina vive. Vem de fora (o
// router lê R2_BUCKET_NAME_MAQUINARIO) porque cada tipo de anexo tem o seu, e
// a escolha é de wiring, não de linha do banco -- não existe coluna `bucket`.
type MaquinaController struct {
	service     MaquinaServicesInterface
	bucketFotos string
}

func NewMaquinaController(service MaquinaServicesInterface, bucketFotos string) *MaquinaController {

	return &MaquinaController{
		service:     service,
		bucketFotos: bucketFotos,
	}
}

// resolverFoto troca, no lugar, a chave do R2 pela URL assinada de leitura. O
// service devolve a chave crua (MontarListaMaquinarios copia foto_chave
// direto): ela não abre nada no browser e não deve sair daqui.
//
// Falhar a assinatura não derruba a resposta -- sem foto a tela ainda serve
// (fotoUrl é opcional no tipo do front), com 500 não sobra nada. Vale tanto
// depois do commit do POST quanto no meio de uma listagem.
func (m *MaquinaController) resolverFoto(ctx context.Context, maquina *model.Maquinario) {

	if maquina.FotoUrl == nil {
		return
	}

	url, err := bucketr2.URLLeitura(ctx, m.bucketFotos, *maquina.FotoUrl, ttlFotoMaquina)
	if err != nil {
		log.Printf("assinar url da foto maquina=%d: %v", maquina.Id, err)
		maquina.FotoUrl = nil
		return
	}

	maquina.FotoUrl = &url
}

// chaveDaFoto sobe a parte `foto` pro R2 e devolve a key. Ausência não é erro:
// o Zod do front não exige foto no cadastro de máquina (só na solicitação), e
// no PUT nil quer dizer "não troquei a foto" -- a query preserva a atual.
//
// Sobe ANTES da transação da máquina: falhar aqui não deixa resíduo no banco.
// O contrário vale só pro outro lado -- transação falhando depois deixa um
// objeto órfão no R2, que é lixo barato e sem referência; gravar linha
// apontando pra objeto que não subiu seria pior.
func (m *MaquinaController) chaveDaFoto(ctx *gin.Context, tenantId int64) (*string, bool) {

	header, err := ctx.FormFile("foto")
	if err != nil {

		if errors.Is(err, http.ErrMissingFile) {
			return nil, true
		}

		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":    "erro ao ler a foto enviada",
			"detalhes": err.Error(),
		})
		return nil, false
	}

	chave, err := bucketr2.UploadFoto(ctx.Request.Context(), tenantId, m.bucketFotos, header)
	if err != nil {
		log.Printf("upload foto maquina tenant=%d: %v", tenantId, err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar a foto"})
		return nil, false
	}

	return &chave, true
}

// Cadastrar é POST /maquinas.
func (m *MaquinaController) Cadastrar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoMultipart[model.MaquinarioInsert](ctx, bucketr2.TamanhoMaximoFoto)
		if !ok {
			return
		}

		chave, ok := m.chaveDaFoto(ctx, tenantId)
		if !ok {
			return
		}
		input.FotoChave = chave

		maquina, err := m.service.CadastrarMaquina(ctx.Request.Context(), tenantId, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "já existe uma máquina com esse número de patrimônio"})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("cadastrar maquina tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cadastrar máquina"})
			}
			return
		}

		m.resolverFoto(ctx.Request.Context(), &maquina)

		ctx.JSON(http.StatusCreated, maquina)
	}
}

// ListarMaquinas é GET /maquinas, com ?lojaId= e ?setorId= opcionais e
// combináveis (ParametrosListagemMaquinas no front): o Solicitante pede as
// máquinas do próprio setor em Nova Solicitação, o Administrador pede todas.
// Array simples, sem paginação -- o front pagina no cliente.
func (m *MaquinaController) ListarMaquinas() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		lojaId, ok := idDeQuery(ctx, "lojaId")
		if !ok {
			return
		}

		setorId, ok := idDeQuery(ctx, "setorId")
		if !ok {
			return
		}

		// Quem chama define o recorte: os filtros da query só estreitam o que o
		// escopo já permite. Sem isto um solicitante lista o tenant inteiro só
		// omitindo o ?setorId= que o front manda.
		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		maquinas, err := m.service.ListarMaquinario(ctx.Request.Context(), tenantId, usuarioId, perfil, lojaId, setorId)
		if err != nil {

			log.Printf("listar maquinas tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar máquinas"})
			return
		}

		// Índice, não valor: `for _, maquina := range` daria uma cópia e a
		// assinatura da URL não voltaria pro slice que vai na resposta.
		for i := range maquinas {
			m.resolverFoto(ctx.Request.Context(), &maquinas[i])
		}

		ctx.JSON(http.StatusOK, maquinas)
	}
}

// Obter é GET /maquinas/:id -- o que a tela de edição carrega. Sem filtro de
// `ativa` no service, de propósito: o formulário precisa ler a máquina mesmo
// desativada.
func (m *MaquinaController) Obter() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		maquina, err := m.service.ObterMaquina(ctx.Request.Context(), tenantId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				log.Printf("obter maquina id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter máquina"})
			}
			return
		}

		m.resolverFoto(ctx.Request.Context(), &maquina)

		ctx.JSON(http.StatusOK, maquina)
	}
}

// Atualizar é PUT /maquinas/:id. Mesmo corpo do POST, então o mapa de erro é o
// mesmo mais o 404 do id da rota.
//
// ErrNaoEncontrado aqui é 404 e não 422 (diferente de PUT /usuarios): o único
// registro que pode faltar é a própria máquina. Setor inexistente ou de outro
// tenant não passa pela FK composta e chega como ErrConflitoIntegridade.
func (m *MaquinaController) Atualizar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoMultipart[model.AtualizarMaquina](ctx, bucketr2.TamanhoMaximoFoto)
		if !ok {
			return
		}

		// nil = não mandou foto nova, e a query mantém a atual (COALESCE). A
		// antiga fica órfã no R2 quando a foto é trocada -- ver maquina.sql.
		chave, ok := m.chaveDaFoto(ctx, tenantId)
		if !ok {
			return
		}
		input.FotoChave = chave

		maquina, err := m.service.AtualizarMaquina(ctx.Request.Context(), tenantId, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "já existe uma máquina com esse número de patrimônio"})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("atualizar maquina id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar máquina"})
			}
			return
		}

		m.resolverFoto(ctx.Request.Context(), &maquina)

		ctx.JSON(http.StatusOK, maquina)
	}
}

// Desativar é DELETE /maquinas/:id -- soft delete (ativa = false), como em
// loja e setor. Sem cascata nas preventivas: elas apontam pra máquina e ficam
// como estão, junto com o histórico de OS.
func (m *MaquinaController) Desativar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		if err := m.service.DesativarMaquina(ctx.Request.Context(), tenantId, id); err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("desativar maquina id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao desativar máquina"})
			}
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"message": "máquina desativada"})
	}
}
