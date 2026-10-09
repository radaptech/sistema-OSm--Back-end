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

	porMes := make(map[string][]repository.ListarHistoricoOsDaMaquinaRow)
	for _, os := range historico {
		porMes[os.MesEncerramento] = append(porMes[os.MesEncerramento], os)
	}

	indicadores := IndicadoresMaquina{
		MaquinaId:         maquinaId,
		ResumoIndicadores: resumir(historico),
		PorMes:            make([]IndicadorMensal, 0, mesesNoGrafico),
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
	for _, mes := range meses {
		indicadores.PorMes = append(indicadores.PorMes, IndicadorMensal{
			Mes:               mes[5:7] + "/" + mes[0:4],
			ResumoIndicadores: resumir(porMes[mes]),
		})
	}

	return indicadores
}

func resumir(historico []repository.ListarHistoricoOsDaMaquinaRow) ResumoIndicadores {

	horasPorDefeito := make(map[string]float64, len(tiposDefeito))

	var horasParadaTotal, custoTotal, somaTrabalhadas float64
	var comHorasTrabalhadas int

	for _, os := range historico {
		parada := zeroSeNulo(os.HorasParada)
		horasParadaTotal += parada
		horasPorDefeito[string(os.TipoDefeito)] += parada

		// MTTR é a média do reparo, e OS sem horas trabalhadas não teve reparo
		// medido -- entra como zero ela puxaria a média para baixo inventando
		// um conserto instantâneo.
		if os.HorasTrabalhadas.Valid {
			somaTrabalhadas += os.HorasTrabalhadas.Float64
			comHorasTrabalhadas++
		}

		custoTotal += zeroSeNulo(os.CustoHoraTecnico) + zeroSeNulo(os.CustoManutencao)
	}

	resumo := ResumoIndicadores{
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
func mtbf(historico []repository.ListarHistoricoOsDaMaquinaRow) float64 {

	if len(historico) < 2 {
		return 0
	}

	var soma float64
	for i := 1; i < len(historico); i++ {
		soma += historico[i].AbertaEm.Time.Sub(historico[i-1].AbertaEm.Time).Hours()
	}

	return arredondar(soma / float64(len(historico)-1))
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
