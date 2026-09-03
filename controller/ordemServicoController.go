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
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/internal/service"
)

type OrdemServicoServiceInterface interface {
	ListarOrdensServico(ctx context.Context, tenantId, usuarioId int64, perfil string, filtros service.FiltrosOrdemServico) ([]model.OrdemServico, error)
	ObterIndicadoresDaMaquina(ctx context.Context, tenantId, maquinaId, usuarioId int64, perfil string) (model.IndicadoresMaquina, error)
	// Ciclo de vida da OS (fase 2) -- todo método aqui recebe atorId como o
	// TÉCNICO do token, nunca do corpo (mesmo motivo do resto do sistema): é
	// o service que confere que atorId é o dono da OS, devolvendo
	// ErrNaoEncontrado quando não é (ver a nota em ObterOrdemServicoPorID).
	Iniciar(ctx context.Context, tenantId, atorId, ordemServicoId int64) (model.OrdemServico, error)
	Pausar(ctx context.Context, tenantId, atorId, ordemServicoId int64, motivo string) (model.OrdemServico, error)
	Retomar(ctx context.Context, tenantId, atorId, ordemServicoId int64) (model.OrdemServico, error)
	AcionarTerceiro(ctx context.Context, tenantId, atorId, ordemServicoId, empresaTerceirizadaId int64) (model.OrdemServico, error)
	Encerrar(ctx context.Context, tenantId, atorId, ordemServicoId int64, payload model.EncerramentoOrdemServicoPayload) (model.OrdemServico, error)
}

// OrdemServicoController não guarda bucket nenhum, diferente de
// SolicitacaoController e MaquinaController: o tipo OrdemServico do front não
// tem campo de mídia (a foto do defeito é da SOLICITAÇÃO, e é lá que o modal
// de detalhes vai buscá-la). Nada a assinar no R2 aqui.
type OrdemServicoController struct {
	service OrdemServicoServiceInterface
}

func NewOrdemServicoController(service OrdemServicoServiceInterface) *OrdemServicoController {

	return &OrdemServicoController{
		service: service,
	}
}

// statusOsValidos e tiposOsValidos são os ENUMs status_os/tipo_os do banco.
// Mesmo motivo de statusSolicitacaoValidos em solicitacaoController.go: o
// filtro entra num cast ::status_os/::tipo_os dentro da query, então valor
// fora da lista viraria erro 22P02 do Postgres -- 500 para o que é erro do
// cliente. Barra aqui e responde 400.
var statusOsValidos = []string{"Aberta", "Em Andamento", "Pausada", "Concluída"}
var tiposOsValidos = []string{"maquinario", "terceiros", "reparo"}

// statusDeQuery lê ?status= da fila de OS.
//
// ⚠️ Separado por VÍRGULA, não repetido: montarQuery no front faz
// `busca.set(chave, valor.join(','))` para todo array (servicos/montarQuery.ts),
// então ?status=Aberta,Em Andamento é uma chave só. ctx.QueryArray devolveria
// um item só, com a vírgula dentro, e o cast no Postgres estouraria em 22P02.
//
// Devolve nil (não filtra) quando ausente ou vazio, mesmo critério de
// idDeQuery -- o front já descarta filtro vazio, e "todas" é o modo normal do
// Painel do Gestor.
func statusDeQuery(ctx *gin.Context) ([]string, bool) {

	bruto := ctx.Query("status")
	if bruto == "" {
		return nil, true
	}

	status := strings.Split(bruto, ",")
	for i, s := range status {
		// Um espaço depois da vírgula é o erro humano óbvio em teste manual
		// (curl/Postman), e o encode do front já preserva o espaço DENTRO do
		// valor ("Em Andamento") -- aparar a borda não estraga nenhum rótulo.
		status[i] = strings.TrimSpace(s)
		if !slices.Contains(statusOsValidos, status[i]) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "status inválido: " + status[i]})
			return nil, false
		}
	}

	return status, true
}

// Listar é GET /ordens-servico -- um endpoint para os três painéis, recortado
// pelo escopo de quem chama (o WHERE da query, nunca o filtro do cliente):
//
//	Gestor  (PainelGestor)                  -> sem filtro
//	Técnico (PainelTecnico)                 -> ?tecnicoId=
//	Admin   (CustosPendentes/OSFinalizadas) -> ?status=Concluída / ?finalizada=true
//
// Array simples, sem paginação -- o front pagina no cliente, mesmo padrão de
// /solicitacoes, /maquinas e /preventivas. `?pagina=` é aceito e ignorado: o
// front o inclui no objeto de parâmetros de algumas telas, e recusar seria
// quebrar por um campo que não muda nada.
//
// Só lê -- o único erro possível é do banco, e ele vai 500 com o erro cru no
// log (nome de constraint/coluna não pode ir no corpo da resposta).
func (o *OrdemServicoController) Listar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		// Do TOKEN, nunca da query: aceitar do cliente deixaria um Técnico
		// listar a loja inteira mandando outro id.
		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		status, ok := statusDeQuery(ctx)
		if !ok {
			return
		}

		var tipo *string
		if bruto := ctx.Query("tipo"); bruto != "" {
			if !slices.Contains(tiposOsValidos, bruto) {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "tipo inválido"})
				return
			}
			tipo = &bruto
		}

		// Diferente de status/tipo, "finalizada" tem só dois valores válidos e
		// quem os define é o ParseBool: qualquer outra coisa é erro de
		// cliente, não "não filtrar" -- ?finalizada=sim silenciosamente
		// ignorado devolveria a lista inteira para a tela de OS Finalizadas.
		var finalizada *bool
		if bruto := ctx.Query("finalizada"); bruto != "" {
			v, err := strconv.ParseBool(bruto)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "finalizada inválido"})
				return
			}
			finalizada = &v
		}

		lojaId, ok := idDeQuery(ctx, "lojaId")
		if !ok {
			return
		}

		tecnicoId, ok := idDeQuery(ctx, "tecnicoId")
		if !ok {
			return
		}

		var busca *string
		if bruto := ctx.Query("busca"); bruto != "" {
			busca = &bruto
		}

		ordens, err := o.service.ListarOrdensServico(ctx.Request.Context(), tenantId, usuarioId, perfil, service.FiltrosOrdemServico{
			Status:     status,
			Tipo:       tipo,
			Finalizada: finalizada,
			LojaId:     lojaId,
			TecnicoId:  tecnicoId,
			Busca:      busca,
		})
		if err != nil {
			log.Printf("listar ordens de serviço tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar ordens de serviço"})
			return
		}

		ctx.JSON(http.StatusOK, ordens)
	}
}

// Indicadores é GET /indicadores/maquinas/:id -- o Painel de Indicadores do
// Gestor (DashboardGestor). O `:id` é de MÁQUINA, não de OS: a rota vive fora
// de /ordens-servico porque é assim que o front a chama (servicoIndicadores),
// mas o handler mora aqui pelo mesmo motivo do service -- tudo que ela lê é
// histórico de OS.
//
// ErrNaoEncontrado é 404 e cobre os dois casos de uma vez: máquina que não
// existe e máquina fora do escopo de quem chama. Distingui-los responderia
// "esta existe, você é que não pode ver" -- que é justamente o que a
// enumeração de ids procura. Não é 403: 403 é perfil errado (o RBAC da rota),
// e aqui o perfil está certo.
func (o *OrdemServicoController) Indicadores() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		maquinaId, ok := idDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		indicadores, err := o.service.ObterIndicadoresDaMaquina(ctx.Request.Context(), tenantId, maquinaId, usuarioId, perfil)
		if err != nil {
			if errors.Is(err, helper.ErrNaoEncontrado) {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "máquina não encontrada"})
				return
			}
			log.Printf("indicadores da máquina %d tenant=%d: %v", maquinaId, tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter indicadores da máquina"})
			return
		}

		ctx.JSON(http.StatusOK, indicadores)
	}
}

// Iniciar é POST /ordens-servico/:id/iniciar -- Aberta -> Em Andamento.
// Sem corpo. 200, não 201: atualiza uma OS que já existe (quem cria é
// SolicitacaoController.AbrirOS), mesmo critério de Rejeitar.
func (o *OrdemServicoController) Iniciar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		os, err := o.service.Iniciar(ctx.Request.Context(), tenantId, atorId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("iniciar ordem de serviço=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar ordem de serviço"})
			}
			return
		}

		ctx.JSON(http.StatusOK, os)
	}
}

// Pausar é POST /ordens-servico/:id/pausar -- (Aberta ou Em Andamento) ->
// Pausada, com `{motivo}` no corpo.
func (o *OrdemServicoController) Pausar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.PausaOrdemServicoPayload](ctx)
		if !ok {
			return
		}

		os, err := o.service.Pausar(ctx.Request.Context(), tenantId, atorId, id, input.Motivo)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("pausar ordem de serviço=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao pausar ordem de serviço"})
			}
			return
		}

		ctx.JSON(http.StatusOK, os)
	}
}

// Retomar é POST /ordens-servico/:id/retomar -- Pausada -> o status que ela
// tinha antes de pausar. Sem corpo.
func (o *OrdemServicoController) Retomar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		os, err := o.service.Retomar(ctx.Request.Context(), tenantId, atorId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("retomar ordem de serviço=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao retomar ordem de serviço"})
			}
			return
		}

		ctx.JSON(http.StatusOK, os)
	}
}

// AcionarTerceiro é POST /ordens-servico/:id/acionar-terceiro -- promove tipo
// pra 'terceiros', com `{empresaTerceirizadaId}` no corpo. Não muda status de
// execução (ver a nota no service).
func (o *OrdemServicoController) AcionarTerceiro() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.AcionamentoTerceiroPayload](ctx)
		if !ok {
			return
		}

		os, err := o.service.AcionarTerceiro(ctx.Request.Context(), tenantId, atorId, id, input.EmpresaTerceirizadaId)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("acionar terceiro ordem de serviço=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao acionar terceiro"})
			}
			return
		}

		ctx.JSON(http.StatusOK, os)
	}
}

// Encerrar é POST /ordens-servico/:id/encerrar -- Em Andamento -> Concluída,
// gravando o que o Técnico apurou e o custo na mesma escrita (ver a nota no
// service).
func (o *OrdemServicoController) Encerrar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.EncerramentoOrdemServicoPayload](ctx)
		if !ok {
			return
		}

		os, err := o.service.Encerrar(ctx.Request.Context(), tenantId, atorId, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("encerrar ordem de serviço=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao encerrar ordem de serviço"})
			}
			return
		}

		ctx.JSON(http.StatusOK, os)
	}
}
