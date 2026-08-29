package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type PreventivaService struct {
	Pool *pgxpool.Pool
}

func NewRepoPreventiva(pool *pgxpool.Pool) *PreventivaService {

	return &PreventivaService{
		Pool: pool,
	}
}

// gravarPreventivas insere a lista de preventivas de uma máquina.
//
// Recebe *repository.Queries e não o Pool de propósito: é assim que
// CadastrarMaquina/AtualizarMaquina conseguem chamá-la de dentro da transação
// que já abriram -- máquina e preventivas gravam juntas ou não gravam. Um
// método de PreventivaService abriria transação própria e quebraria isso.
// Mesmo padrão de gravarEscopo em EscopoPerfilService.go.
//
// maquinaID vem de quem chama, não de p.MaquinaId: no cadastro a máquina acaba
// de ser inserida e o front mandou 0 no payload.
func gravarPreventivas(ctx context.Context, repo *repository.Queries, tenantID, maquinaID int64, preventivas []model.PreventivaPayload) error {

	// A regra "máquina exige ao menos uma preventiva" é do servidor, não só do
	// Zod: sem isto um POST direto (Postman) cria máquina sem preventiva
	// nenhuma e a regra de negócio passa a existir só no navegador.
	if len(preventivas) == 0 {
		return fmt.Errorf("%w: a máquina precisa de pelo menos uma manutenção preventiva", helper.ErrValidacao)
	}

	for _, p := range preventivas {
		descricao := strings.TrimSpace(p.Descricao)
		if descricao == "" {
			return fmt.Errorf("%w: a descrição da preventiva não pode ficar em branco", helper.ErrValidacao)
		}
		// ck_intervalo (intervalo_dias > 0) recusaria no banco, mas como 422; o
		// valor veio do formulário, então é erro de preenchimento (400).
		if p.IntervaloDias <= 0 {
			return fmt.Errorf("%w: o intervalo da preventiva deve ser de pelo menos 1 dia", helper.ErrValidacao)
		}
		if p.ProximaData == nil || p.ProximaData.IsZero() {
			return fmt.Errorf("%w: informe a próxima data da preventiva", helper.ErrValidacao)
		}

		_, err := repo.CriarPreventiva(ctx, repository.CriarPreventivaParams{
			TenantID:      tenantID,
			MaquinaID:     maquinaID,
			Descricao:     descricao,
			IntervaloDias: p.IntervaloDias,
			ProximaData:   pgtype.Date{Time: p.ProximaData.Time(), Valid: true},
			Ativa:         p.Ativa,
		})
		if err != nil {
			return helper.TraduzErroPostgres(err)
		}
	}

	return nil
}

// CadastrarPreventiva é POST /preventivas -- a preventiva avulsa, criada fora
// do formulário de máquina. Roda em transação porque gravarPreventivas é
// compartilhada com o caminho da máquina, que precisa de uma.
func (s *PreventivaService) CadastrarPreventiva(ctx context.Context, tenantID int64, payload model.PreventivaPayload) (model.Preventiva, error) {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.Preventiva{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	// A FK composta garante o tenant, não que a máquina exista: sem este cheque
	// um maquinaId inexistente vira 23503 e sobe como 422 sem dizer o quê.
	if _, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{
		ID:       payload.MaquinaId,
		TenantID: tenantID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Preventiva{}, fmt.Errorf("%w: máquina %d não existe neste tenant", helper.ErrConflitoIntegridade, payload.MaquinaId)
		}
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}

	if err := gravarPreventivas(ctx, repo, tenantID, payload.MaquinaId, []model.PreventivaPayload{payload}); err != nil {
		return model.Preventiva{}, err
	}

	// gravarPreventivas não devolve id (serve uma lista), então a resposta sai
	// da listagem da máquina -- a recém-criada é a última pela ordenação.
	// EscopoUsuarioID nil: esta releitura é interna, para achar o id da linha
	// recém-criada -- não é listagem de ninguém, não há escopo a aplicar.
	criadas, err := repo.ListarPreventivas(ctx, repository.ListarPreventivasParams{
		TenantID:  tenantID,
		MaquinaID: &payload.MaquinaId,
	})
	if err != nil {
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}
	if len(criadas) == 0 {
		return model.Preventiva{}, helper.ErrNaoEncontrado
	}

	var nova repository.ListarPreventivasRow
	for _, p := range criadas {
		if p.ID > nova.ID {
			nova = p
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Preventiva{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return model.MontarPreventiva(nova), nil
}

// ListarPreventivas é GET /preventivas (?maquinaId=). Só as ativas -- ver
// preventiva.sql -- e só as que o escopo de quem chama alcança: a aba
// "Manutenção Prev." do gestor tem que trazer as lojas dele, não o tenant.
func (s *PreventivaService) ListarPreventivas(ctx context.Context, tenantID, usuarioID int64, perfil string, maquinaID *int64) ([]model.Preventiva, error) {

	repo := repository.New(s.Pool)

	preventivas, err := repo.ListarPreventivas(ctx, repository.ListarPreventivasParams{
		TenantID:        tenantID,
		MaquinaID:       maquinaID,
		EscopoUsuarioID: escopoDe(usuarioID, perfil),
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	// Não-nil: o front tipa PreventivaListada[] e nil viraria `null` no JSON.
	dto := make([]model.Preventiva, 0, len(preventivas))
	for _, p := range preventivas {
		dto = append(dto, model.MontarPreventiva(p))
	}

	return dto, nil
}

// ObterPreventiva é GET /preventivas/:id.
func (s *PreventivaService) ObterPreventiva(ctx context.Context, tenantID, id int64) (model.Preventiva, error) {

	repo := repository.New(s.Pool)

	preventiva, err := repo.ObterPreventivaPorID(ctx, repository.ObterPreventivaPorIDParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Preventiva{}, helper.ErrNaoEncontrado
		}
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}

	return model.MontarPreventiva(repository.ListarPreventivasRow(preventiva)), nil
}

// AtualizarPreventiva é PUT /preventivas/:id. Sem maquina_id: mover a
// preventiva de máquina deixaria as solicitações que ela já gerou apontando
// para outra máquina (ver preventiva.sql). O front manda o campo; o service
// ignora.
func (s *PreventivaService) AtualizarPreventiva(ctx context.Context, tenantID, id int64, payload model.PreventivaPayload) (model.Preventiva, error) {

	descricao := strings.TrimSpace(payload.Descricao)
	if descricao == "" {
		return model.Preventiva{}, fmt.Errorf("%w: a descrição da preventiva não pode ficar em branco", helper.ErrValidacao)
	}
	if payload.IntervaloDias <= 0 {
		return model.Preventiva{}, fmt.Errorf("%w: o intervalo da preventiva deve ser de pelo menos 1 dia", helper.ErrValidacao)
	}
	if payload.ProximaData == nil || payload.ProximaData.IsZero() {
		return model.Preventiva{}, fmt.Errorf("%w: informe a próxima data da preventiva", helper.ErrValidacao)
	}

	repo := repository.New(s.Pool)

	if _, err := repo.AtualizarPreventiva(ctx, repository.AtualizarPreventivaParams{
		ID:            id,
		TenantID:      tenantID,
		Descricao:     descricao,
		IntervaloDias: payload.IntervaloDias,
		ProximaData:   pgtype.Date{Time: payload.ProximaData.Time(), Valid: true},
		Ativa:         payload.Ativa,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Preventiva{}, helper.ErrNaoEncontrado
		}
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}

	// Relê para devolver os nomes denormalizados: RETURNING não enxerga JOIN.
	return s.ObterPreventiva(ctx, tenantID, id)
}

// DesativarPreventiva é DELETE /preventivas/:id -- soft delete (ativa = false).
//
// Soft não é só convenção da casa aqui: fk_solicitacao_preventiva não tem
// ON DELETE, então preventiva que já gerou solicitação automática recusaria o
// DELETE com 23503 (ver preventiva.sql).
func (s *PreventivaService) DesativarPreventiva(ctx context.Context, tenantID, id int64) error {

	repo := repository.New(s.Pool)

	linhas, err := repo.DesativarPreventiva(ctx, repository.DesativarPreventivaParams{
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

// AbrirSolicitacoesDePreventivasVencidas percorre as preventivas cuja
// proxima_data já passou e abre uma Solicitação (não uma OS) para cada uma. É
// o miolo do subcomando de CLI `preventivas-vencidas`, chamado pelo Railway
// Cron -- ver "Abertura automática de solicitação por preventiva" no CLAUDE.md.
//
// A solicitação nasce Pendente e cai na fila do Gestor: a OS só existe quando
// ele aprova com técnico e urgência. Criar OS direto pularia a aprovação, que é
// o ponto do fluxo.
//
// Sem tenantID no parâmetro, diferente de todo o resto do pacote: não há
// request nem token: o job varre todos os tenants e o tenant_id vem na linha da
// preventiva (ver ListarPreventivasVencidas).
//
// Devolve quantas solicitações abriu e os erros das que falharam, juntados com
// errors.Join -- falha em uma preventiva não pode impedir as outras de rodarem,
// então nenhuma delas aborta o laço. Erro não-nil aqui é resultado parcial, não
// fracasso: quem chama loga e segue.
func (s *PreventivaService) AbrirSolicitacoesDePreventivasVencidas(ctx context.Context) (int, error) {

	// Leitura fora de transação: cada preventiva ganha a sua logo abaixo.
	vencidas, err := repository.New(s.Pool).ListarPreventivasVencidas(ctx)
	if err != nil {
		return 0, helper.TraduzErroPostgres(err)
	}

	var criadas int
	var falhas []error

	for _, p := range vencidas {
		err := s.abrirSolicitacaoDaPreventiva(ctx, p)

		switch {
		case err == nil:
			criadas++

		// uq_preventiva_pendente barrando o INSERT significa que outra execução
		// do job criou a solicitação entre o SELECT e o INSERT desta -- duas
		// réplicas, ou dois disparos do cron colados. É exatamente o serviço que
		// o índice existe para prestar: não é falha, e refazer não tem sentido
		// (a solicitação que interessa já está na fila do Gestor).
		case errors.Is(err, helper.ErrDadoDuplicado):

		default:
			falhas = append(falhas, fmt.Errorf("preventiva %d (tenant %d): %w", p.ID, p.TenantID, err))
		}
	}

	return criadas, errors.Join(falhas...)
}

// abrirSolicitacaoDaPreventiva grava a solicitação de uma preventiva vencida e
// avança o ciclo dela.
//
// Transação própria por preventiva, e não uma para o lote todo, pelos dois
// lados: uma linha ruim não pode derrubar as outras 200, e as duas escritas
// aqui dentro têm que ser atômicas entre si. Separadas, avançar a data com o
// INSERT falhando pularia o ciclo em silêncio, e inserir sem avançar faria a
// preventiva disparar de novo no instante em que o Gestor convertesse a
// solicitação.
func (s *PreventivaService) abrirSolicitacaoDaPreventiva(ctx context.Context, p repository.ListarPreventivasVencidasRow) error {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	if _, err := repo.CriarSolicitacaoPreventiva(ctx, repository.CriarSolicitacaoPreventivaParams{
		TenantID:     p.TenantID,
		MaquinaID:    p.MaquinaID,
		SetorID:      p.SetorID,
		PreventivaID: p.ID,
		Descricao:    "Manutenção preventiva: " + p.Descricao,
	}); err != nil {
		return helper.TraduzErroPostgres(err)
	}

	// A query soma o intervalo a partir da proxima_data vencida, não de hoje --
	// senão um ciclo processado com atraso arrastaria todos os seguintes.
	if _, err := repo.AvancarProximaData(ctx, repository.AvancarProximaDataParams{
		ID:       p.ID,
		TenantID: p.TenantID,
	}); err != nil {
		return helper.TraduzErroPostgres(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return nil
}
