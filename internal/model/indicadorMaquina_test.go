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
	if len(ind.PorMes) != 2 || ind.PorMes[0].Mes != "01/2026" || ind.PorMes[0].CustoTotal != 150 || ind.PorMes[1].Mes != "02/2026" || ind.PorMes[1].CustoTotal != 0 {
		t.Errorf("porMes = %+v, esperado [{01/2026 150} {02/2026 0}]", ind.PorMes)
	}

	// Cada mês traz os cards e a rosca só das OS dele: janeiro tem as duas
	// primeiras (aberturas a 48h uma da outra), fevereiro só a terceira.
	jan, fev := ind.PorMes[0], ind.PorMes[1]
	if jan.HorasParadaTotal != 15 || jan.MttrHoras != 4 || jan.MtbfHoras != 48 {
		t.Errorf("janeiro = %+v, esperado parada 15, mttr 4 (a nula fora), mtbf 48", jan.ResumoIndicadores)
	}
	if jan.PorTipoDefeito[0] != (IndicadorPorDefeito{"Predial", 5}) || jan.PorTipoDefeito[1] != (IndicadorPorDefeito{"Corretiva", 10}) {
		t.Errorf("janeiro porTipoDefeito = %+v", jan.PorTipoDefeito)
	}
	if fev.HorasParadaTotal != 2.5 || fev.MttrHoras != 1 || fev.MtbfHoras != 0 {
		t.Errorf("fevereiro = %+v, esperado parada 2.5, mttr 1, mtbf 0 (uma OS só)", fev.ResumoIndicadores)
	}
}

// O gráfico é "Custo Mensal (últimos 12 meses)": mês mais antigo cai fora, e o
// que sobra continua em ordem crescente.
func TestIndicadoresCortaEmDozeMeses(t *testing.T) {

	// Fora de ordem cronológica de propósito: a query ordena por aberta_em, e
	// uma OS aberta antes pode ser encerrada depois -- o mês NÃO vem ordenado.
	meses := []string{"2026-03", "2025-05", "2026-01", "2025-12", "2025-07", "2026-05", "2025-09", "2026-02", "2025-06", "2026-04", "2025-10", "2025-08", "2025-11"}
	historico := make([]repository.ListarHistoricoOsDaMaquinaRow, 0, len(meses))
	for i, mes := range meses {
		historico = append(historico, osEncerrada(i, "Corretiva", mes, f8(1), f8(1), f8(10)))
	}

	ind := MontarIndicadoresMaquina(1, historico)

	esperado := []string{"06/2025", "07/2025", "08/2025", "09/2025", "10/2025", "11/2025", "12/2025", "01/2026", "02/2026", "03/2026", "04/2026", "05/2026"}
	if len(ind.PorMes) != len(esperado) {
		t.Fatalf("porMes = %d meses, esperado %d: %+v", len(ind.PorMes), len(esperado), ind.PorMes)
	}
	for i, e := range esperado {
		if ind.PorMes[i].Mes != e {
			t.Errorf("porMes[%d] = %q, esperado %q", i, ind.PorMes[i].Mes, e)
		}
	}
	// 05/2025 saiu do gráfico, mas continua no total: o card é do histórico
	// inteiro, o gráfico é da janela.
	if ind.CustoTotal != 130 {
		t.Errorf("custoTotal = %v, esperado 130 (os 13 meses, não os 12 do gráfico)", ind.CustoTotal)
	}
}

// osDaLoja monta uma linha do histórico da loja: a de máquina, com as chaves
// do recorte (máquina e setor) que a query por loja acrescenta.
func osDaLoja(maquina, setor int64, dias int, mes string, parada, trabalhadas, custo pgtype.Float8) repository.ListarHistoricoOsDaLojaRow {
	return repository.ListarHistoricoOsDaLojaRow{
		MaquinaID:        maquina,
		SetorID:          setor,
		AbertaEm:         ts(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dias)),
		TipoDefeito:      repository.TipoDefeito("Corretiva"),
		MesEncerramento:  mes,
		HorasParada:      parada,
		HorasTrabalhadas: trabalhadas,
		CustoManutencao:  custo,
	}
}

// O MTBF do grupo mede só entre OS da MESMA máquina: as OS das máquinas 1 e 2
// intercaladas a cada 5 dias não podem virar "uma falha a cada 5 dias" no
// setor -- cada máquina quebra a cada 10.
func TestIndicadoresLojaMtbfPorMaquina(t *testing.T) {

	maquinas := []repository.ListarMaquinasRow{
		{ID: 1, SetorID: 10, SetorNome: "Padaria"},
		{ID: 2, SetorID: 10, SetorNome: "Padaria"},
	}
	ind := MontarIndicadoresLoja(7, maquinas, []repository.ListarHistoricoOsDaLojaRow{
		osDaLoja(1, 10, 0, "2026-01", f8(2), f8(1), f8(100)),
		osDaLoja(2, 10, 5, "2026-01", f8(4), f8(3), f8(50)),
		osDaLoja(1, 10, 10, "2026-01", f8(6), f8(5), f8(10)),
		osDaLoja(2, 10, 15, "2026-01", nulo(), f8(3), nulo()),
	}, nil)

	if ind.MtbfHoras != 240 {
		t.Errorf("mtbfHoras = %v, esperado 240 (10 dias entre OS da mesma máquina)", ind.MtbfHoras)
	}
	if ind.HorasParadaTotal != 12 || ind.CustoTotal != 160 || ind.QuantidadeOs != 4 {
		t.Errorf("total = %+v, esperado parada 12, custo 160, 4 OS", ind)
	}
	if ind.MttrHoras != 3 {
		t.Errorf("mttrHoras = %v, esperado 3 (média das 4 OS)", ind.MttrHoras)
	}
	if len(ind.PorSetor) != 1 || ind.PorSetor[0].QuantidadeMaquinas != 2 || ind.PorSetor[0].MtbfHoras != 240 {
		t.Errorf("porSetor = %+v, esperado um setor com 2 máquinas e MTBF 240", ind.PorSetor)
	}
	if len(ind.PorMaquina) != 2 || ind.PorMaquina[0].CustoTotal != 110 || ind.PorMaquina[1].QuantidadeOs != 2 {
		t.Errorf("porMaquina = %+v", ind.PorMaquina)
	}
}

// Máquina e setor sem OS encerrada ganham card zerado, não somem: a tela
// desenha um card por máquina da lista, e o total tem que bater com eles.
func TestIndicadoresLojaMaquinaSemHistorico(t *testing.T) {

	maquinas := []repository.ListarMaquinasRow{
		{ID: 1, SetorID: 10, SetorNome: "Padaria"},
		{ID: 3, SetorID: 20, SetorNome: "Açougue"},
	}
	ind := MontarIndicadoresLoja(7, maquinas, []repository.ListarHistoricoOsDaLojaRow{
		osDaLoja(1, 10, 0, "2026-01", f8(2), f8(1), f8(100)),
	}, nil)

	if len(ind.PorSetor) != 2 || ind.PorSetor[1].SetorNome != "Açougue" || ind.PorSetor[1].CustoTotal != 0 {
		t.Errorf("porSetor = %+v, esperado o Açougue zerado", ind.PorSetor)
	}
	if ind.PorSetor[1].PorMes == nil || len(ind.PorSetor[1].PorTipoDefeito) != 2 {
		t.Error("setor zerado sem listas montadas; o front faz .map e quebra")
	}
	if len(ind.PorMaquina) != 2 || ind.PorMaquina[1].QuantidadeOs != 0 {
		t.Errorf("porMaquina = %+v, esperado a máquina 3 zerada", ind.PorMaquina)
	}
}

// osPreventiva é uma OS encerrada aberta pelo job de preventiva.
func osPreventiva(dias int, mes string, trabalhadas, custo pgtype.Float8) repository.ListarHistoricoOsDaMaquinaRow {
	linha := osEncerrada(dias, "Predial", mes, nulo(), trabalhadas, custo)
	linha.Origem = repository.OrigemSolicitacaoPreventiva
	return linha
}

// MTBF e MTTR medem FALHA e conserto; a revisão programada não é nenhum dos
// dois. Contada, ela encolheria o MTBF a cada revisão (a máquina "falharia" mais
// só porque recebe manutenção em dia) e puxaria o MTTR para baixo.
//
// Duas quebras com 60 dias entre elas e uma revisão no meio: o MTBF correto é 60
// dias (1440h), não os 30 de quando a revisão entra como falha.
func TestIndicadoresPreventivaNaoEntraNoMtbfNemNoMttr(t *testing.T) {

	historico := []repository.ListarHistoricoOsDaMaquinaRow{
		osEncerrada(0, "Corretiva", "2026-01", f8(4), f8(4), f8(100)),
		osPreventiva(20, "2026-01", f8(1), f8(30)),
		osEncerrada(60, "Corretiva", "2026-03", f8(6), f8(6), f8(200)),
	}

	ind := MontarIndicadoresMaquina(1, historico)

	if ind.MtbfHoras != 1440 {
		t.Errorf("mtbfHoras = %v, esperado 1440 (60 dias entre as duas quebras, a revisão fora)", ind.MtbfHoras)
	}
	// (4 + 6) / 2 = 5; com a revisão de 1h entrando seria 3.67.
	if ind.MttrHoras != 5 {
		t.Errorf("mttrHoras = %v, esperado 5 (a revisão fora do divisor)", ind.MttrHoras)
	}
	// O dinheiro gasto na revisão é real, mas sai à parte do maquinário, como o dos
	// reparos: o custo das máquinas fica sendo o das quebras.
	if ind.CustoTotal != 300 || ind.QuantidadeOs != 2 {
		t.Errorf("maquinário = R$ %v em %d OS, esperado R$ 300 em 2 (a revisão fora)", ind.CustoTotal, ind.QuantidadeOs)
	}
	if ind.CustoPreventivas != 30 || ind.QuantidadePreventivas != 1 {
		t.Errorf("preventivas = R$ %v em %d, esperado R$ 30 em 1", ind.CustoPreventivas, ind.QuantidadePreventivas)
	}
}

// Só preventiva no histórico: sem falha não há MTBF nem MTTR, e o custo aparece na
// faixa das preventivas, com o maquinário zerado.
func TestIndicadoresSoPreventivaNaoTemMtbfNemMttr(t *testing.T) {

	ind := MontarIndicadoresMaquina(1, []repository.ListarHistoricoOsDaMaquinaRow{
		osPreventiva(0, "2026-01", f8(2), f8(40)),
		osPreventiva(30, "2026-01", f8(2), f8(40)),
	})

	if ind.MtbfHoras != 0 || ind.MttrHoras != 0 {
		t.Errorf("mtbf = %v, mttr = %v, esperado 0 e 0 (nenhuma falha)", ind.MtbfHoras, ind.MttrHoras)
	}
	if ind.CustoTotal != 0 || ind.QuantidadeOs != 0 {
		t.Errorf("maquinário = R$ %v em %d OS, esperado zerado", ind.CustoTotal, ind.QuantidadeOs)
	}
	if ind.CustoPreventivas != 80 || ind.QuantidadePreventivas != 2 {
		t.Errorf("preventivas = R$ %v em %d, esperado R$ 80 em 2", ind.CustoPreventivas, ind.QuantidadePreventivas)
	}
}

// O recorte por mês usa o mesmo cálculo: preventiva fora do MTBF/MTTR do mês
// também, senão o card do mês clicado discordaria do total.
func TestIndicadoresPreventivaForaDoMesTambem(t *testing.T) {

	ind := MontarIndicadoresMaquina(1, []repository.ListarHistoricoOsDaMaquinaRow{
		osEncerrada(0, "Corretiva", "2026-01", f8(4), f8(4), f8(100)),
		osPreventiva(10, "2026-01", f8(1), f8(30)),
		osEncerrada(20, "Corretiva", "2026-01", f8(6), f8(6), f8(200)),
	})

	jan := ind.PorMes[0]
	if jan.MtbfHoras != 480 || jan.MttrHoras != 5 {
		t.Errorf("janeiro: mtbf %v, mttr %v, esperado 480 (20 dias) e 5", jan.MtbfHoras, jan.MttrHoras)
	}
}

// reparoDaLoja é um pequeno reparo encerrado (sem máquina, só o setor).
func reparoDaLoja(setor int64, nomeSetor string, dias int, mes string, custo pgtype.Float8) repository.ListarHistoricoReparosDaLojaRow {
	return repository.ListarHistoricoReparosDaLojaRow{
		SetorID:         setor,
		SetorNome:       nomeSetor,
		AbertaEm:        ts(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dias)),
		MesEncerramento: mes,
		CustoManutencao: custo,
	}
}

// O custo dos pequenos reparos aparece SEPARADO do custo das máquinas: nunca somado
// ao custoTotal e fora de MTBF, MTTR e horas. Reparo não tem máquina, então também
// nunca chega num card de máquina.
func TestIndicadoresLojaReparosSaemSeparados(t *testing.T) {

	maquinas := []repository.ListarMaquinasRow{{ID: 1, SetorID: 10, SetorNome: "Padaria"}}
	ind := MontarIndicadoresLoja(7, maquinas, []repository.ListarHistoricoOsDaLojaRow{
		osDaLoja(1, 10, 0, "2026-01", f8(2), f8(1), f8(100)),
		osDaLoja(1, 10, 10, "2026-01", f8(4), f8(3), f8(50)),
	}, []repository.ListarHistoricoReparosDaLojaRow{
		reparoDaLoja(10, "Padaria", 3, "2026-01", f8(30)),
		reparoDaLoja(10, "Padaria", 4, "2026-02", f8(20)),
	})

	// Máquinas: tudo como se os reparos não existissem.
	if ind.CustoTotal != 150 || ind.QuantidadeOs != 2 || ind.HorasParadaTotal != 6 || ind.MttrHoras != 2 || ind.MtbfHoras != 240 {
		t.Errorf("maquinário contaminado pelos reparos: %+v", ind.ResumoIndicadores)
	}
	// Reparos: em campos próprios.
	if ind.QuantidadeReparos != 2 || ind.CustoReparos != 50 {
		t.Errorf("reparos = %d em R$ %v, esperado 2 em R$ 50", ind.QuantidadeReparos, ind.CustoReparos)
	}
	// O setor leva os dois, separados; a máquina, nenhum reparo.
	if ind.PorSetor[0].CustoTotal != 150 || ind.PorSetor[0].CustoReparos != 50 || ind.PorSetor[0].QuantidadeReparos != 2 {
		t.Errorf("setor = %+v", ind.PorSetor[0].ResumoIndicadores)
	}
	if ind.PorMaquina[0].CustoReparos != 0 || ind.PorMaquina[0].QuantidadeReparos != 0 || ind.PorMaquina[0].CustoTotal != 150 {
		t.Errorf("máquina recebeu reparo: %+v", ind.PorMaquina[0].ResumoIndicadores)
	}
	// Mês: janeiro tem máquinas e um reparo; fevereiro SÓ reparo (o mês existe por causa dele).
	if len(ind.PorMes) != 2 {
		t.Fatalf("porMes = %+v, esperado janeiro e fevereiro", ind.PorMes)
	}
	jan, fev := ind.PorMes[0], ind.PorMes[1]
	if jan.CustoTotal != 150 || jan.CustoReparos != 30 || jan.QuantidadeReparos != 1 {
		t.Errorf("janeiro = %+v", jan.ResumoIndicadores)
	}
	if fev.Mes != "02/2026" || fev.CustoTotal != 0 || fev.QuantidadeOs != 0 || fev.CustoReparos != 20 {
		t.Errorf("fevereiro = %+v, esperado só reparo (20), maquinário zerado", fev.ResumoIndicadores)
	}
}

// A preventiva sai à parte como o reparo, mas tem máquina: chega no card da máquina
// também, e fica fora das horas e da rosca (revisão programada não é parada por falha).
func TestIndicadoresLojaPreventivasSaemSeparadas(t *testing.T) {

	maquinas := []repository.ListarMaquinasRow{{ID: 1, SetorID: 10, SetorNome: "Padaria"}}
	preventiva := osDaLoja(1, 10, 5, "2026-02", f8(3), f8(2), f8(40))
	preventiva.Origem = repository.OrigemSolicitacaoPreventiva
	ind := MontarIndicadoresLoja(7, maquinas, []repository.ListarHistoricoOsDaLojaRow{
		osDaLoja(1, 10, 0, "2026-01", f8(2), f8(1), f8(100)),
		preventiva,
	}, []repository.ListarHistoricoReparosDaLojaRow{
		reparoDaLoja(10, "Padaria", 3, "2026-01", f8(30)),
	})

	for nome, r := range map[string]ResumoIndicadores{
		"loja": ind.ResumoIndicadores, "setor": ind.PorSetor[0].ResumoIndicadores, "máquina": ind.PorMaquina[0].ResumoIndicadores,
	} {
		if r.CustoTotal != 100 || r.QuantidadeOs != 1 || r.HorasParadaTotal != 2 || r.PorTipoDefeito[1].HorasParada != 2 {
			t.Errorf("%s: maquinário contaminado pela preventiva: %+v", nome, r)
		}
		if r.CustoPreventivas != 40 || r.QuantidadePreventivas != 1 {
			t.Errorf("%s: preventivas = R$ %v em %d, esperado R$ 40 em 1", nome, r.CustoPreventivas, r.QuantidadePreventivas)
		}
	}
	if ind.CustoReparos != 30 || ind.PorMaquina[0].CustoReparos != 0 {
		t.Errorf("reparos misturados: loja %v, máquina %v", ind.CustoReparos, ind.PorMaquina[0].CustoReparos)
	}
	// Fevereiro só teve a preventiva: o mês existe, com o maquinário zerado.
	fev := ind.PorMes[1]
	if fev.Mes != "02/2026" || fev.CustoTotal != 0 || fev.CustoPreventivas != 40 {
		t.Errorf("fevereiro = %+v, esperado só a preventiva (40)", fev.ResumoIndicadores)
	}
}

// Setor que só teve reparo (sem máquina cadastrada) não aparece em `maquinas`, mas o
// custo dele está no total da loja: precisa de um card que o explique.
func TestIndicadoresLojaSetorSoComReparo(t *testing.T) {

	maquinas := []repository.ListarMaquinasRow{{ID: 1, SetorID: 10, SetorNome: "Padaria"}}
	ind := MontarIndicadoresLoja(7, maquinas, nil, []repository.ListarHistoricoReparosDaLojaRow{
		reparoDaLoja(20, "Limpeza", 0, "2026-01", f8(80)),
	})

	if ind.CustoReparos != 80 {
		t.Errorf("custoReparos da loja = %v, esperado 80", ind.CustoReparos)
	}
	if len(ind.PorSetor) != 2 || ind.PorSetor[1].SetorNome != "Limpeza" || ind.PorSetor[1].CustoReparos != 80 || ind.PorSetor[1].QuantidadeMaquinas != 0 {
		t.Errorf("porSetor = %+v, esperado o setor Limpeza com os 80 e 0 máquinas", ind.PorSetor)
	}
}
