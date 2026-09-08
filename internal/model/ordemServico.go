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
// TemNotaFiscal é a declaração do Técnico no encerramento: houve compra (peça,
// material, fatura da empresa) ou foi só mão de obra? É ela que decide se a
// tela do Administrador mostra os campos de nota -- sem ela, "OS que não gera
// nota" e "nota ainda não preenchida" seriam indistinguíveis. Sem `omitempty`:
// `false` é resposta, não ausência de dado, e some com omitempty.
//
// Itens é a discriminação do custo, uma linha por TAREFA (migration 000012):
// "trocar o rolamento, 180 de peça e 40 de mão de obra" em vez de um "420" que
// ninguém consegue conferir contra nota nenhuma. CustoManutencao e
// CustoHoraTecnico continuam existindo e são a SOMA da coluna correspondente
// dos itens -- ver a nota longa na migration sobre por que o total continua
// guardado.
//
// NotasFiscais substitui o par NumeroNotaFiscal/SerieNotaFiscal, que era
// escalar e não comportava a segunda nota (duas peças compradas em lojas
// diferentes geram dois documentos). Lista vazia com TemNotaFiscal true é
// estado legítimo e frequente: é a fila de conferência do Administrador.
//
// DescricaoServicoTerceiro continua presa a 'terceiros': ela conta o que a
// empresa externa fez, e o que o Técnico fez já mora em
// EncerramentoOrdemServico.Solucao, que existe para todo tipo.
//
// CustoTotal é derivado, somado em MontarOrdemServico -- ver a nota lá.
//
// RevisadoEm é nil enquanto nenhum Administrador conferiu o custo (o Técnico
// lançou no encerramento e ninguém mais mexeu); vira data no primeiro
// POST /ordens-servico/:id/custo. É o que separa a pílula "Pendentes" da
// "Revisadas" em Custos Pendentes -- sem `omitempty` porque o front tipa
// `string | null` e lê o nil.
type CustoOrdemServico struct {
	CustoHoraTecnico         *float64                 `json:"custoHoraTecnico"`
	CustoManutencao          float64                  `json:"custoManutencao"`
	CustoTotal               float64                  `json:"custoTotal"`
	Itens                    []ItemCustoOrdemServico  `json:"itens"`
	TemNotaFiscal            bool                     `json:"temNotaFiscal"`
	NotasFiscais             []NotaFiscalOrdemServico `json:"notasFiscais"`
	DescricaoServicoTerceiro *string                  `json:"descricaoServicoTerceiro,omitempty"`
	LancadoPorNome           string                   `json:"lancadoPorNome"`
	LancadoEm                *config.DataBr           `json:"lancadoEm"`
	RevisadoEm               *config.DataBr           `json:"revisadoEm"`
}

// ItemCustoOrdemServico é uma TAREFA da OS (migration 000012): o que foi feito,
// com o material que consumiu e a mão de obra que cobrou.
//
// CustoHoraTecnico é ponteiro sem `omitempty`, mesmo critério de
// CustoOrdemServico: nulo é a regra de negócio aparecendo (fora de 'maquinario'
// a coluna é proibida) ou a tarefa não ter cobrado hora, e nos dois casos o
// front precisa distinguir de zero.
//
// Descricao é obrigatória (ck_custo_item_descricao): uma linha de dinheiro sem
// nome não é conferível contra nota, que é o ponto inteiro de itemizar.
type ItemCustoOrdemServico struct {
	Id               int64    `json:"id"`
	Descricao        string   `json:"descricao"`
	CustoManutencao  float64  `json:"custoManutencao"`
	CustoHoraTecnico *float64 `json:"custoHoraTecnico"`
}

// NotaFiscalOrdemServico é uma linha de os_nota_fiscal (migration 000012).
//
// Serie é ponteiro com `omitempty` porque a coluna é nullable de verdade --
// nota de consumidor costuma não ter série, e string vazia sairia como `""`
// parecendo uma série de um caractere em branco. Numero nunca é vazio
// (ck_nota_fiscal_numero).
//
// Sem campo de valor de propósito: o valor já está nos itens, e uma segunda
// fonte para a mesma grandeza só cria a pergunta de qual das duas está certa
// no dia em que discordarem.
type NotaFiscalOrdemServico struct {
	Id     int64   `json:"id"`
	Numero string  `json:"numero"`
	Serie  *string `json:"serie,omitempty"`
}

// EncerramentoOrdemServicoPayload é o corpo de POST /ordens-servico/:id/encerrar
// -- espelha EncerramentoOrdemServicoPayload do front, menos OrdemServicoId
// (vem do `:id` da rota, mesmo padrão de AberturaOrdemServicoPayload). Grava
// os_encerramento E os_custo na mesma escrita (docs/modelagem, 2.3 revisão 4)
// -- por isso carrega os dois custos aqui, não só o que o Técnico apurou.
//
// Desde a migration 000012 os custos entram como LISTA, não como dois
// escalares: uma OS pode ter trocado o rolamento E a fita, e somar as duas de
// cabeça antes de digitar era o que o Técnico fazia até aqui. Os agregados
// (custo_manutencao/custo_hora_tecnico) continuam no banco, mas quem os calcula
// é o service, somando o que gravou -- nunca o cliente.
//
// `dive` é obrigatório no binding: sem ele o validator olha a slice e ignora as
// tags de dentro de ItemCustoPayload, e um item com valor negativo ou descrição
// vazia chegaria intacto no CHECK do banco, virando 422 genérico.
type EncerramentoOrdemServicoPayload struct {
	TipoDefeito       string             `json:"tipoDefeito" binding:"required,oneof=Predial Corretiva"`
	DefeitoConstatado string             `json:"defeitoConstatado" binding:"required"`
	CausaRaiz         string             `json:"causaRaiz" binding:"required"`
	Solucao           string             `json:"solucao" binding:"required"`
	Itens             []ItemCustoPayload `json:"itens" binding:"required,min=1,dive"`
	// Declaração do Técnico: houve compra com nota (peça, material, fatura da
	// empresa) ou foi só mão de obra? Decide se a tela do Administrador vai
	// pedir número e série depois. Sem binding de propósito: `required` num
	// bool rejeita `false`, que aqui é a resposta mais comum -- ausente vira
	// false, o mesmo DEFAULT da coluna.
	TemNotaFiscal bool `json:"temNotaFiscal"`
}

// ItemCustoPayload é uma TAREFA enviada pelo Técnico no encerramento e pelo
// Administrador na correção.
//
// `gte=0` em vez de `required` nos dois valores: 0 é valor de negócio legítimo
// (ck_custo_item_valores permite, ex. peça em garantia), e `required` do
// validator rejeita zero em campo numérico -- trataria um conserto de graça
// como campo vazio. Mesma armadilha que os custos escalares já evitavam.
//
// CustoHoraTecnico é ponteiro e opcional: uma tarefa pode ser só material (a
// peça que o Técnico trocou sem cobrar hora). A regra "hora técnica só em
// maquinário" NÃO cabe no binding: ela depende do tipo da OS, que só o service
// conhece -- é ele quem responde 400 nomeando o campo, antes de
// ck_custo_item_hora_tecnico estourar como 422 genérico.
type ItemCustoPayload struct {
	Descricao        string   `json:"descricao" binding:"required"`
	CustoManutencao  float64  `json:"custoManutencao" binding:"gte=0"`
	CustoHoraTecnico *float64 `json:"custoHoraTecnico" binding:"omitempty,gte=0"`
}

// NotaFiscalPayload é uma linha da lista de notas que o Administrador registra.
// Só ele escreve isto: o Técnico apenas DECLARA que houve nota
// (TemNotaFiscal), sem ter o documento em mãos no momento do encerramento.
//
// Serie sem `required` porque nota de consumidor costuma não ter série. Chega
// como string vazia do formulário e o NULLIF de CriarNotaFiscal converte.
type NotaFiscalPayload struct {
	Numero string `json:"numero" binding:"required"`
	Serie  string `json:"serie"`
}

// PausaOrdemServicoPayload é o corpo de POST /ordens-servico/:id/pausar --
// mesmo padrão de RejeicaoSolicitacaoPayload (`{motivo}`). O `binding:"required"`
// aqui só barra ausente/vazio; espaço em branco ("   ") passa pelo bind e cai
// no `campoObrigatorio` do service, que apara antes de validar -- mesma
// divisão de responsabilidade de Rejeitar.
type PausaOrdemServicoPayload struct {
	Motivo string `json:"motivo" binding:"required"`
}

// AcionamentoTerceiroPayload é o corpo de
// POST /ordens-servico/:id/acionar-terceiro -- espelha AcionamentoTerceiroPayload
// do front, menos OrdemServicoId (vem do `:id`). `gt=0` mesmo padrão de
// TecnicoId em AberturaOrdemServicoPayload: id de banco começa em 1, um zero
// aqui já é corpo mal formado, não vale gastar uma ida ao banco (a FK
// composta) só pra descobrir isso.
type AcionamentoTerceiroPayload struct {
	EmpresaTerceirizadaId int64 `json:"empresaTerceirizadaId" binding:"required,gt=0"`
}

// LancamentoCustoManutencaoPayload é o corpo de POST /ordens-servico/:id/custo
// -- espelha LancamentoCustoManutencaoPayload do front, menos OrdemServicoId
// (vem do `:id`, mesmo padrão dos outros payloads de transição). É o
// Administrador CORRIGINDO o que o Técnico já lançou no encerramento
// (CriarCusto), não uma criação -- por isso o service exige a OS `Concluída`
// antes de aceitar isto.
//
// Itens segue a mesma forma e as mesmas regras do encerramento (migration
// 000012): o Administrador recebe a lista do Técnico pré-preenchida na tela e
// devolve a lista inteira, corrigida. É SUBSTITUIÇÃO do conjunto, não patch --
// mesmo padrão do escopo em AtualizarUsuario e das preventivas em
// AtualizarMaquina. `min=1` porque uma OS concluída sem nenhum item de custo
// seria uma OS sem custo, e esse estado não existe depois do encerramento.
//
// NotasFiscais é o que só o Administrador escreve, e é a razão de a lista
// existir: duas peças compradas em lojas diferentes chegam com dois
// documentos. Sem `min` -- lista vazia é legítima e é o estado inicial de toda
// OS que o Técnico declarou como tendo nota e ninguém conferiu ainda.
//
// DescricaoServicoTerceiro segue restrita a 'terceiros' (ck_custo_por_tipo),
// sem binding pelo mesmo motivo de sempre: quem sabe o tipo da OS é o service.
type LancamentoCustoManutencaoPayload struct {
	Itens []ItemCustoPayload `json:"itens" binding:"required,min=1,dive"`
	// Repetido aqui, e não só no encerramento, porque o Administrador PODE
	// corrigir a declaração do Técnico: sem isso, um Técnico que esqueceu de
	// marcar deixaria a OS sem onde lançar a nota que o Administrador tem na
	// mão. Mesmo motivo de não ter binding: `false` é resposta válida.
	TemNotaFiscal            bool                `json:"temNotaFiscal"`
	NotasFiscais             []NotaFiscalPayload `json:"notasFiscais" binding:"omitempty,dive"`
	DescricaoServicoTerceiro *string             `json:"descricaoServicoTerceiro,omitempty"`
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
// Itens e notas entram pelo mesmo caminho das pausas e pelo mesmo motivo:
// desde a migration 000012 as duas são 1:N e um JOIN duplicaria a OS por
// linha. O service busca as três em lote e agrupa por ordem_servico_id antes
// de chamar aqui.
func MontarOrdemServico(
	os repository.ListarOrdensServicoRow,
	pausas []repository.OsPausa,
	itens []repository.ObterItensDeCustoDasOrdensServicoRow,
	notas []repository.ObterNotasFiscaisDasOrdensServicoRow,
) OrdemServico {

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
		// Slices não-nil mesmo vazias: o front tipa `T[]` e faz `.map` direto --
		// `null` quebraria a tela. Mesma regra das listagens do resto da API.
		itensCusto := make([]ItemCustoOrdemServico, 0, len(itens))
		for _, i := range itens {
			itensCusto = append(itensCusto, ItemCustoOrdemServico{
				Id:        i.ID,
				Descricao: i.Descricao,
				// custo_manutencao é NOT NULL em os_custo_item; pgtype.Float8
				// aqui é só consequência do override do sqlc.yaml, `.Valid` é
				// sempre true. O de hora técnica, esse sim, é nulo de verdade:
				// fora de 'maquinario' a coluna é proibida, e dentro dele a
				// tarefa pode não ter cobrado mão de obra.
				CustoManutencao:  i.CustoManutencao.Float64,
				CustoHoraTecnico: floatOuNil(i.CustoHoraTecnico),
			})
		}

		notasFiscais := make([]NotaFiscalOrdemServico, 0, len(notas))
		for _, n := range notas {
			notasFiscais = append(notasFiscais, NotaFiscalOrdemServico{
				Id:     n.ID,
				Numero: n.Numero,
				Serie:  n.Serie,
			})
		}

		ordem.Custo = &CustoOrdemServico{
			CustoHoraTecnico: horaTecnico,
			CustoManutencao:  os.CustoManutencao.Float64,
			CustoTotal:       total,
			Itens:            itensCusto,
			// *bool na linha porque os_custo entra por LEFT JOIN; nil só
			// acontece em OS sem custo, e aí nem chegamos aqui.
			TemNotaFiscal:            os.TemNotaFiscal != nil && *os.TemNotaFiscal,
			NotasFiscais:             notasFiscais,
			DescricaoServicoTerceiro: os.DescricaoServicoTerceiro,
			LancadoPorNome:           textoOuVazio(os.LancadoPorNome),
			LancadoEm:                dataBrOuNil(os.LancadoEm),
			RevisadoEm:               dataBrOuNil(os.CustoRevisadoEm),
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
