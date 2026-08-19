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

type MaquinarioService struct {
	Pool *pgxpool.Pool
}

func NewRepoMaquinario(pool *pgxpool.Pool) *MaquinarioService {

	return &MaquinarioService{
		Pool: pool,
	}
}

func (m *MaquinarioService) CadastrarMaquina(ctx context.Context, tenantId int64, payload model.MaquinarioInsert) (model.Maquinario, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.Maquinario{}, err
	}

	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return model.Maquinario{}, fmt.Errorf("erro ao abrir transacao: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	setor, err := repo.ObterSetorPorID(ctx, repository.ObterSetorPorIDParams{
		ID:       payload.SetorID,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Maquinario{}, fmt.Errorf("%w: setor %d não existe neste tenant", helper.ErrConflitoIntegridade, payload.SetorID)
		}
		return model.Maquinario{}, helper.TraduzErroPostgres(err)
	}

	mm, err := repo.CriarMaquina(ctx, repository.CriarMaquinaParams{
		TenantID:         tenantId,
		SetorID:          setor.ID,
		Criticidade:      repository.NivelCriticidade(payload.Criticidade),
		NumeroPatrimonio: payload.NumeroPatrimonio,
		NumeroSerie:      payload.NumeroSerie,
		Nome:             nome,
		Descricao:        payload.Descricao,
		Marca:            payload.Marca,
		Modelo:           payload.Modelo,
	})
	if err != nil {

		return model.Maquinario{}, helper.TraduzErroPostgres(err)
	}

	// Mesma transação da máquina: a regra é que máquina sem preventiva não
	// chegue a existir, então as duas gravam juntas ou nenhuma grava.
	if err := gravarPreventivas(ctx, repo, tenantId, mm.ID, payload.Preventivas); err != nil {
		return model.Maquinario{}, err
	}

	maquinario, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{
		ID:       mm.ID,
		TenantID: tenantId,
	})
	if err != nil {
		return model.Maquinario{}, helper.TraduzErroPostgres(err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return model.Maquinario{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return model.MontarListaMaquinarios(repository.ListarMaquinasRow(maquinario)), nil
}

func (m *MaquinarioService) ListarMaquinario(ctx context.Context, tenantId int64, lojaId, setorId *int64) ([]model.Maquinario, error) {

	repo := repository.New(m.Pool)

	maquinarios, err := repo.ListarMaquinas(ctx, repository.ListarMaquinasParams{
		TenantID: tenantId,
		SetorID:  setorId,
		LojaID:   lojaId,
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	dto := make([]model.Maquinario, 0, len(maquinarios))
	for _, maquina := range maquinarios {

		dto = append(dto, model.MontarListaMaquinarios(maquina))
	}

	return dto, nil
}

func (m MaquinarioService) ObterMaquina(ctx context.Context, tenantID, id int64) (model.Maquinario, error) {

	repo := repository.New(m.Pool)

	maquinario, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Maquinario{}, helper.ErrNaoEncontrado
		}
		return model.Maquinario{}, helper.TraduzErroPostgres(err)
	}

	return model.MontarListaMaquinarios(repository.ListarMaquinasRow(maquinario)), nil
}

func (m *MaquinarioService) AtualizarMaquina(ctx context.Context, tenantId, id int64, payload model.AtualizarMaquina) (model.Maquinario, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.Maquinario{}, err
	}
	tx, err := m.Pool.Begin(ctx)
	if err != nil {

		return model.Maquinario{}, fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	_, err = repo.AtualizarMaquina(ctx, repository.AtualizarMaquinaParams{
		ID:               id,
		TenantID:         tenantId,
		SetorID:          payload.SetorID,
		Criticidade:      repository.NivelCriticidade(payload.Criticidade),
		NumeroPatrimonio: payload.NumeroPatrimonio,
		NumeroSerie:      payload.NumeroSerie,
		Nome:             nome,
		Descricao:        payload.Descricao,
		Marca:            payload.Marca,
		Modelo:           payload.Modelo,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Maquinario{}, helper.ErrNaoEncontrado
		}
		return model.Maquinario{}, helper.TraduzErroPostgres(err)
	}

	// Substitui o conjunto inteiro, sem merge incremental: desativa as atuais e
	// insere as novas na mesma transação -- espelho do que AtualizarUsuario faz
	// com o escopo de acesso. Desativa em vez de deletar por causa da FK das
	// solicitações já geradas (ver preventiva.sql).
	if err := repo.DesativarPreventivasDaMaquina(ctx, repository.DesativarPreventivasDaMaquinaParams{
		TenantID:  tenantId,
		MaquinaID: id,
	}); err != nil {
		return model.Maquinario{}, helper.TraduzErroPostgres(err)
	}

	if err := gravarPreventivas(ctx, repo, tenantId, id, payload.Preventivas); err != nil {
		return model.Maquinario{}, err
	}

	maquina, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{
		ID:       id,
		TenantID: tenantId,
	})
	if err != nil {
		return model.Maquinario{}, helper.TraduzErroPostgres(err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return model.Maquinario{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return model.MontarListaMaquinarios(repository.ListarMaquinasRow(maquina)), nil

}

func (m *MaquinarioService) DesativarMaquina(ctx context.Context, tenantId, id int64) error {

	repo := repository.New(m.Pool)

	linhas, err := repo.DesativarMaquina(ctx, repository.DesativarMaquinaParams{
		ID:       id,
		TenantID: tenantId,
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}

	if linhas == 0 {
		return helper.ErrNaoEncontrado
	}

	return nil
}
