package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// OrdemServicoService cobre a leitura (Listar/Indicadores) e, a partir de
// Iniciar, o ciclo de vida (iniciar/pausar/retomar/acionar-terceiro/encerrar/
// custo). A OS não NASCE aqui: quem cria é SolicitacaoService.AbrirOS (a
// aprovação do Gestor) -- este service só recebe a OS já existente.
//
// Guarda o *pgxpool.Pool e não um Querier pelo mesmo motivo do resto do
// pacote: as escritas que vêm depois precisam de transação, e Querier não
// expõe WithTx.
type OrdemServicoService struct {
	Pool *pgxpool.Pool
}

func NewRepoOrdemServico(pool *pgxpool.Pool) *OrdemServicoService {

	return &OrdemServicoService{
		Pool: pool,
	}
}

// FiltrosOrdemServico é o que GET /ordens-servico aceita por query string.
// Struct, e não sete parâmetros soltos como em ListarSolicitacoes: são filtros
// demais para uma assinatura posicional continuar legível, e trocar dois
// *int64 vizinhos de lugar (LojaId/TecnicoId) compila e devolve a lista
// errada, calado.
//
// `pagina` do contrato do front não entra: a resposta é array simples e o
// front pagina no cliente, mesmo padrão de ListarSolicitacoes/ListarMaquinas.
type FiltrosOrdemServico struct {
	Status     []string
	Tipo       *string
	Finalizada *bool
	LojaId     *int64
	TecnicoId  *int64
	Busca      *string
}

// ListarOrdensServico é GET /ordens-servico -- um endpoint para os três
// painéis (Gestor, Técnico, Administrador), o que muda é o filtro. Recortada
// pelo escopo de quem chama, igual a ListarSolicitacoes/ListarMaquinas: o RBAC
// da rota libera os três perfis e quem recorta é o WHERE.
//
// ⚠️ O escopo sai de escopoDe(usuarioId, perfil), nunca de FiltrosOrdemServico
// -- os filtros vêm do cliente e só sabem estreitar. Um Técnico mandando
// ?tecnicoId= de outro técnico recebe lista vazia se as OS dele estiverem fora
// do escopo, não a lista do colega.
func (s *OrdemServicoService) ListarOrdensServico(ctx context.Context, tenantId, usuarioId int64, perfil string, filtros FiltrosOrdemServico) ([]model.OrdemServico, error) {

	repo := repository.New(s.Pool)

	linhas, err := repo.ListarOrdensServico(ctx, repository.ListarOrdensServicoParams{
		TenantID:        tenantId,
		Status:          filtros.Status,
		Tipo:            (*repository.TipoOs)(filtros.Tipo),
		Finalizada:      filtros.Finalizada,
		LojaID:          filtros.LojaId,
		TecnicoID:       filtros.TecnicoId,
		Busca:           filtros.Busca,
		EscopoUsuarioID: escopoDe(usuarioId, perfil),
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	return montarOrdensServicoEmLote(ctx, repo, linhas)
}

// montarOrdensServicoEmLote busca as pausas da página inteira numa ida só e
// monta a resposta -- mesmo desenho de montarSolicitacoesEmLote: uma query por
// OS seria N+1, e um JOIN 1:N duplicaria a OS por pausa.
//
// Devolve slice não-nil mesmo vazia: o front tipa OrdemServico[] e `null`
// quebra o .map.
func montarOrdensServicoEmLote(ctx context.Context, repo *repository.Queries, linhas []repository.ListarOrdensServicoRow) ([]model.OrdemServico, error) {

	dados := make([]model.OrdemServico, 0, len(linhas))
	if len(linhas) == 0 {
		return dados, nil
	}

	ids := make([]int64, len(linhas))
	for i, l := range linhas {
		ids[i] = l.ID
	}

	pausas, err := repo.ObterPausasDasOrdensServico(ctx, ids)
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}
	pausasPorOS := make(map[int64][]repository.OsPausa, len(linhas))
	for _, p := range pausas {
		pausasPorOS[p.OrdemServicoID] = append(pausasPorOS[p.OrdemServicoID], p)
	}

	for _, l := range linhas {
		dados = append(dados, model.MontarOrdemServico(l, pausasPorOS[l.ID]))
	}

	return dados, nil
}

// montarOrdemServicoUnica remonta UMA OrdemServico completa e denormalizada
// depois de uma escrita do ciclo de vida (Iniciar e as que vêm a seguir),
// reaproveitando o SELECT com todos os JOINs de ListarOrdensServico (via
// `id`, ver a nota na query) em vez de duplicá-lo numa query só pra isso --
// mesmo motivo de montarOrdensServicoEmLote existir para a listagem.
//
// `repo` tem que ser o *repository.Queries da MESMA transação que acabou de
// escrever -- lido com o Pool direto, a linha ainda não commitada não
// apareceria.
func montarOrdemServicoUnica(ctx context.Context, repo *repository.Queries, tenantId, ordemServicoId int64) (model.OrdemServico, error) {

	linhas, err := repo.ListarOrdensServico(ctx, repository.ListarOrdensServicoParams{
		TenantID: tenantId,
		ID:       &ordemServicoId,
	})
	if err != nil {
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	dados, err := montarOrdensServicoEmLote(ctx, repo, linhas)
	if err != nil {
		return model.OrdemServico{}, err
	}
	if len(dados) == 0 {
		return model.OrdemServico{}, fmt.Errorf("ordem de serviço %d não encontrada depois da escrita", ordemServicoId)
	}

	return dados[0], nil
}

// Iniciar é POST /ordens-servico/:id/iniciar -- Aberta -> Em Andamento. Só o
// Técnico dono da OS. Mesmo esqueleto de AbrirOS/Rejeitar
// (solicitacaoOs.go): abre transação, lê o estado atual, checa dono e
// status, escreve, remonta a resposta, comita.
func (s *OrdemServicoService) Iniciar(ctx context.Context, tenantId, atorId, ordemServicoId int64) (model.OrdemServico, error) {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	atual, err := repo.ObterOrdemServicoPorID(ctx, repository.ObterOrdemServicoPorIDParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, helper.ErrNaoEncontrado
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	// Dono, não RBAC: OS de outro Técnico é "não encontrada", não
	// "proibida" -- mesmo critério de escopo do resto do sistema
	// (ver ObterOrdemServicoPorID). 403 é só perfil errado, e o perfil
	// (técnico) já está certo -- é a rota (Permitir) quem garante isso.
	if atual.TecnicoID != atorId {
		return model.OrdemServico{}, helper.ErrNaoEncontrado
	}

	if atual.Status != repository.StatusOsAberta {
		return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço já está %s", helper.ErrConflitoIntegridade, atual.Status)
	}

	if _, err := repo.IniciarOrdemServico(ctx, repository.IniciarOrdemServicoParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	}); err != nil {
		// A checagem acima já devolveu um erro que diz o porquê; isto aqui é
		// só a rede contra corrida (duas abas do mesmo Técnico) -- mesmo
		// papel do `AND status = 'Pendente'` em AbrirOS.
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço já não está mais aberta", helper.ErrConflitoIntegridade)
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	ordem, err := montarOrdemServicoUnica(ctx, repo, tenantId, ordemServicoId)
	if err != nil {
		return model.OrdemServico{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return ordem, nil
}

// Pausar é POST /ordens-servico/:id/pausar -- (Aberta ou Em Andamento) ->
// Pausada, com o motivo. Aceita as duas origens porque o Técnico pode
// pausar antes mesmo de iniciar (docs/modelagem, StatusRetomavel) -- é por
// isso que `atual.Status` (lido ANTES de escrever) vira `status_anterior`
// em CriarPausa, nunca um valor fixo como em Iniciar.
func (s *OrdemServicoService) Pausar(ctx context.Context, tenantId, atorId, ordemServicoId int64, motivoBruto string) (model.OrdemServico, error) {

	motivo, err := campoObrigatorio("motivo", motivoBruto)
	if err != nil {
		return model.OrdemServico{}, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	atual, err := repo.ObterOrdemServicoPorID(ctx, repository.ObterOrdemServicoPorIDParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, helper.ErrNaoEncontrado
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if atual.TecnicoID != atorId {
		return model.OrdemServico{}, helper.ErrNaoEncontrado
	}

	if atual.Status != repository.StatusOsAberta && atual.Status != repository.StatusOsEmAndamento {
		return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço já está %s", helper.ErrConflitoIntegridade, atual.Status)
	}

	if _, err := repo.PausarOrdemServico(ctx, repository.PausarOrdemServicoParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço não está mais num estado pausável", helper.ErrConflitoIntegridade)
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if _, err := repo.CriarPausa(ctx, repository.CriarPausaParams{
		OrdemServicoID: ordemServicoId,
		StatusAnterior: atual.Status,
		Motivo:         motivo,
	}); err != nil {
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	ordem, err := montarOrdemServicoUnica(ctx, repo, tenantId, ordemServicoId)
	if err != nil {
		return model.OrdemServico{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return ordem, nil
}

// Retomar é POST /ordens-servico/:id/retomar -- Pausada -> o status que a OS
// tinha antes de pausar ('Aberta' ou 'Em Andamento', nunca fixo -- por isso
// lê a pausa aberta primeiro pra saber pra onde voltar, em vez de assumir
// 'Em Andamento' como Iniciar assume 'Aberta').
func (s *OrdemServicoService) Retomar(ctx context.Context, tenantId, atorId, ordemServicoId int64) (model.OrdemServico, error) {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	atual, err := repo.ObterOrdemServicoPorID(ctx, repository.ObterOrdemServicoPorIDParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, helper.ErrNaoEncontrado
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if atual.TecnicoID != atorId {
		return model.OrdemServico{}, helper.ErrNaoEncontrado
	}

	if atual.Status != repository.StatusOsPausada {
		return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço já está %s", helper.ErrConflitoIntegridade, atual.Status)
	}

	// uq_pausa_aberta garante no máximo uma linha com retomada_em NULL por OS
	// -- se o status é 'Pausada', ela tem que existir. pgx.ErrNoRows aqui
	// seria dado inconsistente, não uma corrida esperada (ao contrário dos
	// outros ErrNoRows deste arquivo), então não vira ErrConflitoIntegridade
	// silencioso: fica erro genérico, que vai pro log em vez de virar 409
	// educado pra um problema que não é do usuário.
	pausa, err := repo.ObterPausaAbertaDaOrdemServico(ctx, ordemServicoId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("ordem de serviço %d está Pausada sem pausa em aberto", ordemServicoId)
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if _, err := repo.RetomarOrdemServico(ctx, repository.RetomarOrdemServicoParams{
		Status:   pausa.StatusAnterior,
		ID:       ordemServicoId,
		TenantID: tenantId,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço não está mais pausada", helper.ErrConflitoIntegridade)
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if _, err := repo.FecharPausaAberta(ctx, pausa.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("%w: pausa já foi fechada", helper.ErrConflitoIntegridade)
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	ordem, err := montarOrdemServicoUnica(ctx, repo, tenantId, ordemServicoId)
	if err != nil {
		return model.OrdemServico{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return ordem, nil
}

// AcionarTerceiro é POST /ordens-servico/:id/acionar-terceiro -- promove
// tipo pra 'terceiros' e grava a empresa. Não mexe em status_execucao
// (docs/modelagem, 1.4.2): acionar não é transição de execução, o ciclo
// iniciar/pausar/retomar/encerrar segue igual por cima -- por isso, ao
// contrário de Iniciar/Pausar/Retomar, não checa `atual.Status` contra um
// conjunto fixo, só que a OS não esteja Concluída.
func (s *OrdemServicoService) AcionarTerceiro(ctx context.Context, tenantId, atorId, ordemServicoId, empresaTerceirizadaId int64) (model.OrdemServico, error) {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	atual, err := repo.ObterOrdemServicoPorID(ctx, repository.ObterOrdemServicoPorIDParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, helper.ErrNaoEncontrado
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if atual.TecnicoID != atorId {
		return model.OrdemServico{}, helper.ErrNaoEncontrado
	}

	if atual.Tipo == repository.TipoOsTerceiros {
		return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço já foi encaminhada a uma empresa terceirizada", helper.ErrConflitoIntegridade)
	}
	if atual.Status == repository.StatusOsConcluda {
		return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço já está concluída", helper.ErrConflitoIntegridade)
	}

	if _, err := repo.AcionarTerceiro(ctx, repository.AcionarTerceiroParams{
		EmpresaTerceirizadaID: &empresaTerceirizadaId,
		ID:                    ordemServicoId,
		TenantID:              tenantId,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço não pode mais ser encaminhada", helper.ErrConflitoIntegridade)
		}
		// Cobre também empresa_terceirizada_id inexistente/de outro tenant:
		// a FK composta (tenant_id, empresa_terceirizada_id) vira violação
		// de FK aqui, TraduzErroPostgres já traduz pra ErrConflitoIntegridade
		// -- mesmo caminho do técnico inexistente em AbrirOS.
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	ordem, err := montarOrdemServicoUnica(ctx, repo, tenantId, ordemServicoId)
	if err != nil {
		return model.OrdemServico{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return ordem, nil
}

// Encerrar é POST /ordens-servico/:id/encerrar -- Em Andamento -> Concluída,
// gravando os_encerramento E os_custo na mesma transação (docs/modelagem,
// 2.3 revisão 4: "os dois momentos deixaram de ser sequenciais"). O
// Administrador só CORRIGE depois, em POST /ordens-servico/:id/custo (fora
// daqui) -- nota fiscal inclusive, que por isso nem entra nesta escrita.
//
// `tipo` de CriarEncerramento/CriarCusto vem do `Tipo` que EncerrarOrdemServico
// devolve (o UPDATE não mexe nele, só reflete o estado atual), nunca de
// `atual.Tipo` lido antes: entre a leitura e a escrita a OS não muda de tipo
// sozinha, mas usar o valor que voltou do próprio INSERT que vai alimentar as
// duas FKs compostas é a fonte mais direta, mesmo espírito de
// montarOrdemServicoUnica reler dentro da mesma tx em vez de confiar num
// valor carregado de longe.
func (s *OrdemServicoService) Encerrar(ctx context.Context, tenantId, atorId, ordemServicoId int64, payload model.EncerramentoOrdemServicoPayload) (model.OrdemServico, error) {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	atual, err := repo.ObterOrdemServicoPorID(ctx, repository.ObterOrdemServicoPorIDParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, helper.ErrNaoEncontrado
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if atual.TecnicoID != atorId {
		return model.OrdemServico{}, helper.ErrNaoEncontrado
	}

	if atual.Status != repository.StatusOsEmAndamento {
		return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço já está %s", helper.ErrConflitoIntegridade, atual.Status)
	}

	// ck_custo_por_tipo espelhado aqui: sem isto o CHECK do banco ainda
	// barra, mas a mensagem vira "regra de validação do banco violada"
	// genérica em vez de dizer qual campo está errado.
	if atual.Tipo == repository.TipoOsMaquinario && payload.CustoHoraTecnico == nil {
		return model.OrdemServico{}, fmt.Errorf("%w: custoHoraTecnico é obrigatório em OS de maquinário", helper.ErrValidacao)
	}
	if atual.Tipo != repository.TipoOsMaquinario && payload.CustoHoraTecnico != nil {
		return model.OrdemServico{}, fmt.Errorf("%w: custoHoraTecnico só existe em OS de maquinário", helper.ErrValidacao)
	}

	encerrada, err := repo.EncerrarOrdemServico(ctx, repository.EncerrarOrdemServicoParams{
		ID:       ordemServicoId,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("%w: ordem de serviço não está mais em andamento", helper.ErrConflitoIntegridade)
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	if _, err := repo.CriarEncerramento(ctx, repository.CriarEncerramentoParams{
		TenantID:          tenantId,
		OrdemServicoID:    ordemServicoId,
		Tipo:              encerrada.Tipo,
		TipoDefeito:       repository.TipoDefeito(payload.TipoDefeito),
		EncerradoPorID:    atorId,
		DefeitoConstatado: payload.DefeitoConstatado,
		CausaRaiz:         payload.CausaRaiz,
		Solucao:           payload.Solucao,
	}); err != nil {
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	var custoHoraTecnico pgtype.Float8
	if payload.CustoHoraTecnico != nil {
		custoHoraTecnico = pgtype.Float8{Float64: *payload.CustoHoraTecnico, Valid: true}
	}

	if _, err := repo.CriarCusto(ctx, repository.CriarCustoParams{
		TenantID:         tenantId,
		OrdemServicoID:   ordemServicoId,
		Tipo:             encerrada.Tipo,
		CustoHoraTecnico: custoHoraTecnico,
		CustoManutencao:  pgtype.Float8{Float64: payload.CustoManutencao, Valid: true},
		LancadoPorID:     atorId,
	}); err != nil {
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	ordem, err := montarOrdemServicoUnica(ctx, repo, tenantId, ordemServicoId)
	if err != nil {
		return model.OrdemServico{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return ordem, nil
}

// ObterIndicadoresDaMaquina é GET /indicadores/maquinas/:id -- o Painel de
// Indicadores do Gestor. Mora aqui, e não num IndicadorService próprio, porque
// tudo que ele lê é histórico de OS: o pacote já teria que expor a mesma query
// para si mesmo.
//
// São duas idas ao banco de propósito. A primeira existe pelo ESCOPO: a máquina
// tem que estar ao alcance de quem chama, e a segunda query sozinha não sabe
// dizer a diferença entre "máquina de outra loja" e "máquina sem OS encerrada"
// -- as duas devolvem zero linhas. Sem esse cheque, o Gestor leria custo e
// indisponibilidade de qualquer máquina do tenant enumerando id, e ainda por
// cima disfarçado de painel vazio.
func (s *OrdemServicoService) ObterIndicadoresDaMaquina(ctx context.Context, tenantId, maquinaId, usuarioId int64, perfil string) (model.IndicadoresMaquina, error) {

	repo := repository.New(s.Pool)

	if _, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{
		ID:              maquinaId,
		TenantID:        tenantId,
		EscopoUsuarioID: escopoDe(usuarioId, perfil),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.IndicadoresMaquina{}, helper.ErrNaoEncontrado
		}
		return model.IndicadoresMaquina{}, helper.TraduzErroPostgres(err)
	}

	historico, err := repo.ListarHistoricoOsDaMaquina(ctx, repository.ListarHistoricoOsDaMaquinaParams{
		TenantID:  tenantId,
		MaquinaID: maquinaId,
	})
	if err != nil {
		return model.IndicadoresMaquina{}, helper.TraduzErroPostgres(err)
	}

	// Máquina existe e está no escopo, mas ainda não teve OS encerrada: zeros,
	// não 404. É o estado normal de máquina recém-cadastrada.
	return model.MontarIndicadoresMaquina(maquinaId, historico), nil
}
