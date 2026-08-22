package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type EmpresaTerceirizadaService struct {
	Pool *pgxpool.Pool
}

func NewRepoEmpresaTerceirizada(pool *pgxpool.Pool) *EmpresaTerceirizadaService {

	return &EmpresaTerceirizadaService{
		Pool: pool,
	}
}

// montarEmpresaTerceirizada é a única tradução de linha do banco para resposta
// -- mesmo motivo de montarLoja/montarSetor: espalhar isso em cada método já
// fez o id sumir de metade delas.
func montarEmpresaTerceirizada(e repository.EmpresaTerceirizada) model.EmpresaTerceirizada {

	return model.EmpresaTerceirizada{
		Id:            e.ID,
		Nome:          e.Nome,
		Especialidade: e.Especialidade,
		Telefone:      e.Telefone,
		Ativa:         e.Ativa,
	}
}

// CadastrarEmpresaTerceirizada é POST /empresas-terceirizadas. Sem transação, e
// não é esquecimento: é uma tabela só, sem filho para gravar junto (diferente
// de máquina+preventivas e de usuário+escopo). Mesmo caso de CadastrarLoja.
func (e *EmpresaTerceirizadaService) CadastrarEmpresaTerceirizada(ctx context.Context, tenantID int64, payload model.NovaEmpresaTerceirizadaPayload) (model.EmpresaTerceirizada, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.EmpresaTerceirizada{}, err
	}

	repo := repository.New(e.Pool)

	empresa, err := repo.CriarEmpresaTerceirizada(ctx, repository.CriarEmpresaTerceirizadaParams{
		TenantID: tenantID,
		Nome:     nome,
		// textoOuNil e não nomeValido: os dois são OPCIONAIS. nomeValido recusa
		// vazio, e o formulário manda "" no campo que ninguém preencheu -- o
		// cadastro inteiro passaria a exigir especialidade, com a mensagem
		// falando em "nome". E o ponteiro pode ser nil (cliente que omite o
		// campo), então desreferenciar aqui é panic.
		Especialidade: textoOuNil(payload.Especialidade),
		Telefone:      textoOuNil(payload.Telefone),
	})
	if err != nil {
		// Nome duplicado no tenant é 23505 -> ErrDadoDuplicado -> 409. Sem a
		// tradução o controller cai no default e responde 500.
		return model.EmpresaTerceirizada{}, helper.TraduzErroPostgres(err)
	}

	return montarEmpresaTerceirizada(empresa), nil
}

// ObterEmpresaTerceirizada é GET /empresas-terceirizadas/:id -- o que a tela de
// edição carrega. A query não filtra `ativa` de propósito: o formulário precisa
// ler o registro mesmo desativado.
//
// O errors.Is(pgx.ErrNoRows) vem ANTES do TraduzErroPostgres porque o helper só
// entende código do pgconn e embrulha o resto com %v, quebrando o errors.Is do
// controller -- id inexistente viraria 500 em vez de 404.
func (e *EmpresaTerceirizadaService) ObterEmpresaTerceirizada(ctx context.Context, tenantID, id int64) (model.EmpresaTerceirizada, error) {

	repo := repository.New(e.Pool)

	empresa, err := repo.ObterEmpresaTerceirizadaPorID(ctx, repository.ObterEmpresaTerceirizadaPorIDParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.EmpresaTerceirizada{}, helper.ErrNaoEncontrado
		}
		return model.EmpresaTerceirizada{}, helper.TraduzErroPostgres(err)
	}

	return montarEmpresaTerceirizada(empresa), nil
}

// ListarEmpresasTerceirizadas é GET /empresas-terceirizadas: array simples, sem
// paginação nem filtro -- servicoEmpresasTerceirizadas.listar() não manda
// parâmetro e a tela do Administrador pagina no cliente.
//
// Sem escopo de acesso, diferente de máquina e preventiva: empresa terceirizada
// não pende de loja nem setor, é do tenant inteiro. Por isso não recebe
// usuarioId/perfil.
func (e *EmpresaTerceirizadaService) ListarEmpresasTerceirizadas(ctx context.Context, tenantID int64) ([]model.EmpresaTerceirizada, error) {

	repo := repository.New(e.Pool)

	empresas, err := repo.ListarEmpresasTerceirizadas(ctx, tenantID)
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	// Não-nil: o front tipa EmpresaTerceirizada[] e nil viraria null no JSON.
	dto := make([]model.EmpresaTerceirizada, 0, len(empresas))
	for _, et := range empresas {
		dto = append(dto, montarEmpresaTerceirizada(et))
	}

	return dto, nil
}

// AtualizarEmpresaTerceirizada é PUT /empresas-terceirizadas/:id. `ativa` fica
// de fora: reativar não existe pela API, e desativar tem rota própria.
func (e *EmpresaTerceirizadaService) AtualizarEmpresaTerceirizada(ctx context.Context, tenantId, id int64, payload model.NovaEmpresaTerceirizadaPayload) (model.EmpresaTerceirizada, error) {

	nome, err := nomeValido(payload.Nome)
	if err != nil {
		return model.EmpresaTerceirizada{}, err
	}

	repo := repository.New(e.Pool)

	empresa, err := repo.AtualizarEmpresaTerceirizada(ctx, repository.AtualizarEmpresaTerceirizadaParams{
		ID:            id,
		TenantID:      tenantId,
		Nome:          nome,
		Especialidade: textoOuNil(payload.Especialidade),
		Telefone:      textoOuNil(payload.Telefone),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.EmpresaTerceirizada{}, helper.ErrNaoEncontrado
		}
		return model.EmpresaTerceirizada{}, helper.TraduzErroPostgres(err)
	}

	return montarEmpresaTerceirizada(empresa), nil
}

// DesativarEmpresaTerceirizada é DELETE /empresas-terceirizadas/:id -- soft
// delete (ativa = false).
//
// Sem cheque de dependente antes, diferente de DesativarLoja (que recusa com
// setor ativo): esta entidade não tem filho. Ela é REFERENCIADA por
// ordem_servico, mas como o delete é soft a FK nunca reclama e a OS antiga
// continua mostrando quem executou o serviço.
//
// Zero linhas: id inexistente ou de outro tenant (o WHERE filtra os dois). Já
// desativada casa a linha e conta 1 -- desativar de novo é idempotente.
func (e *EmpresaTerceirizadaService) DesativarEmpresaTerceirizada(ctx context.Context, tenantId, id int64) error {

	repo := repository.New(e.Pool)

	linhas, err := repo.DesativarEmpresaTerceirizada(ctx, repository.DesativarEmpresaTerceirizadaParams{
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
