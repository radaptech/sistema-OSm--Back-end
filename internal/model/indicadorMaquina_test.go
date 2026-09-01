package model

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// f8/ts vêm de ordemServico_test.go, mesmo pacote.
func nulo() pgtype.Float8 { return pgtype.Float8{} }

// osEncerrada monta uma linha do histórico. `dias` é quantos dias atrás a OS
// foi aberta -- o que importa para o MTBF é o espaçamento entre elas.
func osEncerrada(dias int, defeito, mes string, parada, trabalhadas, custo pgtype.Float8) repository.ListarHistoricoOsDaMaquinaRow {
	return repository.ListarHistoricoOsDaMaquinaRow{
		AbertaEm:         ts(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dias)),
		TipoDefeito:      repository.TipoDefeito(defeito),
		MesEncerramento:  mes,
		HorasParada:      parada,
		HorasTrabalhadas: trabalhadas,
		CustoManutencao:  custo,
	}
}

// Máquina recém-cadastrada: zeros, mas com as duas listas montadas -- o front
// tipa IndicadoresMaquina sem opcionais e faz .map nas duas.
func TestIndicadoresSemHistorico(t *testing.T) {

	ind := MontarIndicadoresMaquina(9, nil)

	if ind.MaquinaId != 9 || ind.HorasParadaTotal != 0 || ind.MttrHoras != 0 || ind.MtbfHoras != 0 || ind.CustoTotal != 0 {
		t.Errorf("esperado tudo zerado, veio %+v", ind)
	}
	if len(ind.PorTipoDefeito) != 2 {
		t.Errorf("porTipoDefeito = %d itens, esperado os 2 tipos mesmo zerados", len(ind.PorTipoDefeito))
	}
	if ind.PorMes == nil {
		t.Error("porMes nil; o front faz .map e quebra")
	}
}

// Uma OS só não tem intervalo entre falhas -- MTBF precisa de duas aberturas.
func TestIndicadoresMtbfPrecisaDeDuasOs(t *testing.T) {

	ind := MontarIndicadoresMaquina(1, []repository.ListarHistoricoOsDaMaquinaRow{
		osEncerrada(0, "Corretiva", "2026-01", f8(3), f8(2), f8(100)),
	})

	if ind.MtbfHoras != 0 {
		t.Errorf("mtbfHoras = %v com uma OS só, esperado 0", ind.MtbfHoras)
	}
	if ind.MttrHoras != 2 {
		t.Errorf("mttrHoras = %v, esperado 2 (a única OS)", ind.MttrHoras)
	}
}

// O caso completo: três OS, dois tipos de defeito, uma sem horas trabalhadas e
// uma sem custo lançado.
func TestIndicadoresAgregaHistorico(t *testing.T) {

	// Aberturas em 01/01, 03/01 e 06/01 -> intervalos de 48h e 72h, média 60.
	historico := []repository.ListarHistoricoOsDaMaquinaRow{
		osEncerrada(0, "Corretiva", "2026-01", f8(10), f8(4), f8(100)),
		osEncerrada(2, "Predial", "2026-01", f8(5), nulo(), f8(50)),
		osEncerrada(5, "Corretiva", "2026-02", f8(2.5), f8(1), nulo()),
	}

	ind := MontarIndicadoresMaquina(1, historico)

	if ind.HorasParadaTotal != 17.5 {
		t.Errorf("horasParadaTotal = %v, esperado 17.5", ind.HorasParadaTotal)
	}
	// Média de 4 e 1: a OS sem horas trabalhadas fica FORA do divisor. Entrando
	// como zero daria 1.67 -- um conserto instantâneo que não aconteceu.
	if ind.MttrHoras != 2.5 {
		t.Errorf("mttrHoras = %v, esperado 2.5 (média de 4 e 1, a nula fora)", ind.MttrHoras)
	}
	if ind.MtbfHoras != 60 {
		t.Errorf("mtbfHoras = %v, esperado 60 (média de 48h e 72h)", ind.MtbfHoras)
	}
	// Custo nulo é "ainda não lançado" e soma zero, sem sumir com a OS.
	if ind.CustoTotal != 150 {
		t.Errorf("custoTotal = %v, esperado 150", ind.CustoTotal)
	}

	// A ordem é a do const tiposDefeito do front: é ela que casa a fatia com a
	// cor da rosca.
	esperado := []IndicadorPorDefeito{{"Predial", 5}, {"Corretiva", 12.5}}
	for i, e := range esperado {
		if ind.PorTipoDefeito[i] != e {
			t.Errorf("porTipoDefeito[%d] = %+v, esperado %+v", i, ind.PorTipoDefeito[i], e)
		}
	}

	// MM/YYYY no contrato, ordenado do mais antigo para o mais novo.
	if len(ind.PorMes) != 2 || ind.PorMes[0] != (IndicadorMensal{"01/2026", 150}) || ind.PorMes[1] != (IndicadorMensal{"02/2026", 0}) {
		t.Errorf("porMes = %+v, esperado [{01/2026 150} {02/2026 0}]", ind.PorMes)
	}
}

// O gráfico é "Custo Mensal (últimos 6 meses)": mês mais antigo cai fora, e o
// que sobra continua em ordem crescente.
func TestIndicadoresCortaEmSeisMeses(t *testing.T) {

	// Fora de ordem cronológica de propósito: a query ordena por aberta_em, e
	// uma OS aberta antes pode ser encerrada depois -- o mês NÃO vem ordenado.
	meses := []string{"2026-03", "2025-11", "2026-01", "2025-12", "2026-05", "2026-02", "2026-04"}
	historico := make([]repository.ListarHistoricoOsDaMaquinaRow, 0, len(meses))
	for i, mes := range meses {
		historico = append(historico, osEncerrada(i, "Corretiva", mes, f8(1), f8(1), f8(10)))
	}

	ind := MontarIndicadoresMaquina(1, historico)

	esperado := []string{"12/2025", "01/2026", "02/2026", "03/2026", "04/2026", "05/2026"}
	if len(ind.PorMes) != len(esperado) {
		t.Fatalf("porMes = %d meses, esperado %d: %+v", len(ind.PorMes), len(esperado), ind.PorMes)
	}
	for i, e := range esperado {
		if ind.PorMes[i].Mes != e {
			t.Errorf("porMes[%d] = %q, esperado %q", i, ind.PorMes[i].Mes, e)
		}
	}
	// 11/2025 saiu do gráfico, mas continua no total: o card é do histórico
	// inteiro, o gráfico é da janela.
	if ind.CustoTotal != 70 {
		t.Errorf("custoTotal = %v, esperado 70 (os 7 meses, não os 6 do gráfico)", ind.CustoTotal)
	}
}
