package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	bucketr2 "github.com/radaptech/sistema-OSm--Back-end/bucketR2"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type SolicitacaoServiceInterface interface {
	CadastrarSolicitacaoMaquinario(ctx context.Context, tenantId, solicitanteId int64, payload model.NovaSolicitacaoMaquinarioPayload) (model.SolicitacaoOS, error)
	CadastrarSolicitacaoReparo(ctx context.Context, tenantId, solicitanteId int64, payload model.NovaSolicitacaoReparoPayload) (model.SolicitacaoOS, error)
	ListarMinhasSolicitacoes(ctx context.Context, tenantId, solicitanteId int64, pagina int32, status, busca *string) (model.RespostaPaginada[model.SolicitacaoOS], error)
	ListarSolicitacoes(ctx context.Context, tenantId, usuarioId int64, perfil string, status, tipo, busca *string, lojaId *int64) ([]model.SolicitacaoOS, error)
	ObterSolicitacao(ctx context.Context, tenantId, usuarioId int64, perfil string, id int64) (model.SolicitacaoOS, error)
	ObterResumo(ctx context.Context, tenantId, solicitanteId int64) (model.ResumoSolicitacoes, error)
	AbrirOS(ctx context.Context, tenantId, atorId int64, perfil string, solicitacaoId int64, payload model.AberturaOrdemServicoPayload) (model.OrdemServico, error)
	Rejeitar(ctx context.Context, tenantId, atorId int64, perfil string, solicitacaoId int64, motivoBruto string) (model.SolicitacaoOS, error)
}

// SolicitacaoController guarda os três buckets que ele resolve: anexos de
// solicitação de maquinário (que pode virar OS), anexos de pequeno reparo, e
// a foto de CADASTRO da máquina (para SolicitacaoOS.maquinaFotoUrl -- o mesmo
// bucket que MaquinaController usa, resolvido aqui de novo porque a
// solicitação denormaliza a foto da máquina na resposta).
type SolicitacaoController struct {
	service               SolicitacaoServiceInterface
	bucketOsServico       string
	bucketPequenosReparos string
	bucketMaquinas        string
}

func NewSolicitacaoController(service SolicitacaoServiceInterface, bucketOsServico, bucketPequenosReparos, bucketMaquinas string) *SolicitacaoController {

	return &SolicitacaoController{
		service:               service,
		bucketOsServico:       bucketOsServico,
		bucketPequenosReparos: bucketPequenosReparos,
		bucketMaquinas:        bucketMaquinas,
	}
}

// chaveDoUpload sobe a parte `campo` do multipart pro bucket indicado e
// devolve key/mime/tamanho -- mesmo papel de MaquinaController.chaveDaFoto,
// generalizado pros dois campos que solicitação usa (foto obrigatória, vídeo
// opcional) e com uma checagem a mais: o content-type precisa começar com
// prefixoMime. O front já filtra por accept (UploadVideo.tsx,
// accept="video/*"), mas aceitar qualquer arquivo aqui seria confiar numa
// fronteira que o cliente controla.
//
// obrigatorio=false e ausência do campo devolve tudo zerado sem erro (é o
// caso do vídeo); obrigatorio=true e ausência é 400 (só a foto usa isto --
// diferente de POST /maquinas, aqui a foto do defeito é sempre obrigatória,
// é a evidência que o Gestor avalia antes de aprovar).
//
// Sobe ANTES da transação da solicitação, mesmo motivo de chaveDaFoto: falhar
// aqui não deixa resíduo no banco.
func (s *SolicitacaoController) chaveDoUpload(ctx *gin.Context, tenantId int64, bucket, campo, prefixoMime string, obrigatorio bool) (chave, mime string, tamanho int64, ok bool) {

	header, err := ctx.FormFile(campo)
	if err != nil {

		if errors.Is(err, http.ErrMissingFile) {
			if obrigatorio {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": campo + " é obrigatória"})
				return "", "", 0, false
			}
			return "", "", 0, true
		}

		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":    "erro ao ler " + campo + " enviada",
			"detalhes": err.Error(),
		})
		return "", "", 0, false
	}

	mime = header.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, prefixoMime) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": campo + " precisa ser um arquivo do tipo " + prefixoMime + "*"})
		return "", "", 0, false
	}

	chave, err = bucketr2.UploadFoto(ctx.Request.Context(), tenantId, bucket, header)
	if err != nil {
		log.Printf("upload %s tenant=%d: %v", campo, tenantId, err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar " + campo})
		return "", "", 0, false
	}

	return chave, mime, header.Size, true
}

// resolverSolicitacao troca, no lugar, as chaves do R2 pelas URLs assinadas de
// leitura: os anexos (bucket varia por tipo -- maquinário usa o bucket de OS
// de Serviço, reparo o de Pequenos Reparos) e a foto de cadastro da máquina
// (bucket de máquina). Mesmo padrão de MaquinaController.resolverFoto:
// falhar a assinatura não derruba a resposta inteira.
func (s *SolicitacaoController) resolverSolicitacao(ctx context.Context, sol *model.SolicitacaoOS) {

	bucket := s.bucketOsServico
	if sol.Tipo == "reparo" {
		bucket = s.bucketPequenosReparos
	}

	for i := range sol.Anexos {
		if sol.Anexos[i].Url == nil {
			continue
		}
		url, err := bucketr2.URLLeitura(ctx, bucket, *sol.Anexos[i].Url, ttlFotoMaquina)
		if err != nil {
			log.Printf("assinar url do anexo solicitacao=%d anexo=%d: %v", sol.Id, sol.Anexos[i].Id, err)
			// null é o degrade: melhor a tela reconhecer "sem mídia" do que
			// tentar carregar uma URL vazia como se fosse válida.
			sol.Anexos[i].Url = nil
			continue
		}
		sol.Anexos[i].Url = &url
	}

	if sol.MaquinaFotoUrl == nil {
		return
	}

	url, err := bucketr2.URLLeitura(ctx, s.bucketMaquinas, *sol.MaquinaFotoUrl, ttlFotoMaquina)
	if err != nil {
		log.Printf("assinar url da foto da maquina solicitacao=%d: %v", sol.Id, err)
		sol.MaquinaFotoUrl = nil
		return
	}
	sol.MaquinaFotoUrl = &url
}

func (s *SolicitacaoController) resolverSolicitacoes(ctx context.Context, lista []model.SolicitacaoOS) {
	for i := range lista {
		s.resolverSolicitacao(ctx, &lista[i])
	}
}

// CriarMaquinario é POST /solicitacoes/maquinario -- foto obrigatória, vídeo
// opcional, corpo maior que as demais rotas multipart (TamanhoMaximoComVideo)
// porque é a única que aceita vídeo.
func (s *SolicitacaoController) CriarMaquinario() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		solicitanteId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoMultipart[model.NovaSolicitacaoMaquinarioPayload](ctx, bucketr2.TamanhoMaximoComVideo)
		if !ok {
			return
		}

		fotoChave, fotoMime, fotoTamanho, ok := s.chaveDoUpload(ctx, tenantId, s.bucketOsServico, "foto", "image/", true)
		if !ok {
			return
		}
		input.FotoChave, input.FotoMime, input.FotoTamanho = fotoChave, fotoMime, fotoTamanho

		videoChave, videoMime, videoTamanho, ok := s.chaveDoUpload(ctx, tenantId, s.bucketOsServico, "video", "video/", false)
		if !ok {
			return
		}
		if videoChave != "" {
			input.VideoChave, input.VideoMime, input.VideoTamanho = &videoChave, &videoMime, &videoTamanho
		}

		solicitacao, err := s.service.CadastrarSolicitacaoMaquinario(ctx.Request.Context(), tenantId, solicitanteId, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("cadastrar solicitação de maquinário tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cadastrar solicitação"})
			}
			return
		}

		s.resolverSolicitacao(ctx.Request.Context(), &solicitacao)
		ctx.JSON(http.StatusCreated, solicitacao)
	}
}

// CriarReparo é POST /solicitacoes/reparo -- só foto, sem vídeo (a tela de
// Pequeno Reparo nem oferece o campo).
func (s *SolicitacaoController) CriarReparo() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		solicitanteId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoMultipart[model.NovaSolicitacaoReparoPayload](ctx, bucketr2.TamanhoMaximoFoto)
		if !ok {
			return
		}

		fotoChave, fotoMime, fotoTamanho, ok := s.chaveDoUpload(ctx, tenantId, s.bucketPequenosReparos, "foto", "image/", true)
		if !ok {
			return
		}
		input.FotoChave, input.FotoMime, input.FotoTamanho = fotoChave, fotoMime, fotoTamanho

		solicitacao, err := s.service.CadastrarSolicitacaoReparo(ctx.Request.Context(), tenantId, solicitanteId, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("cadastrar solicitação de reparo tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cadastrar solicitação"})
			}
			return
		}

		s.resolverSolicitacao(ctx.Request.Context(), &solicitacao)
		ctx.JSON(http.StatusCreated, solicitacao)
	}
}

// statusSolicitacaoValidos e tiposSolicitacaoValidos são os ENUMs
// status_solicitacao/tipo_solicitacao do banco. O filtro entra num cast
// ::status_solicitacao/::tipo_solicitacao dentro da query, então valor fora
// da lista vira erro 22P02 do Postgres -- 500 para o que é erro do cliente.
// Barra aqui e responde 400 (mesmo padrão de perfisValidos em
// loginController.go).
var statusSolicitacaoValidos = []string{"Pendente", "Convertida", "Rejeitada"}
var tiposSolicitacaoValidos = []string{"maquinario", "reparo"}

// Minhas é GET /solicitacoes/minhas -- paginada, restrita ao próprio
// solicitante (o service nem recebe perfil: é sempre "o que é meu").
func (s *SolicitacaoController) Minhas() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		solicitanteId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		pagina := int32(1)
		if bruto := ctx.Query("pagina"); bruto != "" {
			n, err := strconv.Atoi(bruto)
			if err != nil || n < 1 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "pagina inválida"})
				return
			}
			pagina = int32(n)
		}

		var status *string
		if bruto := ctx.Query("status"); bruto != "" {
			if !slices.Contains(statusSolicitacaoValidos, bruto) {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "status inválido"})
				return
			}
			status = &bruto
		}

		var busca *string
		if bruto := ctx.Query("busca"); bruto != "" {
			busca = &bruto
		}

		pagina2, err := s.service.ListarMinhasSolicitacoes(ctx.Request.Context(), tenantId, solicitanteId, pagina, status, busca)
		if err != nil {
			log.Printf("listar minhas solicitações tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar solicitações"})
			return
		}

		s.resolverSolicitacoes(ctx.Request.Context(), pagina2.Dados)
		ctx.JSON(http.StatusOK, pagina2)
	}
}

// Listar é GET /solicitacoes -- a fila do Gestor (e Técnico/Administrador),
// recortada pelo escopo de quem chama. Array simples, sem paginação -- o
// front pagina no cliente, mesmo padrão de /maquinas e /preventivas.
func (s *SolicitacaoController) Listar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		var status *string
		if bruto := ctx.Query("status"); bruto != "" {
			if !slices.Contains(statusSolicitacaoValidos, bruto) {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "status inválido"})
				return
			}
			status = &bruto
		}

		var tipo *string
		if bruto := ctx.Query("tipo"); bruto != "" {
			if !slices.Contains(tiposSolicitacaoValidos, bruto) {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "tipo inválido"})
				return
			}
			tipo = &bruto
		}

		lojaId, ok := idDeQuery(ctx, "lojaId")
		if !ok {
			return
		}

		var busca *string
		if bruto := ctx.Query("busca"); bruto != "" {
			busca = &bruto
		}

		solicitacoes, err := s.service.ListarSolicitacoes(ctx.Request.Context(), tenantId, usuarioId, perfil, status, tipo, busca, lojaId)
		if err != nil {
			log.Printf("listar solicitações tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar solicitações"})
			return
		}

		s.resolverSolicitacoes(ctx.Request.Context(), solicitacoes)
		ctx.JSON(http.StatusOK, solicitacoes)
	}
}

// Obter é GET /solicitacoes/:id -- aberto a qualquer perfil autenticado,
// recortado pelo escopo de quem chama (ver comentário de ObterSolicitacao no
// service e de escopo_usuario_id em solicitacao_os.sql).
func (s *SolicitacaoController) Obter() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		solicitacao, err := s.service.ObterSolicitacao(ctx.Request.Context(), tenantId, usuarioId, perfil, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				log.Printf("obter solicitação id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter solicitação"})
			}
			return
		}

		s.resolverSolicitacao(ctx.Request.Context(), &solicitacao)
		ctx.JSON(http.StatusOK, solicitacao)
	}
}

// Resumo é GET /solicitacoes/resumo -- os três contadores da Home do
// Solicitante, sempre do próprio (mesmo ator de Minhas).
func (s *SolicitacaoController) Resumo() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		solicitanteId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		resumo, err := s.service.ObterResumo(ctx.Request.Context(), tenantId, solicitanteId)
		if err != nil {
			log.Printf("obter resumo de solicitações tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter resumo"})
			return
		}

		ctx.JSON(http.StatusOK, resumo)
	}
}

// AbrirOS é POST /solicitacoes/:id/abrir-os -- a aprovação do Gestor: define
// técnico e urgência, e a solicitação vira uma OrdemServico. 201 porque nasce
// um recurso novo (a OS), diferente de Rejeitar, que só muda o estado da
// solicitação que já existe.
func (s *SolicitacaoController) AbrirOS() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.AberturaOrdemServicoPayload](ctx)
		if !ok {
			return
		}

		os, err := s.service.AbrirOS(ctx.Request.Context(), tenantId, atorId, perfil, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("abrir os solicitacao=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao abrir ordem de serviço"})
			}
			return
		}

		ctx.JSON(http.StatusCreated, os)
	}
}

// Rejeitar é POST /solicitacoes/:id/rejeitar -- encerra a solicitação sem
// abrir OS.
func (s *SolicitacaoController) Rejeitar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.RejeicaoSolicitacaoPayload](ctx)
		if !ok {
			return
		}

		solicitacao, err := s.service.Rejeitar(ctx.Request.Context(), tenantId, atorId, perfil, id, input.Motivo)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("rejeitar solicitacao=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao rejeitar solicitação"})
			}
			return
		}

		s.resolverSolicitacao(ctx.Request.Context(), &solicitacao)
		ctx.JSON(http.StatusOK, solicitacao)
	}
}
