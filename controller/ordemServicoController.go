package controller

import (
	"context"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/internal/service"
)

type OrdemServicoServiceInterface interface {
	ListarOrdensServico(ctx context.Context, tenantId, usuarioId int64, perfil string, filtros service.FiltrosOrdemServico) ([]model.OrdemServico, error)
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
