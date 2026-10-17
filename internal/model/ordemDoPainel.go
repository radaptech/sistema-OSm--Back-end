package model

import (
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// GrupoOrdensDoPainel são os três grupos de custo do Painel de Indicadores da loja: o
// maquinário (o que quebrou), os pequenos reparos (que não têm máquina) e as manutenções
// preventivas (revisão programada) -- os dois últimos SEPARADOS do primeiro.
const (
	GrupoMaquinario  = "maquinario"
	GrupoReparos     = "reparos"
	GrupoPreventivas = "preventivas"
)

// OrdemDoPainel é uma linha da lista por trás de um card de custo do Painel de Indicadores
// (GET /indicadores/lojas/:id/ordens): uma OS encerrada que compõe o número do card. O `id` é o da
// OS -- o front abre o detalhe dela a partir dele.
//
// Serve aos dois grupos. No maquinário, Item é o nome da máquina e Codigo o patrimônio; no
// reparo, Item é o que o Solicitante digitou e Codigo é nulo (não há máquina). Origem diz de onde
// veio a OS (a de preventiva sai com o selo âmbar).
//
// CustoTotal é somado aqui (hora técnica + manutenção, nulo conta como zero), a mesma conta do
// painel: a soma dos itens é o número do card.
type OrdemDoPainel struct {
	Id          int64          `json:"id"`
	Item        string         `json:"item"`
	Codigo      *string        `json:"codigo"`
	MaquinaId   *int64         `json:"maquinaId"`
	Origem      string         `json:"origem"`
	Descricao   string         `json:"descricao"`
	SetorId     int64          `json:"setorId"`
	SetorNome   string         `json:"setorNome"`
	TecnicoNome string         `json:"tecnicoNome"`
	EncerradaEm *config.DataBr `json:"encerradaEm"`
	CustoTotal  float64        `json:"custoTotal"`
}

// MontarReparosEncerrados devolve slice não-nil: `null` quebraria o .map do front.
func MontarReparosEncerrados(linhas []repository.ListarReparosEncerradosDaLojaRow) []OrdemDoPainel {

	ordens := make([]OrdemDoPainel, 0, len(linhas))

	for _, l := range linhas {
		item := ""
		if l.ItemDescricao != nil {
			item = *l.ItemDescricao
		}
		ordens = append(ordens, OrdemDoPainel{
			Id:          l.ID,
			Item:        item,
			Origem:      string(l.Origem),
			Descricao:   l.Descricao,
			SetorId:     l.SetorID,
			SetorNome:   l.SetorNome,
			TecnicoNome: l.TecnicoNome,
			EncerradaEm: dataBrOuNil(l.DataFim),
			CustoTotal:  zeroSeNulo(l.CustoHoraTecnico) + zeroSeNulo(l.CustoManutencao),
		})
	}

	return ordens
}

// MontarOrdensMaquinarioEncerradas, idem para o maquinário e para as preventivas (a mesma query,
// separadas pela origem).
func MontarOrdensMaquinarioEncerradas(linhas []repository.ListarOrdensMaquinarioEncerradasDaLojaRow) []OrdemDoPainel {

	ordens := make([]OrdemDoPainel, 0, len(linhas))

	for _, l := range linhas {
		codigo := l.MaquinaCodigo
		maquinaId := l.MaquinaID
		ordens = append(ordens, OrdemDoPainel{
			Id:          l.ID,
			Item:        l.MaquinaNome,
			Codigo:      &codigo,
			MaquinaId:   &maquinaId,
			Origem:      string(l.Origem),
			Descricao:   l.Descricao,
			SetorId:     l.SetorID,
			SetorNome:   l.SetorNome,
			TecnicoNome: l.TecnicoNome,
			EncerradaEm: dataBrOuNil(l.DataFim),
			CustoTotal:  zeroSeNulo(l.CustoHoraTecnico) + zeroSeNulo(l.CustoManutencao),
		})
	}

	return ordens
}
