package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// As duas escritas de custo (Encerrar, pelo Técnico, e CorrigirCusto, pelo
// Administrador) gravam as mesmas duas listas com as mesmas regras. Elas moram
// aqui, e não em helpers.go, pelo mesmo critério que separa
// solicitacaoOsHelpers.go: só fazem sentido junto do custo da OS.
//
// As duas funções recebem *repository.Queries, e não o Pool, exatamente como
// gravarPreventivas e gravarEscopo: quem chama já abriu a transação, e um
// método de service abriria outra por dentro, quebrando a atomicidade que é o
// ponto do encerramento (os_encerramento, os_custo e as duas listas gravam
// juntos ou nada grava).

// agregadosDeCusto é o que os_custo continua guardando depois da migration
// 000012: a soma de cada coluna de dinheiro da lista de tarefas.
//
// Os dois campos continuam existindo por decisão registrada na migration --
// vw_os_finalizada, ListarOrdensServico e os indicadores leem as colunas, e
// derivá-las na leitura seria reescrever tudo isso para não mudar nada na
// tela. O que a decisão exige em troca é esta função: o servidor NUNCA aceita
// o total vindo do cliente, ele soma o que acabou de gravar.
type agregadosDeCusto struct {
	Manutencao  float64
	HoraTecnico pgtype.Float8
}

// gravarItensDeCusto valida a lista, substitui o conjunto inteiro de itens da
// OS e devolve os agregados para quem chama escrever em os_custo.
//
// Substitui em vez de mesclar, mesmo padrão de gravarPreventivas em
// AtualizarMaquina e do escopo em AtualizarUsuario: a tela manda a lista
// inteira já editada, então merge incremental precisaria de um id por linha
// que o formulário não tem, e um item removido na tela continuaria no banco.
func gravarItensDeCusto(
	ctx context.Context,
	repo *repository.Queries,
	tenantID, ordemServicoID int64,
	tipo repository.TipoOs,
	itens []model.ItemCustoPayload,
) (agregadosDeCusto, error) {

	var agregados agregadosDeCusto

	// O binding (`min=1`) já barra a lista vazia vinda de HTTP. Aqui é a rede
	// para qualquer chamador futuro que não passe pelo controller -- mesmo
	// papel do len(preventivas) == 0 em gravarPreventivas.
	if len(itens) == 0 {
		return agregados, fmt.Errorf("%w: informe ao menos um custo", helper.ErrValidacao)
	}

	var (
		totalManutencao  float64
		totalHoraTecnico float64
	)

	for _, item := range itens {
		descricao := strings.TrimSpace(item.Descricao)
		// ck_custo_item_descricao recusaria no banco, mas como 422 genérico. O
		// texto veio do formulário, então é erro de preenchimento (400).
		// `binding:"required"` não pega: string de espaços passa por ele.
		if descricao == "" {
			return agregados, fmt.Errorf("%w: a descrição do custo não pode ficar em branco", helper.ErrValidacao)
		}

		// ck_custo_item_hora_tecnico espelhado aqui, mesma razão de sempre: sem
		// isto o CHECK do banco ainda barra, mas com mensagem genérica em vez de
		// dizer qual campo está errado.
		if item.CustoHoraTecnico != nil && tipo != repository.TipoOsMaquinario {
			return agregados, fmt.Errorf("%w: custo hora do técnico só existe em OS de maquinário", helper.ErrValidacao)
		}

		var horaTecnico pgtype.Float8
		if item.CustoHoraTecnico != nil {
			horaTecnico = pgtype.Float8{Float64: *item.CustoHoraTecnico, Valid: true}
			totalHoraTecnico += *item.CustoHoraTecnico
		}
		totalManutencao += item.CustoManutencao

		if err := repo.CriarItemDeCusto(ctx, repository.CriarItemDeCustoParams{
			TenantID:         tenantID,
			OrdemServicoID:   ordemServicoID,
			Tipo:             tipo,
			Descricao:        descricao,
			CustoManutencao:  pgtype.Float8{Float64: item.CustoManutencao, Valid: true},
			CustoHoraTecnico: horaTecnico,
		}); err != nil {
			return agregados, helper.TraduzErroPostgres(err)
		}
	}

	agregados.Manutencao = totalManutencao

	// Em maquinário a coluna existe sempre, mesmo somando zero: uma OS sem mão
	// de obra cobrada é conserto de graça, não ausência de informação, e é o
	// mesmo sentido que custo_hora_tecnico já tinha quando era um campo escalar
	// obrigatório. Fora de maquinário fica Invalid (NULL), que é o que
	// ck_custo_por_tipo exige -- ver a nota em CriarCusto.
	//
	// ⚠️ Não existe mais "maquinário exige hora técnica". A regra vinha de o
	// campo ser um escalar obrigatório; com uma linha por TAREFA ela obrigava o
	// Técnico a inventar uma tarefa só para carregar a mão de obra quando a OS
	// tinha duas peças e nenhuma hora a cobrar. Zero diz a mesma coisa sem
	// mentir sobre o que foi feito.
	if tipo == repository.TipoOsMaquinario {
		agregados.HoraTecnico = pgtype.Float8{Float64: totalHoraTecnico, Valid: true}
	}

	return agregados, nil
}

// gravarNotasFiscais substitui o conjunto de documentos da OS. Só o
// Administrador chega aqui: o Técnico apenas DECLARA que houve nota
// (tem_nota_fiscal) no encerramento, sem ter o papel em mãos.
//
// declarouNota é o valor do PAYLOAD, não o que está gravado: esta chamada pode
// ser justamente o Administrador desmarcando a declaração do Técnico.
func gravarNotasFiscais(
	ctx context.Context,
	repo *repository.Queries,
	tenantID, ordemServicoID int64,
	declarouNota bool,
	notas []model.NotaFiscalPayload,
) error {

	// trg_nota_fiscal_declarada espelhado aqui, mesma razão dos CHECKs: sem
	// isto a mensagem que sobe é a EXCEPTION crua do trigger.
	if !declarouNota && len(notas) > 0 {
		return fmt.Errorf("%w: nota fiscal exige que a OS esteja marcada como tendo nota fiscal", helper.ErrValidacao)
	}

	// Desmarcar a declaração apaga as notas na mesma escrita -- é a direção que
	// o trigger não cobre de propósito (ver a nota na migration 000012). O
	// DELETE acima do laço já cuida disso: com declarouNota false a lista vem
	// vazia e nada é reinserido.
	if err := repo.DeletarNotasFiscaisDaOrdemServico(ctx, repository.DeletarNotasFiscaisDaOrdemServicoParams{
		TenantID:       tenantID,
		OrdemServicoID: ordemServicoID,
	}); err != nil {
		return helper.TraduzErroPostgres(err)
	}

	for _, nota := range notas {
		numero := strings.TrimSpace(nota.Numero)
		// ck_nota_fiscal_numero recusaria, como 422. Veio do formulário, é 400.
		if numero == "" {
			return fmt.Errorf("%w: o número da nota fiscal não pode ficar em branco", helper.ErrValidacao)
		}

		if err := repo.CriarNotaFiscal(ctx, repository.CriarNotaFiscalParams{
			TenantID:       tenantID,
			OrdemServicoID: ordemServicoID,
			Numero:         numero,
			// A série vai crua: quem apara e converte "" em NULL é o
			// NULLIF(btrim(...)) da query, para que a unicidade não trate
			// "sem série" e "série vazia" como documentos diferentes.
			Serie: nota.Serie,
		}); err != nil {
			// uq_nota_fiscal_os vira ErrDadoDuplicado (409): duas linhas com o
			// mesmo número e série é o Administrador digitando a mesma nota
			// duas vezes, não erro de servidor.
			return helper.TraduzErroPostgres(err)
		}
	}

	return nil
}
