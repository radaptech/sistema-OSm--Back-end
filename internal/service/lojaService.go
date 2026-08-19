package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type LojaService struct {
	Pool *pgxpool.Pool
}

func NewRepoLojas(pool *pgxpool.Pool) *LojaService {

	return &LojaService{
		Pool: pool,
	}
}

// montarLoja é a única tradução de linha do banco para resposta -- um formato
// de Loja, montado num lugar só (mesmo motivo de montarUsuario).
func montarLoja(l repository.Loja) model.Loja {
	return model.Loja{
		Id:   l.ID,
		Nome: l.Nome,
		// Empresa é o tenant: não há coluna empresa_id, é o mesmo id.
		EmpresaId: l.TenantID,
		Ativa:     l.Ativa,
	}
}

// nomeValido apara espaços e recusa o que sobrar vazio. O banco não tem CHECK
// de nome não-vazio, e binding:"required" do Gin passa numa string de espaços
// -- sem isto entra loja com nome em branco, que ninguém consegue selecionar
// depois num select.
func nomeValido(nome string) (string, error) {
	limpo := strings.TrimSpace(nome)
	if limpo == "" {
		return "", fmt.Errorf("%w: nome é obrigatório", helper.ErrValidacao)
	}
	return limpo, nil
}

// CadastrarLoja é POST /lojas. Nome é único por tenant
// (uq_loja_tenant_nome): duplicado volta 23505 e vira ErrDadoDuplicado.
func (l *LojaService) CadastrarLoja(ctx context.Context, tenantID int64, payload model.NovaLojaPayload) (model.Loja, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.Loja{}, err
	}

	repo := repository.New(l.Pool)

	loja, err := repo.CriarLoja(ctx, repository.CriarLojaParams{
		TenantID: tenantID,
		Nome:     nome,
	})
	if err != nil {
		return model.Loja{}, helper.TraduzErroPostgres(err)
	}

	return montarLoja(loja), nil
}

// ObterLoja é GET /lojas/:id -- o que a tela de edição carrega.
//
// O errors.Is(pgx.ErrNoRows) vem ANTES de TraduzErroPostgres de propósito: o
// helper só entende códigos do pgconn, e ErrNoRows cai no fmt.Errorf("%v")
// final dele, que embrulha com %v e não %w -- o errors.Is do controller nunca
// casaria e um id inexistente responderia 500 em vez de 404.
func (l *LojaService) ObterLoja(ctx context.Context, tenantId, id int64) (model.Loja, error) {

	repo := repository.New(l.Pool)

	loja, err := repo.ObterLojaPorID(ctx, repository.ObterLojaPorIDParams{
		ID:       id,
		TenantID: tenantId,
	})
	if err != nil {
		// tenant_id está no WHERE: id de outro tenant é indistinguível de
		// inexistente, que é tudo que o cliente pode saber.
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Loja{}, helper.ErrNaoEncontrado
		}
		return model.Loja{}, helper.TraduzErroPostgres(err)
	}

	return montarLoja(loja), nil
}

// ListarLojas é GET /lojas: array simples, sem paginação -- o front pagina e
// filtra no cliente (front-end/CLAUDE.md item 12).
func (l *LojaService) ListarLojas(ctx context.Context, tenantID int64) ([]model.Loja, error) {

	repo := repository.New(l.Pool)

	lojas, err := repo.ListarLojas(ctx, tenantID)
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	// Não-nil: o front tipa Loja[] e nil viraria `null` no JSON.
	dto := make([]model.Loja, 0, len(lojas))
	for _, loja := range lojas {
		dto = append(dto, montarLoja(loja))
	}

	return dto, nil
}

// AtualizarLoja é PUT /lojas/:id.
func (l *LojaService) AtualizarLoja(ctx context.Context, tenantID, id int64, payload model.NovaLojaPayload) (model.Loja, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.Loja{}, err
	}

	repo := repository.New(l.Pool)

	loja, err := repo.AtualizarLoja(ctx, repository.AtualizarLojaParams{
		ID:       id,
		TenantID: tenantID,
		Nome:     nome,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Loja{}, helper.ErrNaoEncontrado
		}
		return model.Loja{}, helper.TraduzErroPostgres(err)
	}

	return montarLoja(loja), nil
}

// DesativarLoja é DELETE /lojas/:id -- soft delete (ativa = false).
//
// Recusa enquanto houver setor ativo na loja, e a contagem roda na MESMA
// transação do UPDATE: em duas chamadas separadas, alguém cria um setor entre
// a contagem e a desativação e a loja fica inativa com setor ativo pendurado
// -- e usuario_escopo_setor aponta pro setor, então o gestor continuaria com
// acesso a uma loja que sumiu das listagens.
//
// Recusar em vez de cascatear porque o soft delete não tem volta pela API
// (não existe reativação): desativar 8 setores de uma vez por um clique é
// estrago que ninguém desfaz. Quem quiser a loja fora tira os setores antes.
//
// ponytail: sem cascata; se virar incômodo, troque a recusa por
// DesativarSetoresDaLoja na mesma tx (a query já existe).
func (l *LojaService) DesativarLoja(ctx context.Context, tenantID, id int64) error {

	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	setores, err := repo.ContarSetoresAtivosDaLoja(ctx, repository.ContarSetoresAtivosDaLojaParams{
		TenantID: tenantID,
		LojaID:   id,
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}
	if setores > 0 {
		// Sentinela na frente: "%w: detalhe" lê como frase, "detalhe: %w" deixa
		// o texto genérico pendurado no fim da mensagem que o usuário vê.
		return fmt.Errorf("%w: a loja ainda tem %d setor(es) ativo(s), desative-os antes", helper.ErrConflitoIntegridade, setores)
	}

	linhas, err := repo.DesativarLoja(ctx, repository.DesativarLojaParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}

	// Zero linhas: id inexistente ou de outro tenant (o WHERE filtra os dois).
	// Já desativada casa a linha e conta 1 -- desativar de novo é idempotente.
	if linhas == 0 {
		return helper.ErrNaoEncontrado
	}

	return tx.Commit(ctx)
}

// ListarEmpresas é GET /empresas. Vive no LojaService porque é o único lugar
// que consome: o select de Empresa no cadastro de loja. Não existe CRUD de
// empresa -- o tenant nasce pela CLI de provisionamento.
//
// Devolve uma lista de um item só (a empresa do tenant autenticado), e não o
// objeto direto, porque o front tipa Empresa[] e itera pra montar o select.
func (l *LojaService) ListarEmpresas(ctx context.Context, tenantID int64) ([]model.Empresa, error) {

	repo := repository.New(l.Pool)

	empresa, err := repo.ObterEmpresaPorID(ctx, tenantID)
	if err != nil {
		// Token válido de um tenant que sumiu ou foi desativado: nada a listar,
		// e não é erro de servidor.
		if errors.Is(err, pgx.ErrNoRows) {
			return []model.Empresa{}, nil
		}
		return nil, helper.TraduzErroPostgres(err)
	}

	return []model.Empresa{{Id: empresa.ID, Nome: empresa.Nome}}, nil
}
