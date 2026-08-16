package controller

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

// A interface existe pelo mesmo motivo de LoginServiceInterface: LojaService
// guarda um *pgxpool.Pool concreto, então sem ela não dá pra testar handler
// sem banco.
type LojaServiceInterface interface {
	CadastrarLoja(ctx context.Context, tenantID int64, payload model.NovaLojaPayload) (model.Loja, error)
	ObterLoja(ctx context.Context, tenantId, id int64) (model.Loja, error)
	ListarLojas(ctx context.Context, tenantID int64) ([]model.Loja, error)
	AtualizarLoja(ctx context.Context, tenantID, id int64, payload model.NovaLojaPayload) (model.Loja, error)
	DesativarLoja(ctx context.Context, tenantID, id int64) error
	ListarEmpresas(ctx context.Context, tenantID int64) ([]model.Empresa, error)
}

type LojaController struct {
	service LojaServiceInterface
}

func NewLojaController(service LojaServiceInterface) *LojaController {

	return &LojaController{
		service: service,
	}
}

// tenantDaRota é o tenant do token -- todas as rotas de loja são autenticadas.
// Ler o header X-tenant-ID aqui deixaria um administrador do tenant A cadastrar
// loja no tenant B só trocando o header; o único endpoint que lê o header é o
// login, antes de existir token.
func tenantDaRota(ctx *gin.Context) (int64, bool) {
	tenantId, ok := middleware.GetTenantIDToken(ctx)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
		return 0, false
	}
	return tenantId, true
}

// corpoLoja lê e valida o corpo de POST/PUT. Extra no JSON é ignorado pelo
// binding -- inclusive o empresaId que o front manda hoje e que não tem coluna
// onde cair (ver model.Loja).
func corpoLoja(ctx *gin.Context) (model.NovaLojaPayload, bool) {
	var input model.NovaLojaPayload
	if err := ctx.ShouldBindJSON(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":    "dados invalidos",
			"detalhes": err.Error(),
		})
		return model.NovaLojaPayload{}, false
	}
	return input, true
}

// Cadastrar é POST /lojas.
func (l *LojaController) Cadastrar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		input, ok := corpoLoja(ctx)
		if !ok {
			return
		}

		tenantId, okTenant := tenantDaRota(ctx)
		if !okTenant {
			return
		}

		loja, err := l.service.CadastrarLoja(ctx.Request.Context(), tenantId, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "já existe uma loja com esse nome"})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				// Erro interno só no log: o erro cru do pgx carrega nome de
				// constraint/coluna e às vezes o SQL.
				log.Printf("cadastrar loja tenant=%d: %v", tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cadastrar loja"})
			}
			return
		}

		ctx.JSON(http.StatusCreated, loja)
	}
}

// Obter é GET /lojas/:id -- o que a tela de edição carrega.
func (l *LojaController) Obter() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, okTenant := tenantDaRota(ctx)
		if !okTenant {
			return
		}

		loja, err := l.service.ObterLoja(ctx.Request.Context(), tenantId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				log.Printf("obter loja id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter loja"})
			}
			return
		}

		ctx.JSON(http.StatusOK, loja)
	}
}

// Listar é GET /lojas: array simples, sem paginação -- o front pagina e filtra
// no cliente. Sem switch de erro: listagem não tem erro de negócio, ou o banco
// respondeu ou caiu. Tenant sem loja é 200 com [].
func (l *LojaController) Listar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		lojas, err := l.service.ListarLojas(ctx.Request.Context(), tenantId)
		if err != nil {
			log.Printf("listar lojas tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar lojas"})
			return
		}

		ctx.JSON(http.StatusOK, lojas)
	}
}

// Atualizar é PUT /lojas/:id. Aqui ErrNaoEncontrado é 404 e não o 422 do
// Atualizar de usuário: o corpo da loja não cita nenhuma outra entidade, então
// o :id é a única coisa que pode não existir.
func (l *LojaController) Atualizar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		input, okCorpo := corpoLoja(ctx)
		if !okCorpo {
			return
		}

		tenantId, okTenant := tenantDaRota(ctx)
		if !okTenant {
			return
		}

		loja, err := l.service.AtualizarLoja(ctx.Request.Context(), tenantId, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": "já existe uma loja com esse nome"})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("atualizar loja id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar loja"})
			}
			return
		}

		ctx.JSON(http.StatusOK, loja)
	}
}

// Desativar é DELETE /lojas/:id -- soft delete (ativa = false).
//
// O 422 aqui é o caso concreto de "loja ainda tem setor ativo": o service
// recusa em vez de cascatear, e a mensagem diz quantos setores faltam desativar
// -- é ela que o front mostra no toast, então não pode virar texto genérico.
func (l *LojaController) Desativar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, okTenant := tenantDaRota(ctx)
		if !okTenant {
			return
		}

		if err := l.service.DesativarLoja(ctx.Request.Context(), tenantId, id); err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				log.Printf("desativar loja id=%d tenant=%d: %v", id, tenantId, err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao desativar loja"})
			}
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"message": "loja desativada"})
	}
}

// ListarEmpresas é GET /empresas -- alimenta o select de Empresa no cadastro
// de loja. Fica neste controller porque empresa não tem CRUD: é o tenant, e a
// única tela que pergunta por ela é a de loja.
func (l *LojaController) ListarEmpresas() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		empresas, err := l.service.ListarEmpresas(ctx.Request.Context(), tenantId)
		if err != nil {
			log.Printf("listar empresas tenant=%d: %v", tenantId, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar empresas"})
			return
		}

		ctx.JSON(http.StatusOK, empresas)
	}
}
