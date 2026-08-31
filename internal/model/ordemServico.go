package model

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// RetomadaEm é sempre emitido (sem omitempty): `null` é o que diz "pausa ainda aberta".
type PausaOrdemServico struct {
	Id             int64          `json:"id"`
	Motivo         string         `json:"motivo"`
	PausadaEm      *config.DataBr `json:"pausadaEm"`
	RetomadaEm     *config.DataBr `json:"retomadaEm"`
	StatusAnterior string         `json:"statusAnterior"`
}

// TipoDefeito fica solto em OrdemServico, não aqui: é como o front tipa.
type EncerramentoOrdemServico struct {
	DefeitoConstatado string `json:"defeitoConstatado"`
	CausaRaiz         string `json:"causaRaiz"`
	Solucao           string `json:"solucao"`
	EncerradoPorNome  string `json:"encerradoPorNome"`
}

// CustoHoraTecnico sempre emitido: `null` é ck_custo_por_tipo aparecendo (só
// 'maquinario' cobra hora técnica), não dado faltando.
type CustoOrdemServico struct {
	CustoHoraTecnico         *float64       `json:"custoHoraTecnico"`
	CustoManutencao          float64        `json:"custoManutencao"`
	CustoTotal               float64        `json:"custoTotal"`
	NumeroNotaFiscal         *string        `json:"numeroNotaFiscal,omitempty"`
	SerieNotaFiscal          *string        `json:"serieNotaFiscal,omitempty"`
	DescricaoServicoTerceiro *string        `json:"descricaoServicoTerceiro,omitempty"`
	LancadoPorNome           string         `json:"lancadoPorNome"`
	LancadoEm                *config.DataBr `json:"lancadoEm"`
}

// Serve GET /ordens-servico e POST /solicitacoes/:id/abrir-os -- uma struct só
// porque o front tipa uma só, e o que a OS recém-aberta não tem é opcional.
// ⚠️ Data é *config.DataBr, nunca o valor: MarshalJSON tem receiver ponteiro e
// num campo não-ponteiro o encoding/json serializa `{}`, calado.
type OrdemServico struct {
	Id                      int64                     `json:"id"`
	SolicitacaoId           int64                     `json:"solicitacaoId"`
	Tipo                    string                    `json:"tipo"`
	MaquinaId               *int64                    `json:"maquinaId"`
	MaquinaNome             *string                   `json:"maquinaNome"`
	MaquinaCodigo           *string                   `json:"maquinaCodigo"`
	ItemDescricao           *string                   `json:"itemDescricao"`
	Descricao               string                    `json:"descricao"`
	TipoDefeito             *string                   `json:"tipoDefeito,omitempty"`
	SetorId                 int64                     `json:"setorId"`
	SetorNome               string                    `json:"setorNome"`
	LojaId                  int64                     `json:"lojaId"`
	LojaNome                string                    `json:"lojaNome"`
	SolicitanteNome         *string                   `json:"solicitanteNome"`
	Urgencia                string                    `json:"urgencia,omitempty"`
	TecnicoId               int64                     `json:"tecnicoId,omitempty"`
	TecnicoNome             *string                   `json:"tecnicoNome,omitempty"`
	TecnicoArea             *string                   `json:"tecnicoArea,omitempty"`
	EmpresaTerceirizadaId   *int64                    `json:"empresaTerceirizadaId,omitempty"`
	EmpresaTerceirizadaNome *string                   `json:"empresaTerceirizadaNome,omitempty"`
	StatusExecucao          string                    `json:"statusExecucao"`
	Finalizada              bool                      `json:"finalizada"`
	AfetaProducao           bool                      `json:"afetaProducao"`
	DataSolicitacao         *config.DataBr            `json:"dataSolicitacao"`
	DataAbertura            *config.DataBr            `json:"dataAbertura"`
	DataInicio              *config.DataBr            `json:"dataInicio,omitempty"`
	DataFim                 *config.DataBr            `json:"dataFim,omitempty"`
	HorasTrabalhadas        *float64                  `json:"horasTrabalhadas,omitempty"`
	HorasParada             *float64                  `json:"horasParada,omitempty"`
	PausaAtual              *PausaOrdemServico        `json:"pausaAtual,omitempty"`
	Pausas                  []PausaOrdemServico       `json:"pausas,omitempty"`
	Encerramento            *EncerramentoOrdemServico `json:"encerramento,omitempty"`
	Custo                   *CustoOrdemServico        `json:"custo,omitempty"`
}

// O `.Valid` separa "não tem" de "é zero" -- OS sem custo lançado não é OS de
// custo zero, e máquina que não parou não parou por zero horas.
func dataBrOuNil(ts pgtype.Timestamptz) *config.DataBr {
	if !ts.Valid {
		return nil
	}
	return config.NewDataBrPtr(ts.Time)
}

func floatOuNil(f pgtype.Float8) *float64 {
	if !f.Valid {
		return nil
	}
	// f é cópia, então o endereço não escapa para a próxima volta do laço de quem chama.
	return &f.Float64
}

// Única tradução de linha de OS para resposta. As pausas chegam já filtradas
// para esta OS -- vêm de query própria porque um JOIN 1:N duplicaria a OS.
func MontarOrdemServico(os repository.ListarOrdensServicoRow, pausas []repository.OsPausa) OrdemServico {

	ordem := OrdemServico{
		Id:                      os.ID,
		SolicitacaoId:           os.SolicitacaoID,
		Tipo:                    string(os.Tipo),
		MaquinaId:               os.MaquinaID,
		MaquinaNome:             os.MaquinaNome,
		MaquinaCodigo:           os.MaquinaCodigo,
		ItemDescricao:           os.ItemDescricao,
		Descricao:               os.Descricao,
		SetorId:                 os.SetorID,
		SetorNome:               os.SetorNome,
		LojaId:                  os.LojaID,
		LojaNome:                os.LojaNome,
		SolicitanteNome:         os.SolicitanteNome,
		Urgencia:                string(os.Urgencia),
		TecnicoId:               os.TecnicoID,
		TecnicoNome:             &os.TecnicoNome,
		TecnicoArea:             os.TecnicoArea,
		EmpresaTerceirizadaId:   os.EmpresaTerceirizadaID,
		EmpresaTerceirizadaNome: os.EmpresaTerceirizadaNome,
		StatusExecucao:          string(os.Status),
		Finalizada:              os.Finalizada,
		AfetaProducao:           os.AfetaProducao,
		DataSolicitacao:         dataBrOuNil(os.DataSolicitacao),
		DataAbertura:            dataBrOuNil(os.AbertaEm),
		DataInicio:              dataBrOuNil(os.IniciadaEm),
		DataFim:                 dataBrOuNil(os.DataFim),
		HorasTrabalhadas:        floatOuNil(os.HorasTrabalhadas),
		HorasParada:             floatOuNil(os.HorasParada),
	}

	if os.TipoDefeito != nil {
		tipoDefeito := string(*os.TipoDefeito)
		ordem.TipoDefeito = &tipoDefeito
	}

	// Coluna NOT NULL na tabela filha: não-nula aqui só pode ser o LEFT JOIN tendo achado a linha.
	if os.DefeitoConstatado != nil {
		ordem.Encerramento = &EncerramentoOrdemServico{
			DefeitoConstatado: *os.DefeitoConstatado,
			CausaRaiz:         textoOuVazio(os.CausaRaiz),
			Solucao:           textoOuVazio(os.Solucao),
			EncerradoPorNome:  textoOuVazio(os.EncerradoPorNome),
		}
	}

	// custo_hora_tecnico não serviria de sinal: é nulo por regra em reparo e terceiros.
	if os.CustoManutencao.Valid {
		horaTecnico := floatOuNil(os.CustoHoraTecnico)
		// Somado aqui e não no SELECT: mais uma expressão na query seria mais uma
		// chance de cair na armadilha do numeric (ver sqlc.yaml).
		total := os.CustoManutencao.Float64
		if horaTecnico != nil {
			total += *horaTecnico
		}
		ordem.Custo = &CustoOrdemServico{
			CustoHoraTecnico:         horaTecnico,
			CustoManutencao:          os.CustoManutencao.Float64,
			CustoTotal:               total,
			NumeroNotaFiscal:         os.NumeroNotaFiscal,
			SerieNotaFiscal:          os.SerieNotaFiscal,
			DescricaoServicoTerceiro: os.DescricaoServicoTerceiro,
			LancadoPorNome:           textoOuVazio(os.LancadoPorNome),
			LancadoEm:                dataBrOuNil(os.LancadoEm),
		}
	}

	for _, p := range pausas {
		pausa := PausaOrdemServico{
			Id:             p.ID,
			Motivo:         p.Motivo,
			PausadaEm:      dataBrOuNil(p.PausadaEm),
			RetomadaEm:     dataBrOuNil(p.RetomadaEm),
			StatusAnterior: string(p.StatusAnterior),
		}
		ordem.Pausas = append(ordem.Pausas, pausa)
		// uq_pausa_aberta garante no máximo uma por OS, então a última a casar é a única.
		if !p.RetomadaEm.Valid {
			atual := pausa
			ordem.PausaAtual = &atual
		}
	}

	return ordem
}

// Campos que o front tipa como string obrigatória mas chegam ponteiro pelo LEFT
// JOIN. Nunca deveria cair no zero; melhor que estourar nil numa resposta HTTP.
func textoOuVazio(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Mesma struct de MontarOrdemServico, com menos preenchido: a query da abertura
// não faz os JOINs de técnico, e OS recém-nascida não tem encerramento/custo/pausa.
// Urgencia/TecnicoId/AfetaProducao vêm à parte por não estarem em nenhuma das duas rows.
func MontarOrdemServicoDaAbertura(os repository.CriarOrdemServicoDeSolicitacaoRow, s repository.ObterSolicitacaoPorIDRow, urgencia string, tecnicoId int64, afetaProducao bool) OrdemServico {

	return OrdemServico{
		Id:              os.ID,
		SolicitacaoId:   s.ID,
		Tipo:            string(s.Tipo),
		MaquinaId:       s.MaquinaID,
		MaquinaNome:     s.MaquinaNome,
		MaquinaCodigo:   s.MaquinaCodigo,
		ItemDescricao:   s.ItemDescricao,
		Descricao:       s.Descricao,
		SetorId:         s.SetorID,
		SetorNome:       s.SetorNome,
		LojaId:          s.LojaID,
		LojaNome:        s.LojaNome,
		SolicitanteNome: s.SolicitanteNome,
		Urgencia:        urgencia,
		TecnicoId:       tecnicoId,
		StatusExecucao:  string(repository.StatusOsAberta),
		Finalizada:      false,
		AfetaProducao:   afetaProducao,
		DataSolicitacao: config.NewDataBrPtr(s.CriadoEm.Time),
		DataAbertura:    config.NewDataBrPtr(os.AbertaEm.Time),
	}
}
