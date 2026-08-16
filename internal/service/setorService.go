package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type SetorService struct {
	Pool *pgxpool.Pool
}

func NewRepoSetor(pool *pgxpool.Pool) *SetorService {

	return &SetorService{
		Pool: pool,
	}
}

// CadastrarSetor é POST /setores. Nome é único por loja (uq_setor_loja):
// duplicado volta 23505 e vira ErrDadoDuplicado.
//
// Roda em transação por causa do cheque da loja: a FK composta fk_setor_loja
// garante que a loja existe no mesmo tenant, mas NÃO que ela está ativa --
// sem isto dá para desativar uma loja (o que só é permitido com zero setores
// ativos) e depois pendurar um setor novo nela, contornando a regra pelo outro
// lado. O SELECT ... FOR SHARE segura a linha da loja até o commit, senão
// alguém desativa a loja entre o cheque e o INSERT.
func (s *SetorService) CadastrarSetor(ctx context.Context, tenantID int64, payload model.NovoSetorPayload) (model.Setor, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.Setor{}, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.Setor{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	loja, err := repo.ObterLojaParaEscrita(ctx, repository.ObterLojaParaEscritaParams{
		ID:       payload.LojaId,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Setor{}, fmt.Errorf("%w: loja %d não existe neste tenant", helper.ErrConflitoIntegridade, payload.LojaId)
		}
		return model.Setor{}, helper.TraduzErroPostgres(err)
	}
	if !loja.Ativa {
		return model.Setor{}, fmt.Errorf("%w: a loja %q está desativada", helper.ErrConflitoIntegridade, loja.Nome)
	}

	setor, err := repo.CriarSetor(ctx, repository.CriarSetorParams{
		TenantID: tenantID,
		LojaID:   payload.LojaId,
		Nome:     nome,
	})
	if err != nil {
		return model.Setor{}, helper.TraduzErroPostgres(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Setor{}, helper.TraduzErroPostgres(err)
	}

	return montarSetor(setor), nil
}

// montarSetor é a única tradução de linha do banco para resposta -- mesmo
// motivo de montarLoja/montarUsuario.
func montarSetor(s repository.Setor) model.Setor {
	return model.Setor{
		Id:     s.ID,
		Nome:   s.Nome,
		LojaId: s.LojaID,
		Ativo:  s.Ativo,
	}
}
func (s *SetorService) ListarSetores(ctx context.Context, tenantID int64, idLoja *int64) ([]model.Setor, error) {

	repo := repository.New(s.Pool)

	setores, err := repo.ListarSetores(ctx, repository.ListarSetoresParams{
		TenantID: tenantID,
		LojaID:   idLoja,
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	// Não-nil: o front tipa SetorCadastrado[] e nil viraria `null` no JSON.
	dto := make([]model.Setor, 0, len(setores))
	for _, set := range setores {
		dto = append(dto, montarSetor(set))
	}

	return dto, nil
}

// ObterSetor é GET /setores/:id -- o que a tela de edição carrega.
func (s *SetorService) ObterSetor(ctx context.Context, tenantID, id int64) (model.Setor, error) {

	repo := repository.New(s.Pool)

	setor, err := repo.ObterSetorCompletoPorID(ctx, repository.ObterSetorCompletoPorIDParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			return model.Setor{}, helper.ErrNaoEncontrado
		}
		return model.Setor{}, helper.TraduzErroPostgres(err)
	}

	return montarSetor(setor), nil
}

// AtualizarSetor é PUT /setores/:id. Só o nome muda: o lojaId que o front
// manda no corpo é ignorado de propósito (ver model.NovoSetorPayload).
func (s *SetorService) AtualizarSetor(ctx context.Context, tenantId, id int64, payload model.NovoSetorPayload) (model.Setor, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.Setor{}, err
	}

	repo := repository.New(s.Pool)
	setor, err := repo.AtualizarSetor(ctx, repository.AtualizarSetorParams{
		ID:       id,
		TenantID: tenantId,
		Nome:     nome,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Setor{}, helper.ErrNaoEncontrado
		}
		return model.Setor{}, helper.TraduzErroPostgres(err)
	}

	return montarSetor(setor), nil
}

// DesativarSetor é DELETE /setores/:id -- soft delete (ativo = false).
//
// Sem cheque de uso: máquina e OS apontam pro setor, e é justamente por isso
// que o delete é soft -- o histórico continua resolvendo o nome. O escopo de
// acesso (usuario_escopo_setor) também fica, para não perder a configuração de
// quem for reativado.
func (s *SetorService) DesativarSetor(ctx context.Context, tenantID, id int64) error {

	repo := repository.New(s.Pool)

	linhas, err := repo.DesativarSetor(ctx, repository.DesativarSetorParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {

		return helper.TraduzErroPostgres(err)
	}

	if linhas == 0 {
		return helper.ErrNaoEncontrado
	}

	return nil
}
