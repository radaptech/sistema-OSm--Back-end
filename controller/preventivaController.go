package controller

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type PreventivaServiceInterface interface {
	CadastrarPreventiva(ctx context.Context, tenantID int64, payload model.PreventivaPayload) (model.Preventiva, error)
	ListarPreventivas(ctx context.Context, tenantID, usuarioID int64, perfil string, maquinaID *int64) ([]model.Preventiva, error)
	ObterPreventiva(ctx context.Context, tenantID, id int64) (model.Preventiva, error)
	AtualizarPreventiva(ctx context.Context, tenantID, id int64, payload model.PreventivaPayload) (model.Preventiva, error)
	DesativarPreventiva(ctx context.Context, tenantID, id int64) error
}

type PreventivaController struct {
	service PreventivaServiceInterface
}

func NewPreventivaController(service PreventivaServiceInterface) *PreventivaController {

	return &PreventivaController{
		service: service,
	}
}

// Cadastrar é POST /preventivas -- a preventiva avulsa do
// ModalManutencaoPreventiva. O caminho do formulário de máquina NÃO passa por
// aqui: lá as preventivas viajam dentro do corpo da máquina e o service as
// grava na mesma transação (gravarPreventivas).
//
// maquinaId é conferido aqui e não por tag `binding`: PreventivaPayload é a
// mesma struct dos itens que viajam dentro de POST /maquinas, e ali o campo é
// ignorado de propósito (a máquina ainda não tem id, o front manda 0) -- um
// `required` na struct quebraria o cadastro de máquina. Sem o cheque, o zero
// chegaria no service e voltaria como 422 "máquina 0 não existe", que é o
// status errado para um campo obrigatório em branco.
func (p *PreventivaController) Cadastrar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.PreventivaPayload](ctx)
		if !ok {
			return
		}

		if input.MaquinaId < 1 {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "informe a máquina da preventiva"})
			return
		}

		preventiva, err := p.service.CadastrarPreventiva(ctx.Request.Context(), tenantId, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("cadastrar preventiva tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cadastrar preventiva"})
			}
			return
		}

		ctx.JSON(http.StatusCreated, preventiva)
	}
}

// Listar é GET /preventivas, com ?maquinaId= opcional: a tela de edição de
// máquina pede as de uma máquina só, a aba "Manutenção Prev." do painel do
// gestor pede todas. Só as ativas -- ver preventiva.sql. Array simples, sem
// paginação.
func (p *PreventivaController) Listar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		maquinaId, ok := idDeQuery(ctx, "maquinaId")
		if !ok {
			return
		}

		// Mesmo recorte de /maquinas: a aba do gestor traz as lojas dele, não o
		// tenant. Só o administrador vê tudo (escopoDe no service).
		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		preventivas, err := p.service.ListarPreventivas(ctx.Request.Context(), tenantId, usuarioId, perfil, maquinaId)
		if err != nil {

			log.Printf("listar preventivas tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar preventivas"})
			return
		}

		ctx.JSON(http.StatusOK, preventivas)
	}
}

// Obter é GET /preventivas/:id.
func (p *PreventivaController) Obter() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		preventiva, err := p.service.ObterPreventiva(ctx.Request.Context(), tenantId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				log.Printf("obter preventiva id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter preventiva"})
			}
			return
		}

		ctx.JSON(http.StatusOK, preventiva)
	}
}

// Atualizar é PUT /preventivas/:id.
//
// O maquinaId do corpo é ignorado pelo service (mover a preventiva de máquina
// deixaria as solicitações que ela já gerou apontando para outra), então aqui
// ele não é validado -- diferente do POST.
func (p *PreventivaController) Atualizar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.PreventivaPayload](ctx)
		if !ok {
			return
		}

		preventiva, err := p.service.AtualizarPreventiva(ctx.Request.Context(), tenantId, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("atualizar preventiva id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar preventiva"})
			}
			return
		}

		ctx.JSON(http.StatusOK, preventiva)
	}
}

// Desativar é DELETE /preventivas/:id -- soft delete.
//
// ⚠️ `ativa` acumula dois sentidos: o alternador "Preventiva habilitada no
// sistema" do modal e este delete. Desabilitar pelo PUT e excluir por aqui
// produzem o mesmo estado -- é decisão registrada, não acidente (ver
// database/queries/preventiva.sql).
func (p *PreventivaController) Desativar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		if err := p.service.DesativarPreventiva(ctx.Request.Context(), tenantId, id); err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("desativar preventiva id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao desativar preventiva"})
			}
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"message": "preventiva desativada"})
	}
}
