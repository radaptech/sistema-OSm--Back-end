package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// Só leitura: a OS nasce em SolicitacaoService.AbrirOS, e o ciclo de vida é fase 2.
// Guarda o Pool e não um Querier porque essas escritas vão precisar de WithTx.
type OrdemServicoService struct {
	Pool *pgxpool.Pool
}

func NewRepoOrdemServico(pool *pgxpool.Pool) *OrdemServicoService {

	return &OrdemServicoService{
		Pool: pool,
	}
}

// Struct e não parâmetros soltos como no resto do pacote: trocar LojaId e
// TecnicoId de lugar numa assinatura posicional compila e devolve lista errada.
// `pagina` fica de fora -- o front pagina no cliente.
type FiltrosOrdemServico struct {
	Status     []string
	Tipo       *string
	Finalizada *bool
	LojaId     *int64
	TecnicoId  *int64
	Busca      *string
}

// ⚠️ O escopo sai de escopoDe(usuarioId, perfil), nunca dos filtros: o RBAC da
// rota libera três perfis, e quem recorta é o WHERE. Filtro do cliente só estreita.
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

// Pausas da página inteira numa ida só: por OS seria N+1, e num JOIN a 1:N
// duplicaria a OS. Slice não-nil mesmo vazia -- `null` quebra o .map do front.
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
