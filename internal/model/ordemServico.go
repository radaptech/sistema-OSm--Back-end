package model

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// PausaOrdemServico espelha PausaOrdemServico do front. O histórico inteiro
// viaja em OrdemServico.Pausas (três pausas seguidas são três linhas -- ver
// os_pausa em docs/modelagem, seção 3.3), e a que está em aberto viaja
// repetida em PausaAtual, porque é ela que o Gestor vê em destaque no card da
// aba "OS em Andamento" e a tela não deveria ter que procurá-la na lista.
//
// RetomadaEm sem `omitempty`: o front tipa `retomadaEm: string | null` (sem
// `?`), então o campo é sempre emitido -- `null` é o que diz "esta pausa ainda
// está aberta".
type PausaOrdemServico struct {
	Id             int64          `json:"id"`
	Motivo         string         `json:"motivo"`
	PausadaEm      *config.DataBr `json:"pausadaEm"`
	RetomadaEm     *config.DataBr `json:"retomadaEm"`
	StatusAnterior string         `json:"statusAnterior"`
}

// EncerramentoOrdemServico espelha EncerramentoOrdemServico do front: o que o
// Técnico escreveu ao fechar a OS. TipoDefeito NÃO mora aqui, e sim solto em
// OrdemServico -- é assim que o front tipa, porque a classificação
// Predial/Corretiva aparece no cabeçalho do card, não no bloco de texto.
type EncerramentoOrdemServico struct {
	DefeitoConstatado string `json:"defeitoConstatado"`
	CausaRaiz         string `json:"causaRaiz"`
	Solucao           string `json:"solucao"`
	EncerradoPorNome  string `json:"encerradoPorNome"`
}

// CustoOrdemServico espelha CustoOrdemServico do front.
//
// CustoHoraTecnico é ponteiro e sem `omitempty` (front: `number | null`, sem
// `?`): ck_custo_por_tipo proíbe hora técnica fora de 'maquinario' -- em
// 'terceiros' quem trabalhou foi a empresa e em 'reparo' o serviço não cobra
// hora. `null` ali é a regra de negócio aparecendo, não dado faltando.
//
// Os três campos de nota fiscal são o espelho disso: só existem em
// 'terceiros', e por isso levam `omitempty`.
//
// CustoTotal é derivado, somado em MontarOrdemServico -- ver a nota lá.
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

// OrdemServico espelha OrdemServico do front (ordemServico.ts) e serve os DOIS
// caminhos que devolvem uma OS: GET /ordens-servico (completa) e
// POST /solicitacoes/:id/abrir-os (recém-criada). Uma struct só, e não duas,
// porque o front também tipa um só: tudo que a OS recém-aberta não tem ainda
// -- técnico denormalizado, encerramento, custo, horas, pausas -- é opcional
// no contrato, e uma OS que acabou de nascer legitimamente não tem nada disso.
//
// ⚠️ Todo campo de data é *config.DataBr, nunca o valor: o MarshalJSON do
// DataBr tem receiver ponteiro, então num campo não-ponteiro o encoding/json
// ignora o método e serializa `{}` -- a data some da resposta sem erro nenhum.
//
// Finalizada é resolvida pelo servidor (encerramento MAIS custo lançado), não
// por cada tela: é a regra que separa a aba "OS Finalizadas" do Gestor da fila
// "Custos Pendentes" do Administrador, e as duas leem esta mesma rota.
//
// AfetaProducao é o que liga o relógio de máquina parada: com ela falsa,
// HorasParada vem nula e as telas exibem "Não se aplica" em vez de um número
// -- que é diferente de zero, e é por isso que o campo é ponteiro.
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

// dataBrOuNil e floatOuNil traduzem os tipos nullable do pgx para os ponteiros
// que o contrato pede. O `.Valid` é o que separa "não tem" de "é zero" -- e
// aqui os dois casos existem de verdade: uma OS sem custo lançado não é uma OS
// de custo zero, e uma máquina que não parou não parou por zero horas.
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
	// f é cópia (parâmetro por valor), então o endereço não escapa para a
	// próxima linha do laço de quem chama.
	return &f.Float64
}

// MontarOrdemServico é a única tradução de linha de OS para resposta -- mesmo
// papel de MontarSolicitacao/MontarPreventiva.
//
// As pausas entram já filtradas para ESTA OS (o service agrupa o resultado de
// ObterPausasDasOrdensServico por ordem_servico_id), mesmo desenho de
// impactos/anexos em MontarSolicitacao: ficam fora da row principal porque um
// JOIN 1:N duplicaria a OS por pausa, e a conversão mora aqui para esta
// continuar sendo a única tradutora.
//
// Os três blocos opcionais nascem só quando a linha correspondente existe:
// encerramento quando o Técnico encerrou, custo quando alguém lançou. O sinal
// é uma coluna NOT NULL da tabela filha vindo não-nula -- com LEFT JOIN é
// exatamente isso que distingue "linha existe" de "linha não existe".
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

	// defeito_constatado é NOT NULL em os_encerramento: não-nulo aqui só pode
	// significar que o LEFT JOIN achou a linha.
	if os.DefeitoConstatado != nil {
		ordem.Encerramento = &EncerramentoOrdemServico{
			DefeitoConstatado: *os.DefeitoConstatado,
			CausaRaiz:         textoOuVazio(os.CausaRaiz),
			Solucao:           textoOuVazio(os.Solucao),
			EncerradoPorNome:  textoOuVazio(os.EncerradoPorNome),
		}
	}

	// Mesmo critério, com custo_manutencao (NOT NULL em os_custo).
	// custo_hora_tecnico não serviria: ele é nulo por regra em reparo e
	// terceiros, mesmo com a linha existindo.
	if os.CustoManutencao.Valid {
		horaTecnico := floatOuNil(os.CustoHoraTecnico)
		// CustoTotal é somado aqui, e não no SELECT: uma expressão a mais na
		// query seria mais uma chance de cair na armadilha do numeric (ver a
		// nota no sqlc.yaml), e a conta é uma soma. Hora técnica ausente conta
		// como zero -- é o mesmo COALESCE de vw_os_finalizada.
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
		// A pausa em aberto é a de retomada_em nulo -- uq_pausa_aberta garante
		// no máximo uma por OS, então a última a casar é a única.
		if !p.RetomadaEm.Valid {
			atual := pausa
			ordem.PausaAtual = &atual
		}
	}

	return ordem
}

// textoOuVazio existe para os campos que o front tipa como string obrigatória
// mas que chegam como ponteiro pelo LEFT JOIN. Quando o bloco pai existe (é a
// única situação em que são lidos), a coluna é NOT NULL e nunca cai no zero --
// mas devolver "" é melhor do que estourar um nil pointer numa resposta HTTP
// por causa de uma linha inconsistente.
func textoOuVazio(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// MontarOrdemServicoDaAbertura traduz a OS recém-criada + a solicitação que a
// originou (já em mãos do service, que a releu para checar o status antes do
// INSERT) pro corpo de resposta de POST /solicitacoes/:id/abrir-os.
// Urgencia/TecnicoId/AfetaProducao vêm à parte porque não estão na row nem da
// OS (RETURNING mínimo, sem JOIN) nem da solicitação -- são o que o Gestor
// decidiu e o que o service computou de solicitacao_impacto, respectivamente.
//
// Devolve a mesma struct de MontarOrdemServico, só que com menos preenchido:
// tecnicoNome/tecnicoArea/empresaTerceirizada* ficam de fora porque a query da
// abertura não faz esses JOINs, e encerramento/custo/horas/pausas porque uma
// OS que acabou de nascer não tem nada disso. Os oito são opcionais no
// contrato do front. Finalizada nasce sempre `false`.
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
