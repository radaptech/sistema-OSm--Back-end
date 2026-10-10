package model

import (
	"math"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// IndicadorPorDefeito, IndicadorMensal e IndicadoresMaquina espelham
// tipos/indicadorMaquina.ts -- o corpo de GET /indicadores/maquinas/:id, o
// Painel de Indicadores do Gestor (DashboardGestor).
//
// Todos os números são float64 não-ponteiro, ao contrário de OrdemServico, onde
// horas e custo são *float64: lá `null` significa "não se aplica" e a tela
// escreve isso em texto; aqui o destino é um card, uma rosca e um gráfico de
// barras, e gráfico não desenha ausência. Máquina sem histórico devolve zeros,
// que é o que o painel sabe exibir.
type IndicadorPorDefeito struct {
	TipoDefeito string  `json:"tipoDefeito"`
	HorasParada float64 `json:"horasParada"`
}

// ResumoIndicadores são as grandezas dos cards e da rosca. Vale para o histórico
// inteiro (IndicadoresMaquina) e para cada mês (IndicadorMensal): o painel troca
// um pelo outro quando o Gestor clica numa barra do gráfico mensal. Embutido nos
// dois, sai achatado no JSON -- o corpo do histórico inteiro não mudou de forma.
type ResumoIndicadores struct {
	// QuantidadeOs é quantas OS encerradas entraram na conta -- o "em N OS" dos
	// cards da loja, que vale também para o recorte de um mês.
	QuantidadeOs     int                   `json:"quantidadeOs"`
	HorasParadaTotal float64               `json:"horasParadaTotal"`
	MttrHoras        float64               `json:"mttrHoras"`
	MtbfHoras        float64               `json:"mtbfHoras"`
	CustoTotal       float64               `json:"custoTotal"`
	PorTipoDefeito   []IndicadorPorDefeito `json:"porTipoDefeito"`
}

type IndicadorMensal struct {
	Mes string `json:"mes"`
	ResumoIndicadores
}

type IndicadoresMaquina struct {
	MaquinaId int64 `json:"maquinaId"`
	ResumoIndicadores
	PorMes []IndicadorMensal `json:"porMes"`
}

// IndicadoresLoja é o corpo de GET /indicadores/lojas/:id -- a tela da loja no
// Painel de Indicadores: o total da loja (com o gráfico mensal), um card por
// setor e um por máquina. Tudo restrito ao que quem chama alcança: o Gestor
// com só dois setores da loja recebe a loja como a soma desses dois.
type IndicadoresLoja struct {
	LojaId int64 `json:"lojaId"`
	ResumoIndicadores
	PorMes     []IndicadorMensal        `json:"porMes"`
	PorSetor   []IndicadorSetor         `json:"porSetor"`
	PorMaquina []IndicadorMaquinaResumo `json:"porMaquina"`
}

// IndicadorSetor leva o próprio porMes: clicar num mês do gráfico da loja
// filtra também os cards de setor, então cada setor precisa do recorte mensal.
type IndicadorSetor struct {
	SetorId            int64  `json:"setorId"`
	SetorNome          string `json:"setorNome"`
	QuantidadeMaquinas int    `json:"quantidadeMaquinas"`
	ResumoIndicadores
	PorMes []IndicadorMensal `json:"porMes"`
}

// IndicadorMaquinaResumo é o card da máquina na tela da loja. Também leva o
// porMes, pelo mesmo motivo do setor: o mês escolhido no gráfico da loja vale
// para todos os cards da tela, e um card de máquina mostrando o histórico
// inteiro ao lado de setores filtrados misturaria dois períodos sem avisar.
type IndicadorMaquinaResumo struct {
	MaquinaId int64 `json:"maquinaId"`
	SetorId   int64 `json:"setorId"`
	ResumoIndicadores
	PorMes []IndicadorMensal `json:"porMes"`
}

// linhaHistorico é o que resumir/mtbf leem: as duas queries de histórico (por
// máquina e por loja) devolvem tipos gerados diferentes, e o cálculo não pode
// existir em dois lugares -- o painel da loja tem que bater com a soma das
// máquinas que a tela mostra.
type linhaHistorico struct {
	maquinaId        int64
	abertaEm         pgtype.Timestamptz
	tipoDefeito      string
	mesEncerramento  string
	horasParada      pgtype.Float8
	horasTrabalhadas pgtype.Float8
	custoHoraTecnico pgtype.Float8
	custoManutencao  pgtype.Float8
}

// A ordem é a do const tiposDefeito do front (tipos/ordemServico.ts): é ela que
// casa cada fatia da rosca com a cor de CORES_TIPO_DEFEITO.
var tiposDefeito = []string{"Predial", "Corretiva"}

// O gráfico de barras é rotulado "Custo Mensal (últimos 12 meses)".
const mesesNoGrafico = 12

// MontarIndicadoresMaquina agrega o histórico de OS encerradas da máquina nas
// grandezas do painel, no total e por mês de encerramento. Recebe as linhas JÁ
// ordenadas por aberta_em ascendente (ListarHistoricoOsDaMaquina) -- o MTBF
// depende disso, e o recorte por mês preserva essa ordem.
//
// Histórico vazio devolve os zeros e as duas listas montadas (vazia a de meses,
// completa a de defeitos): o front tipa `IndicadoresMaquina` sem opcionais e
// faz `.map` nas duas, então `null` quebraria a tela de uma máquina nova.
func MontarIndicadoresMaquina(maquinaId int64, historico []repository.ListarHistoricoOsDaMaquinaRow) IndicadoresMaquina {

	linhas := make([]linhaHistorico, 0, len(historico))
	for _, os := range historico {
		linhas = append(linhas, linhaHistorico{
			maquinaId:        maquinaId,
			abertaEm:         os.AbertaEm,
			tipoDefeito:      string(os.TipoDefeito),
			mesEncerramento:  os.MesEncerramento,
			horasParada:      os.HorasParada,
			horasTrabalhadas: os.HorasTrabalhadas,
			custoHoraTecnico: os.CustoHoraTecnico,
			custoManutencao:  os.CustoManutencao,
		})
	}

	return IndicadoresMaquina{
		MaquinaId:         maquinaId,
		ResumoIndicadores: resumir(linhas),
		PorMes:            resumirPorMes(linhas),
	}
}

// MontarIndicadoresLoja recorta o histórico da loja em total, setores e
// máquinas. `maquinas` é a lista que a tela desenha (ListarMaquinas com o
// mesmo escopo): toda máquina e todo setor dela ganham card, inclusive os sem
// OS encerrada, que saem zerados -- o mesmo motivo de MontarIndicadoresMaquina
// devolver zeros e não 404.
//
// O histórico chega ordenado por aberta_em (ListarHistoricoOsDaLoja), e os
// recortes preservam a ordem -- o MTBF de cada um depende dela.
func MontarIndicadoresLoja(lojaId int64, maquinas []repository.ListarMaquinasRow, historico []repository.ListarHistoricoOsDaLojaRow) IndicadoresLoja {

	linhas := make([]linhaHistorico, 0, len(historico))
	porSetor := make(map[int64][]linhaHistorico)
	porMaquina := make(map[int64][]linhaHistorico)
	for _, os := range historico {
		linha := linhaHistorico{
			maquinaId:        os.MaquinaID,
			abertaEm:         os.AbertaEm,
			tipoDefeito:      string(os.TipoDefeito),
			mesEncerramento:  os.MesEncerramento,
			horasParada:      os.HorasParada,
			horasTrabalhadas: os.HorasTrabalhadas,
			custoHoraTecnico: os.CustoHoraTecnico,
			custoManutencao:  os.CustoManutencao,
		}
		linhas = append(linhas, linha)
		porSetor[os.SetorID] = append(porSetor[os.SetorID], linha)
		porMaquina[os.MaquinaID] = append(porMaquina[os.MaquinaID], linha)
	}

	indicadores := IndicadoresLoja{
		LojaId:            lojaId,
		ResumoIndicadores: resumir(linhas),
		PorMes:            resumirPorMes(linhas),
		PorSetor:          make([]IndicadorSetor, 0),
		PorMaquina:        make([]IndicadorMaquinaResumo, 0, len(maquinas)),
	}

	// Setores na ordem em que aparecem em `maquinas` (ListarMaquinas ordena por
	// nome da máquina); a tela reordena pelo nome do setor.
	posicaoSetor := make(map[int64]int)
	for _, m := range maquinas {
		indicadores.PorMaquina = append(indicadores.PorMaquina, IndicadorMaquinaResumo{
			MaquinaId:         m.ID,
			SetorId:           m.SetorID,
			ResumoIndicadores: resumir(porMaquina[m.ID]),
			PorMes:            resumirPorMes(porMaquina[m.ID]),
		})

		i, existe := posicaoSetor[m.SetorID]
		if !existe {
			i = len(indicadores.PorSetor)
			posicaoSetor[m.SetorID] = i
			indicadores.PorSetor = append(indicadores.PorSetor, IndicadorSetor{
				SetorId:           m.SetorID,
				SetorNome:         m.SetorNome,
				ResumoIndicadores: resumir(porSetor[m.SetorID]),
				PorMes:            resumirPorMes(porSetor[m.SetorID]),
			})
		}
		indicadores.PorSetor[i].QuantidadeMaquinas++
	}

	return indicadores
}

// resumirPorMes é o recorte do gráfico de barras: um resumo por mês de
// encerramento, só os meses com OS, os últimos mesesNoGrafico.
func resumirPorMes(linhas []linhaHistorico) []IndicadorMensal {

	porMes := make(map[string][]linhaHistorico)
	for _, os := range linhas {
		porMes[os.mesEncerramento] = append(porMes[os.mesEncerramento], os)
	}

	// A chave é YYYY-MM justamente para ordenar como texto (ver a query); o
	// contrato pede MM/YYYY, montado só agora.
	meses := make([]string, 0, len(porMes))
	for mes := range porMes {
		meses = append(meses, mes)
	}
	slices.Sort(meses)
	if len(meses) > mesesNoGrafico {
		meses = meses[len(meses)-mesesNoGrafico:]
	}

	resultado := make([]IndicadorMensal, 0, mesesNoGrafico)
	for _, mes := range meses {
		resultado = append(resultado, IndicadorMensal{
			Mes:               mes[5:7] + "/" + mes[0:4],
			ResumoIndicadores: resumir(porMes[mes]),
		})
	}
	return resultado
}

func resumir(historico []linhaHistorico) ResumoIndicadores {

	horasPorDefeito := make(map[string]float64, len(tiposDefeito))

	var horasParadaTotal, custoTotal, somaTrabalhadas float64
	var comHorasTrabalhadas int

	for _, os := range historico {
		parada := zeroSeNulo(os.horasParada)
		horasParadaTotal += parada
		horasPorDefeito[os.tipoDefeito] += parada

		// MTTR é a média do reparo, e OS sem horas trabalhadas não teve reparo
		// medido -- entra como zero ela puxaria a média para baixo inventando
		// um conserto instantâneo.
		if os.horasTrabalhadas.Valid {
			somaTrabalhadas += os.horasTrabalhadas.Float64
			comHorasTrabalhadas++
		}

		custoTotal += zeroSeNulo(os.custoHoraTecnico) + zeroSeNulo(os.custoManutencao)
	}

	resumo := ResumoIndicadores{
		QuantidadeOs:     len(historico),
		HorasParadaTotal: arredondar(horasParadaTotal),
		CustoTotal:       arredondar(custoTotal),
		MtbfHoras:        mtbf(historico),
		PorTipoDefeito:   make([]IndicadorPorDefeito, 0, len(tiposDefeito)),
	}

	if comHorasTrabalhadas > 0 {
		resumo.MttrHoras = arredondar(somaTrabalhadas / float64(comHorasTrabalhadas))
	}

	// Os dois tipos saem sempre, mesmo zerados: a rosca tem legenda fixa, e uma
	// fatia que some seria lida como "não existe esse defeito" em vez de "não
	// houve".
	for _, tipo := range tiposDefeito {
		resumo.PorTipoDefeito = append(resumo.PorTipoDefeito, IndicadorPorDefeito{
			TipoDefeito: tipo,
			HorasParada: arredondar(horasPorDefeito[tipo]),
		})
	}

	return resumo
}

// mtbf é a média do intervalo entre aberturas consecutivas, em horas -- o tempo
// que a máquina costuma passar rodando entre uma OS e a próxima. Com menos de
// duas OS não há intervalo nenhum, e zero é o que o card exibe -- o que, no
// recorte de um mês, é o caso comum: só conta o intervalo entre OS do mesmo mês.
//
// O marco é aberta_em e não a data da solicitação, diferente de horas_parada:
// aqui o que se mede é o espaçamento entre as falhas, e ele fica igual
// independentemente do marco escolhido, desde que seja sempre o mesmo.
//
// Num grupo (setor, loja) o intervalo só é medido entre OS da MESMA máquina, e
// a média é sobre todos esses intervalos juntos. Medir entre OS consecutivas
// do grupo daria o espaçamento entre falhas de máquinas diferentes, que
// encolhe a cada máquina cadastrada no setor sem nenhuma delas quebrar mais.
// Para uma máquina só, é exatamente a conta de antes.
func mtbf(historico []linhaHistorico) float64 {

	ultimaAbertura := make(map[int64]pgtype.Timestamptz)
	var soma float64
	var intervalos int
	for _, os := range historico {
		if anterior, ok := ultimaAbertura[os.maquinaId]; ok {
			soma += os.abertaEm.Time.Sub(anterior.Time).Hours()
			intervalos++
		}
		ultimaAbertura[os.maquinaId] = os.abertaEm
	}

	if intervalos == 0 {
		return 0
	}

	return arredondar(soma / float64(intervalos))
}

// Nulo conta como zero, e só neste painel: horas_parada nula é "a máquina não
// parou" e custo nulo é "ainda não lançado" -- nenhum dos dois acrescenta nada
// a um total. É o oposto do que MontarOrdemServico faz com as mesmas colunas,
// onde o nulo vira `null` e a tela escreve "Não se aplica".
func zeroSeNulo(f pgtype.Float8) float64 {
	if !f.Valid {
		return 0
	}
	return f.Float64
}

// Duas casas, mesmo arredondamento do mock que serviu de contrato
// (front src/mocks/regrasMock.ts): o front formata moeda e horas em cima do que
// chega, sem arredondar de novo.
func arredondar(valor float64) float64 {
	return math.Round(valor*100) / 100
}
