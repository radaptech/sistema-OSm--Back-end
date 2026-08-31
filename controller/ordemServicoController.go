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

// Sem bucket, diferente dos outros controllers: OrdemServico não tem campo de
// mídia -- a foto do defeito é da solicitação. Nada a assinar no R2 aqui.
type OrdemServicoController struct {
	service OrdemServicoServiceInterface
}

func NewOrdemServicoController(service OrdemServicoServiceInterface) *OrdemServicoController {

	return &OrdemServicoController{
		service: service,
	}
}

// O filtro entra num cast ::status_os/::tipo_os na query: valor fora da lista
// viraria 22P02 do Postgres, ou seja 500 para o que é erro do cliente.
var statusOsValidos = []string{"Aberta", "Em Andamento", "Pausada", "Concluída"}
var tiposOsValidos = []string{"maquinario", "terceiros", "reparo"}

// ⚠️ Separado por VÍRGULA, não repetido: montarQuery no front serializa array
// com join(','), então ctx.QueryArray traria um item só com a vírgula dentro.
func statusDeQuery(ctx *gin.Context) ([]string, bool) {

	bruto := ctx.Query("status")
	if bruto == "" {
		return nil, true
	}

	status := strings.Split(bruto, ",")
	for i, s := range status {
		// Apara a borda pelo espaço-depois-da-vírgula do teste manual; o espaço
		// DENTRO de "Em Andamento" sobrevive.
		status[i] = strings.TrimSpace(s)
		if !slices.Contains(statusOsValidos, status[i]) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "status inválido: " + status[i]})
			return nil, false
		}
	}

	return status, true
}

// Um endpoint para os três painéis; o que muda é o filtro. `?pagina=` é aceito
// e ignorado -- o front o manda em algumas telas e recusar quebraria à toa.
func (o *OrdemServicoController) Listar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		// Do token, nunca da query: aceitar do cliente deixaria um Técnico listar a loja inteira.
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

		// ?finalizada=sim ignorado em silêncio devolveria a lista inteira para a
		// tela de OS Finalizadas -- valor inválido é 400, não "não filtrar".
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
