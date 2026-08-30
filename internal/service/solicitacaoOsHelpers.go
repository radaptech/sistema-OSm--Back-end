package service

// Funções livres de apoio a SolicitacaoService (solicitacaoOs.go): nada aqui
// é método porque a maioria recebe *repository.Queries em vez de usar
// s.Pool -- quem chama já abriu a transação (mesmo padrão de gravarPreventivas
// em preventivaService.go, chamada de dentro de maquinario.go).

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// anexoNovo é a forma comum de um anexo já upado pro R2 (pelo controller,
// antes de chamar o service -- mesmo padrão de MaquinarioInsert.FotoChave),
// pronto pra virar uma linha de solicitacao_anexo. Existe só pra
// gravarImpactosEAnexos não precisar de 6 parâmetros posicionais (foto +
// vídeo, cada um com chave/mime/tamanho).
type anexoNovo struct {
	Tipo         repository.TipoAnexo
	Chave        string
	MimeType     string
	TamanhoBytes int64
}

// marcadorValido converte um item de Impactos pro ENUM do banco. Não dá pra
// usar `binding:"oneof"` no payload (ver o comentário em
// internal/model/solicitacao.go -- o rótulo "Afeta Produção" tem espaço, e o
// oneof do validator separa alternativas por espaço), então quem valida é
// aqui.
func marcadorValido(item string) (repository.MarcadorImpacto, error) {

	if repository.MarcadorImpacto(item) != repository.MarcadorImpactoAfetaProduo {
		return "", fmt.Errorf("%w: marcador de impacto desconhecido: %s", helper.ErrValidacao, item)
	}
	return repository.MarcadorImpactoAfetaProduo, nil
}

// tipoOsDaSolicitacao converte tipo_solicitacao pra tipo_os -- dois tipos
// Postgres diferentes com os mesmos rótulos textuais (nunca 'terceiros' aqui:
// só o Técnico promove uma OS a isso, depois de aberta). Erro aqui é bug de
// código, não entrada do cliente -- por isso sem sentinela, cai em 500.
func tipoOsDaSolicitacao(tipo repository.TipoSolicitacao) (repository.TipoOs, error) {

	switch tipo {
	case repository.TipoSolicitacaoMaquinario:
		return repository.TipoOsMaquinario, nil
	case repository.TipoSolicitacaoReparo:
		return repository.TipoOsReparo, nil
	}
	return "", fmt.Errorf("tipo de solicitação desconhecido: %s", tipo)
}

// resolverSetorSolicitante busca o único setor do escopo de quem abre uma
// solicitação -- mesma leitura que EscopoPerfilService.montarSessao faz para
// SessaoUsuario.SetorId, só que aqui ninguém precisa do nome. A cardinalidade
// "exatamente 1 loja e 1 setor" nasce no cadastro (validarEscopo); os dois
// `if` abaixo são defesa, não onde a regra é definida -- por isso sem
// sentinela: um solicitante sem essa forma é dado quebrado no servidor, não
// erro de quem chamou, e cai em 500 (mesmo critério do erro homônimo em
// montarSessao).
func resolverSetorSolicitante(ctx context.Context, repo *repository.Queries, usuarioId int64) (int64, error) {

	escopos, err := repo.ObterEscopoSessaoPorUsuario(ctx, usuarioId)
	if err != nil {
		return 0, helper.TraduzErroPostgres(err)
	}
	if len(escopos) != 1 || len(escopos[0].SetoresIds) != 1 {
		return 0, fmt.Errorf("usuário %d sem setor de solicitante no escopo", usuarioId)
	}

	return escopos[0].SetoresIds[0], nil
}

// gravarImpactosEAnexos grava os impactos (marcadores já validados) e os
// anexos (já upados pro R2 pelo controller) de uma solicitação recém-criada,
// na mesma transação.
func gravarImpactosEAnexos(ctx context.Context, repo *repository.Queries, solicitacaoId int64, marcadores []repository.MarcadorImpacto, anexos []anexoNovo) error {

	for _, marcador := range marcadores {
		if err := repo.CriarImpactoSolicitacao(ctx, repository.CriarImpactoSolicitacaoParams{
			SolicitacaoID: solicitacaoId,
			Marcador:      marcador,
		}); err != nil {
			return helper.TraduzErroPostgres(err)
		}
	}

	for _, anexo := range anexos {
		if err := repo.CriarAnexoSolicitacao(ctx, repository.CriarAnexoSolicitacaoParams{
			SolicitacaoID: solicitacaoId,
			Tipo:          anexo.Tipo,
			Chave:         anexo.Chave,
			MimeType:      anexo.MimeType,
			TamanhoBytes:  anexo.TamanhoBytes,
		}); err != nil {
			return helper.TraduzErroPostgres(err)
		}
	}

	return nil
}

// concluirSolicitacao é a cauda comum às duas criações humanas e à rejeição:
// relê a solicitação inteira (RETURNING não enxerga os JOINs da resposta,
// mesmo motivo de CadastrarMaquina relendo por ObterMaquinaPorID), comita e
// monta. escopo_usuario_id vai NULL na releitura -- quem chega até aqui já
// passou pela checagem de escopo de quem gravou/alterou a linha (o próprio
// ator da escrita), reler com o mesmo filtro seria redundante.
func concluirSolicitacao(ctx context.Context, tx pgx.Tx, repo *repository.Queries, tenantId, solicitacaoId int64) (model.SolicitacaoOS, error) {

	obtida, err := repo.ObterSolicitacaoPorID(ctx, repository.ObterSolicitacaoPorIDParams{ID: solicitacaoId, TenantID: tenantId})
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	impactos, err := repo.ObterImpactosDaSolicitacao(ctx, solicitacaoId)
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	anexos, err := repo.ObterAnexosDaSolicitacao(ctx, solicitacaoId)
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return model.SolicitacaoOS{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return model.MontarSolicitacao(obtida, impactos, anexos), nil
}

// montarSolicitacoesEmLote busca impactos e anexos de uma página inteira
// numa ida só ao banco cada (ObterImpactosDasSolicitacoes/
// ObterAnexosDasSolicitacoes), mesmo padrão de ObterEscoposSessaoPorUsuarios
// em ListarUsuarios -- evita N+1, uma consulta por solicitação da página.
func montarSolicitacoesEmLote(ctx context.Context, repo *repository.Queries, linhas []repository.ObterSolicitacaoPorIDRow) ([]model.SolicitacaoOS, error) {

	ids := make([]int64, len(linhas))
	for i, l := range linhas {
		ids[i] = l.ID
	}

	impactos, err := repo.ObterImpactosDasSolicitacoes(ctx, ids)
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}
	impactosPorSolicitacao := make(map[int64][]repository.MarcadorImpacto, len(linhas))
	for _, i := range impactos {
		impactosPorSolicitacao[i.SolicitacaoID] = append(impactosPorSolicitacao[i.SolicitacaoID], i.Marcador)
	}

	anexos, err := repo.ObterAnexosDasSolicitacoes(ctx, ids)
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}
	anexosPorSolicitacao := make(map[int64][]repository.SolicitacaoAnexo, len(linhas))
	for _, a := range anexos {
		anexosPorSolicitacao[a.SolicitacaoID] = append(anexosPorSolicitacao[a.SolicitacaoID], a)
	}

	dados := make([]model.SolicitacaoOS, 0, len(linhas))
	for _, l := range linhas {
		dados = append(dados, model.MontarSolicitacao(l, impactosPorSolicitacao[l.ID], anexosPorSolicitacao[l.ID]))
	}

	return dados, nil
}
