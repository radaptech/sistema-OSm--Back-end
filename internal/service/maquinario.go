package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/auth"
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
		FotoChave:        payload.FotoChave,
	})
	if err != nil {

		return model.Maquinario{}, traduzErroMaquina(err)
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

// ListarMaquinario é GET /maquinas. Além dos filtros opcionais que o cliente
// pede (?lojaId=/?setorId=), a listagem é recortada pelo escopo de quem chama:
// o solicitante enxerga o próprio setor, o técnico e o gestor as lojas/setores
// deles. Sem isso o front seria a única barreira -- e ele manda o filtro, não
// o impõe.
func (m *MaquinarioService) ListarMaquinario(ctx context.Context, tenantId, usuarioId int64, perfil string, lojaId, setorId *int64) ([]model.Maquinario, error) {

	repo := repository.New(m.Pool)

	maquinarios, err := repo.ListarMaquinas(ctx, repository.ListarMaquinasParams{
		TenantID:        tenantId,
		SetorID:         setorId,
		LojaID:          lojaId,
		EscopoUsuarioID: escopoDe(usuarioId, perfil),
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
		FotoChave:        payload.FotoChave,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Maquinario{}, helper.ErrNaoEncontrado
		}
		return model.Maquinario{}, traduzErroMaquina(err)
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

// DesativarMaquina recusa máquina com trabalho em andamento: desativada, ela
// some das listagens e a OS aberta fica apontando para algo que ninguém acha.
func (m *MaquinarioService) DesativarMaquina(ctx context.Context, tenantId, id int64) error {

	repo := repository.New(m.Pool)

	// ponytail: checa e desativa sem lock -- uma solicitação aberta entre os dois
	// passa. Clique raro de administrador; FOR UPDATE na máquina se incomodar.
	historico, err := repo.ContarHistoricoDaMaquina(ctx, repository.ContarHistoricoDaMaquinaParams{
		MaquinaID: &id,
		TenantID:  tenantId,
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}
	if historico.EmAberto > 0 {
		return helper.ErrMaquinaEmUso
	}

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

// ListarMaquinasInativas é GET /maquinas?ativa=false -- só o administrador
// chega aqui (o controller decide), então não há escopo para recortar.
func (m *MaquinarioService) ListarMaquinasInativas(ctx context.Context, tenantId int64) ([]model.Maquinario, error) {

	inativa := false
	maquinas, err := repository.New(m.Pool).ListarMaquinas(ctx, repository.ListarMaquinasParams{
		TenantID: tenantId,
		Ativa:    &inativa,
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	dto := make([]model.Maquinario, 0, len(maquinas))
	for _, maquina := range maquinas {
		dto = append(dto, model.MontarListaMaquinarios(maquina))
	}

	return dto, nil
}

func (m *MaquinarioService) ReativarMaquina(ctx context.Context, tenantId, id int64) error {

	linhas, err := repository.New(m.Pool).ReativarMaquina(ctx, repository.ReativarMaquinaParams{
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

// HistoricoMaquina conta o que a exclusão definitiva vai levar junto.
func (m *MaquinarioService) HistoricoMaquina(ctx context.Context, tenantId, id int64) (model.HistoricoMaquina, error) {

	if _, err := m.ObterMaquina(ctx, tenantId, id); err != nil {
		return model.HistoricoMaquina{}, err
	}

	h, err := repository.New(m.Pool).ContarHistoricoDaMaquina(ctx, repository.ContarHistoricoDaMaquinaParams{
		MaquinaID: &id,
		TenantID:  tenantId,
	})
	if err != nil {
		return model.HistoricoMaquina{}, helper.TraduzErroPostgres(err)
	}

	return model.HistoricoMaquina(h), nil
}

// ExcluirMaquinaDefinitivo apaga a máquina e TODO o histórico dela (ver
// ExcluirMaquinaDefinitivo em maquina.sql). Só passa com a senha de quem pede
// e o número de patrimônio digitado igual ao cadastrado.
//
// Devolve as chaves dos objetos no R2 (foto da máquina e anexos das
// solicitações) que ficaram sem dono: quem apaga é o controller, que conhece
// os buckets, e só depois do commit -- apagar antes e a transação voltar
// deixaria linha apontando para arquivo que não existe mais.
func (m *MaquinarioService) ExcluirMaquinaDefinitivo(ctx context.Context, tenantId, usuarioId, id int64, payload model.ExcluirMaquinaPayload) (foto *string, anexos []string, err error) {

	repo := repository.New(m.Pool)

	usuario, err := repo.ObterUsuarioPorID(ctx, repository.ObterUsuarioPorIDParams{
		ID:       usuarioId,
		TenantID: tenantId,
	})
	if err != nil {
		return nil, nil, helper.TraduzErroPostgres(err)
	}

	// !Ativo: o JWT não consulta o banco, então um administrador desativado
	// segue com token válido até expirar -- aqui o custo disso seria apagar dados.
	if ok, err := auth.HashCompare([]byte(usuario.SenhaHash), payload.Senha); err != nil || !ok || !usuario.Ativo {
		return nil, nil, helper.ErrSenhaIncorreta
	}

	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao abrir transacao: %w", err)
	}
	defer tx.Rollback(ctx)

	repo = repository.New(tx)

	maquina, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{
		ID:       id,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, helper.ErrNaoEncontrado
		}
		return nil, nil, helper.TraduzErroPostgres(err)
	}

	if strings.TrimSpace(payload.Confirmacao) != maquina.NumeroPatrimonio {
		return nil, nil, fmt.Errorf("%w: o patrimônio digitado não confere com o da máquina", helper.ErrValidacao)
	}

	anexos, err = repo.ListarChavesAnexosDaMaquina(ctx, repository.ListarChavesAnexosDaMaquinaParams{
		MaquinaID: &id,
		TenantID:  tenantId,
	})
	if err != nil {
		return nil, nil, helper.TraduzErroPostgres(err)
	}

	if _, err := repo.ExcluirMaquinaDefinitivo(ctx, repository.ExcluirMaquinaDefinitivoParams{
		ID:       id,
		TenantID: tenantId,
	}); err != nil {
		return nil, nil, helper.TraduzErroPostgres(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return maquina.FotoChave, anexos, nil
}

// traduzErroMaquina separa as duas UNIQUE de maquina: patrimônio e série dão
// 23505 iguais, e só o nome da constraint diz qual o usuário tem de corrigir.
func traduzErroMaquina(err error) error {

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "uq_maquina_serie" {
		return helper.ErrSerieDuplicada
	}

	return helper.TraduzErroPostgres(err)
}
