package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// OrdemServicoService é só leitura por enquanto. A OS não nasce aqui: quem a
// cria é SolicitacaoService.AbrirOS (a aprovação do Gestor), e o ciclo de vida
// -- iniciar/pausar/retomar/acionar-terceiro/encerrar/custo -- é a fase 2.
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
