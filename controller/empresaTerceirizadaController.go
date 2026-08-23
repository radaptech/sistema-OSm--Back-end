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

type EmpresaTerceirizadaServiceInterface interface {
	CadastrarEmpresaTerceirizada(ctx context.Context, tenantID int64, payload model.NovaEmpresaTerceirizadaPayload) (model.EmpresaTerceirizada, error)
	ObterEmpresaTerceirizada(ctx context.Context, tenantID, id int64) (model.EmpresaTerceirizada, error)
	ListarEmpresasTerceirizadas(ctx context.Context, tenantID int64) ([]model.EmpresaTerceirizada, error)
	AtualizarEmpresaTerceirizada(ctx context.Context, tenantID, id int64, payload model.NovaEmpresaTerceirizadaPayload) (model.EmpresaTerceirizada, error)
	DesativarEmpresaTerceirizada(ctx context.Context, tenantID, id int64) error
}

type EmpresaTerceirizadaController struct {
	service EmpresaTerceirizadaServiceInterface
}

func NewEmpresaTerceirizadaController(service EmpresaTerceirizadaServiceInterface) *EmpresaTerceirizadaController {

	return &EmpresaTerceirizadaController{
		service: service,
	}
}

// Cadastrar é POST /empresas-terceirizadas.
//
// Corpo JSON puro (corpoJSON), não multipart: esta é a única entidade de
// cadastro sem arquivo nenhum. Especialidade e telefone chegam como string
// vazia quando o formulário não os preenche -- quem transforma em NULL é o
// textoOuNil do service, não o binding.
func (e *EmpresaTerceirizadaController) Cadastrar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.NovaEmpresaTerceirizadaPayload](ctx)
		if !ok {
			return
		}

		empresa, err := e.service.CadastrarEmpresaTerceirizada(ctx.Request.Context(), tenantId, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "já existe uma empresa terceirizada com esse nome"})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("cadastrar empresa terceirizada tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cadastrar empresa terceirizada"})
			}
			return
		}

		ctx.JSON(http.StatusCreated, empresa)
	}
}

// Listar é GET /empresas-terceirizadas: array simples, sem filtro nem
// paginação, e só as ativas -- a lista alimenta o select do Técnico em
// ModalAcionarTerceiro, e oferecer empresa desativada é oferecer o que o
// Administrador acabou de tirar do ar.
//
// Sem escopo de acesso, diferente de /maquinas e /preventivas: empresa
// terceirizada não pende de loja nem setor, é do tenant inteiro -- por isso
// não há atorDaRota aqui.
func (e *EmpresaTerceirizadaController) Listar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		empresas, err := e.service.ListarEmpresasTerceirizadas(ctx.Request.Context(), tenantId)
		if err != nil {

			log.Printf("listar empresas terceirizadas tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar empresas terceirizadas"})
			return
		}

		ctx.JSON(http.StatusOK, empresas)
	}
}

// Obter é GET /empresas-terceirizadas/:id -- o que a tela de edição carrega.
// O service não filtra `ativa`: o formulário precisa ler o registro mesmo
// desativado.
func (e *EmpresaTerceirizadaController) Obter() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		empresa, err := e.service.ObterEmpresaTerceirizada(ctx.Request.Context(), tenantId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				log.Printf("obter empresa terceirizada id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter empresa terceirizada"})
			}
			return
		}

		ctx.JSON(http.StatusOK, empresa)
	}
}

// Atualizar é PUT /empresas-terceirizadas/:id. Mesmo corpo do POST, então o
// mapa de erro é o mesmo mais o 404 do id da rota -- aqui ele é 404 e não 422
// porque o único registro que pode faltar é a própria empresa.
func (e *EmpresaTerceirizadaController) Atualizar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.NovaEmpresaTerceirizadaPayload](ctx)
		if !ok {
			return
		}

		empresa, err := e.service.AtualizarEmpresaTerceirizada(ctx.Request.Context(), tenantId, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "já existe uma empresa terceirizada com esse nome"})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("atualizar empresa terceirizada id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar empresa terceirizada"})
			}
			return
		}

		ctx.JSON(http.StatusOK, empresa)
	}
}

// Desativar é DELETE /empresas-terceirizadas/:id -- soft delete, como em loja e
// setor. Sem recusa por dependente: esta entidade não tem filho, e a OS que já
// a acionou continua apontando pra linha (o delete é soft).
func (e *EmpresaTerceirizadaController) Desativar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		if err := e.service.DesativarEmpresaTerceirizada(ctx.Request.Context(), tenantId, id); err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("desativar empresa terceirizada id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao desativar empresa terceirizada"})
			}
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"message": "empresa terceirizada desativada"})
	}
}
