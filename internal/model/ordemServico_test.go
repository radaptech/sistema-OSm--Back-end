package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
func f8(v float64) pgtype.Float8        { return pgtype.Float8{Float64: v, Valid: true} }
func ptr[T any](v T) *T                 { return &v }

// osAberta é o estado mínimo: OS que o Gestor acabou de aprovar, sem nada do
// ciclo de vida ainda. Todo campo opcional tem que sumir da resposta.
func osAberta() repository.ListarOrdensServicoRow {
	agora := time.Date(2026, 8, 30, 14, 30, 0, 0, time.UTC)
	return repository.ListarOrdensServicoRow{
		ID: 7, SolicitacaoID: 3, Tipo: repository.TipoOsMaquinario,
		TecnicoID: 9, TecnicoNome: "Eder", TecnicoArea: ptr("Elétrica"),
		Status: repository.StatusOsAberta, Urgencia: repository.NivelUrgenciaAlta,
		Descricao: "Forno não aquece", AfetaProducao: true,
		MaquinaID: ptr(int64(2)), MaquinaNome: ptr("Forno"), MaquinaCodigo: ptr("PAT-1"),
		SetorID: 4, SetorNome: "Padaria", LojaID: 1, LojaNome: "Loja A",
		SolicitanteNome: ptr("Bruno"),
		DataSolicitacao: ts(agora.Add(-8 * time.Hour)), AbertaEm: ts(agora),
	}
}

// A armadilha documentada no CLAUDE.md: MarshalJSON do DataBr tem receiver
// ponteiro, então num campo não-ponteiro o encoding/json ignora o método e
// serializa `{}` -- a data some da resposta sem erro nenhum.
func TestOrdemServicoSerializaDatasNoFormatoBr(t *testing.T) {

	corpo := map[string]any{}
	bruto, err := json.Marshal(MontarOrdemServico(osAberta(), nil))
	if err != nil {
		t.Fatalf("erro ao serializar: %v", err)
	}
	if err := json.Unmarshal(bruto, &corpo); err != nil {
		t.Fatalf("erro ao desserializar: %v", err)
	}

	if corpo["dataAbertura"] != "30/08/2026 14:30:00" {
		t.Errorf("dataAbertura = %v, esperado dd/mm/yyyy HH:MM:SS", corpo["dataAbertura"])
	}
	if corpo["dataSolicitacao"] != "30/08/2026 06:30:00" {
		t.Errorf("dataSolicitacao = %v", corpo["dataSolicitacao"])
	}
}

// Os campos que a OS recém-aberta não tem são `?` no tipo do front: emitir
// `null` neles faria o `if (ordem.custo)` da tela passar por um objeto vazio.
func TestOrdemServicoAbertaOmiteOCicloDeVida(t *testing.T) {

	corpo := map[string]any{}
	bruto, _ := json.Marshal(MontarOrdemServico(osAberta(), nil))
	if err := json.Unmarshal(bruto, &corpo); err != nil {
		t.Fatalf("erro ao desserializar: %v", err)
	}

	for _, campo := range []string{
		"tipoDefeito", "dataInicio", "dataFim", "horasTrabalhadas", "horasParada",
		"pausaAtual", "pausas", "encerramento", "custo",
		"empresaTerceirizadaId", "empresaTerceirizadaNome",
	} {
		if _, presente := corpo[campo]; presente {
			t.Errorf("%q não devia ser emitido numa OS recém-aberta (veio %v)", campo, corpo[campo])
		}
	}

	// Estes o front tipa SEM `?`: têm que aparecer mesmo valendo null/false.
	for _, campo := range []string{
		"id", "solicitacaoId", "tipo", "maquinaId", "maquinaNome", "maquinaCodigo",
		"itemDescricao", "descricao", "setorId", "setorNome", "lojaId", "lojaNome",
		"solicitanteNome", "statusExecucao", "finalizada", "afetaProducao",
		"dataSolicitacao", "dataAbertura",
	} {
		if _, presente := corpo[campo]; !presente {
			t.Errorf("%q é obrigatório no contrato e não foi emitido", campo)
		}
	}
	if corpo["finalizada"] != false {
		t.Errorf("finalizada = %v, esperado false", corpo["finalizada"])
	}
	if corpo["itemDescricao"] != nil {
		t.Errorf("itemDescricao de OS de maquinário devia ser null, veio %v", corpo["itemDescricao"])
	}
}

func TestOrdemServicoCustoTotal(t *testing.T) {

	casos := []struct {
		nome        string
		horaTecnico pgtype.Float8
		manutencao  pgtype.Float8
		temCusto    bool
		total       float64
		horaNula    bool
	}{
		// Maquinário: os dois lançados, total é a soma.
		{"hora técnica mais manutenção", f8(150.50), f8(320.25), true, 470.75, false},
		// Reparo/terceiros: ck_custo_por_tipo proíbe hora técnica, mas a linha
		// de custo existe. Total é só a manutenção -- hora ausente vale zero,
		// mesmo COALESCE de vw_os_finalizada.
		{"sem hora técnica", pgtype.Float8{}, f8(18.90), true, 18.90, true},
		// Sem linha em os_custo: o bloco inteiro não existe.
		{"custo não lançado", pgtype.Float8{}, pgtype.Float8{}, false, 0, false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			linha := osAberta()
			linha.CustoHoraTecnico, linha.CustoManutencao = caso.horaTecnico, caso.manutencao
			linha.LancadoPorNome, linha.LancadoEm = ptr("Ana"), ts(time.Now())

			ordem := MontarOrdemServico(linha, nil)

			if !caso.temCusto {
				if ordem.Custo != nil {
					t.Fatalf("custo devia ser nil, veio %+v", ordem.Custo)
				}
				return
			}
			if ordem.Custo == nil {
				t.Fatal("custo devia existir")
			}
			if ordem.Custo.CustoTotal != caso.total {
				t.Errorf("custoTotal = %v, esperado %v", ordem.Custo.CustoTotal, caso.total)
			}
			if caso.horaNula && ordem.Custo.CustoHoraTecnico != nil {
				t.Errorf("custoHoraTecnico = %v, esperado nil", *ordem.Custo.CustoHoraTecnico)
			}
		})
	}
}

// horasParada nula não é o mesmo que zero: com afetaProducao falsa a máquina
// seguiu operando, e a tela exibe "Não se aplica". Um float64 cru aqui
// devolveria 0 e a tela mostraria "0h de parada", que é outra afirmação.
func TestOrdemServicoHorasNulasNaoViramZero(t *testing.T) {

	linha := osAberta()
	linha.AfetaProducao = false
	linha.HorasTrabalhadas = f8(4)
	// HorasParada fica no zero value: Valid falso.

	ordem := MontarOrdemServico(linha, nil)
	if ordem.HorasParada != nil {
		t.Errorf("horasParada = %v, esperado nil", *ordem.HorasParada)
	}
	if ordem.HorasTrabalhadas == nil || *ordem.HorasTrabalhadas != 4 {
		t.Errorf("horasTrabalhadas = %v, esperado 4", ordem.HorasTrabalhadas)
	}

	corpo := map[string]any{}
	bruto, _ := json.Marshal(ordem)
	json.Unmarshal(bruto, &corpo)
	if _, presente := corpo["horasParada"]; presente {
		t.Error("horasParada não devia ser emitida quando não se aplica")
	}
}

// pausaAtual é a de retomada_em nula, e vem repetida em pausas -- não é um
// substituto da lista. floatOuNil/dataBrOuNil devolvem endereço de cópia, então
// esta é também a trava contra o clássico "todas as pausas apontam para a
// última do laço".
func TestOrdemServicoPausas(t *testing.T) {

	agora := time.Now()
	pausas := []repository.OsPausa{
		{ID: 1, Motivo: "aguardando peça", StatusAnterior: repository.StatusOsEmAndamento,
			PausadaEm: ts(agora.Add(-4 * time.Hour)), RetomadaEm: ts(agora.Add(-3 * time.Hour))},
		{ID: 2, Motivo: "peça em falta no estoque", StatusAnterior: repository.StatusOsEmAndamento,
			PausadaEm: ts(agora.Add(-30 * time.Minute))},
	}

	ordem := MontarOrdemServico(osAberta(), pausas)

	if len(ordem.Pausas) != 2 {
		t.Fatalf("pausas = %d, esperado 2 (o histórico inteiro)", len(ordem.Pausas))
	}
	if ordem.Pausas[0].RetomadaEm == nil {
		t.Error("a primeira pausa foi retomada e devia trazer retomadaEm")
	}
	if ordem.Pausas[1].RetomadaEm != nil {
		t.Error("a segunda pausa está aberta e retomadaEm devia ser null")
	}
	if ordem.PausaAtual == nil {
		t.Fatal("pausaAtual devia existir: há uma pausa em aberto")
	}
	if ordem.PausaAtual.Id != 2 || ordem.PausaAtual.Motivo != "peça em falta no estoque" {
		t.Errorf("pausaAtual = %+v, esperado a de id 2", ordem.PausaAtual)
	}

	// Sem pausa em aberto, pausaAtual some mas o histórico fica.
	semAberta := MontarOrdemServico(osAberta(), pausas[:1])
	if semAberta.PausaAtual != nil {
		t.Errorf("pausaAtual = %+v, esperado nil", semAberta.PausaAtual)
	}
	if len(semAberta.Pausas) != 1 {
		t.Errorf("pausas = %d, esperado 1", len(semAberta.Pausas))
	}

	// retomadaEm é `string | null` no front (sem `?`): sempre emitido.
	corpo := map[string]any{}
	bruto, _ := json.Marshal(ordem)
	json.Unmarshal(bruto, &corpo)
	lista, _ := corpo["pausas"].([]any)
	if len(lista) != 2 {
		t.Fatalf("pausas no JSON = %d", len(lista))
	}
	aberta, _ := lista[1].(map[string]any)
	if _, presente := aberta["retomadaEm"]; !presente {
		t.Error("retomadaEm devia ser emitido como null, não omitido")
	}
}

func TestOrdemServicoEncerrada(t *testing.T) {

	linha := osAberta()
	linha.Status = repository.StatusOsConcluda
	linha.Finalizada = true
	linha.TipoDefeito = ptr(repository.TipoDefeitoCorretiva)
	linha.DataFim = ts(time.Now())
	linha.DefeitoConstatado = ptr("Resistência queimada")
	linha.CausaRaiz = ptr("Fim de vida útil")
	linha.Solucao = ptr("Troca da resistência")
	linha.EncerradoPorNome = ptr("Eder")

	ordem := MontarOrdemServico(linha, nil)

	if ordem.Encerramento == nil {
		t.Fatal("encerramento devia existir")
	}
	if ordem.Encerramento.DefeitoConstatado != "Resistência queimada" || ordem.Encerramento.EncerradoPorNome != "Eder" {
		t.Errorf("encerramento = %+v", ordem.Encerramento)
	}
	// tipoDefeito é solto na OS, não dentro do bloco de encerramento -- é
	// assim que o front tipa (aparece no cabeçalho do card).
	if ordem.TipoDefeito == nil || *ordem.TipoDefeito != "Corretiva" {
		t.Errorf("tipoDefeito = %v, esperado Corretiva", ordem.TipoDefeito)
	}
	// Encerrada mas sem custo lançado: é a fila de Custos Pendentes.
	if ordem.Custo != nil {
		t.Errorf("custo = %+v, esperado nil", ordem.Custo)
	}
}
