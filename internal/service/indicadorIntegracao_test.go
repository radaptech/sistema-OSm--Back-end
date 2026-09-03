package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// TestObterIndicadoresDaMaquina cobre GET /indicadores/maquinas/:id contra
// Postgres de verdade: a query (quem entra no histórico e quem fica fora), o
// escopo virando 404 e o mês em America/Sao_Paulo.
//
// A matemática em si (MTTR, MTBF, corte de 6 meses) é testada sem banco em
// internal/model/indicadorMaquina_test.go -- aqui o que se prova é que os
// números que chegam ao model são os certos.
func TestObterIndicadoresDaMaquina(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svcSolicitacao := NewRepoSolicitacao(pool)
	svc := NewRepoOrdemServico(pool)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("erro em %q: %v", sql, err)
		}
	}

	var tenantID, lojaA, lojaB, setorA, setorB int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('ind', 'Empresa Ind') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	for _, l := range []struct {
		nome string
		dest *int64
	}{{"Loja A", &lojaA}, {"Loja B", &lojaB}} {
		if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, $2) RETURNING id`, tenantID, l.nome).Scan(l.dest); err != nil {
			t.Fatalf("erro ao criar loja: %v", err)
		}
	}
	for _, s := range []struct {
		nome string
		loja int64
		dest *int64
	}{{"Padaria", lojaA, &setorA}, {"Hortifruti", lojaB, &setorB}} {
		if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, $3) RETURNING id`, tenantID, s.loja, s.nome).Scan(s.dest); err != nil {
			t.Fatalf("erro ao criar setor: %v", err)
		}
	}

	maquina := func(nome string, setor int64) model.Maquinario {
		t.Helper()
		m, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
			SetorID: setor, Criticidade: "Alta", NumeroPatrimonio: "PAT-" + nome, Nome: nome,
			Preventivas: []model.PreventivaPayload{{
				Descricao: "Revisão", IntervaloDias: 30,
				ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
			}},
		})
		if err != nil {
			t.Fatalf("erro ao criar máquina %s: %v", nome, err)
		}
		return m
	}
	forno := maquina("Forno", setorA)
	prensa := maquina("Prensa", setorA)
	nova := maquina("Nova", setorA)
	balanca := maquina("Balanca", setorB)

	area, senha := "Elétrica", "senha-forte-123"
	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) model.Usuario {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, senha
		u, err := svcUsuario.CadastrarUsuario(ctx, p, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
		return u
	}

	admin := cadastrar("Ana", "ana@ind.com", "administrador", model.NovoUsuarioPayload{})
	// Gestor só da Loja A: é ele que não pode ver os indicadores da Balança.
	gestor := cadastrar("Dora", "dora@ind.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, AcessoTotalSetores: true,
	})
	tecnico := cadastrar("Eder", "eder@ind.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, Area: &area,
	})
	solA := cadastrar("Bruno", "bruno@ind.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
	})
	solB := cadastrar("Caio", "caio@ind.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaB}, SetoresIds: []int64{setorB},
	})

	// A OS vai pelo caminho de verdade (solicitação -> AbrirOS); só o ciclo de
	// vida entra na mão, porque ele ainda não tem service (fase 2).
	abrir := func(solicitante model.Usuario, maquinaID int64, descricao string) model.OrdemServico {
		t.Helper()
		sol, err := svcSolicitacao.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoMaquinarioPayload{
			MaquinaId: maquinaID, Descricao: descricao, Impactos: []string{"Afeta Produção"},
			FotoChave: "tenant/1/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 1,
		})
		if err != nil {
			t.Fatalf("erro ao criar solicitação %q: %v", descricao, err)
		}
		os, err := svcSolicitacao.AbrirOS(ctx, tenantID, admin.Id, "administrador", sol.Id, model.AberturaOrdemServicoPayload{
			Urgencia: "Alta", TecnicoId: tecnico.Id,
		})
		if err != nil {
			t.Fatalf("erro ao abrir OS %q: %v", descricao, err)
		}
		return os
	}

	// Datas fixas, não relativas: os números abaixo (6h de parada, 768h de
	// MTBF) só são exatos com a linha do tempo pregada.
	encerrar := func(os model.OrdemServico, solicitada, aberta, iniciada, fim, defeito string, custoHora *float64, custoManutencao float64) {
		t.Helper()
		exec(`UPDATE solicitacao_os SET criado_em = $2 WHERE id = $1`, os.SolicitacaoId, solicitada)
		exec(`UPDATE ordem_servico SET status = 'Concluída', aberta_em = $2, iniciada_em = $3 WHERE id = $1`, os.Id, aberta, iniciada)
		exec(`INSERT INTO os_encerramento (tenant_id, ordem_servico_id, tipo, tipo_defeito, encerrado_por_id, data_fim, defeito_constatado, causa_raiz, solucao)
		      VALUES ($1, $2, 'maquinario', $3, $4, $5, 'Constatado', 'Causa', 'Solução')`, tenantID, os.Id, defeito, tecnico.Id, fim)
		exec(`INSERT INTO os_custo (tenant_id, ordem_servico_id, tipo, custo_hora_tecnico, custo_manutencao, lancado_por_id)
		      VALUES ($1, $2, 'maquinario', $3, $4, $5)`, tenantID, os.Id, custoHora, custoManutencao, admin.Id)
	}

	custoHora := 100.00

	// Forno, OS 1: parada 08:00->14:00 = 6h (corre desde a SOLICITAÇÃO),
	// trabalhadas 10:00->14:00 = 4h. Custo 300. Junho.
	encerrar(abrir(solA, forno.Id, "Forno não aquece"),
		"2026-06-10 08:00:00-03", "2026-06-10 09:00:00-03", "2026-06-10 10:00:00-03", "2026-06-10 14:00:00-03",
		"Corretiva", &custoHora, 200.00)

	// Forno, OS 2: parada 4h, trabalhadas 2h, custo 50, sem hora técnica.
	// Julho. As duas aberturas ficam a 32 dias (768h) uma da outra.
	encerrar(abrir(solA, forno.Id, "Forno com ruído"),
		"2026-07-12 08:00:00-03", "2026-07-12 09:00:00-03", "2026-07-12 10:00:00-03", "2026-07-12 12:00:00-03",
		"Predial", nil, 50.00)

	// Forno, OS 3: aberta e nunca encerrada. Não entra em nada -- nem no
	// custo, nem na parada, nem no espaçamento do MTBF.
	abrir(solA, forno.Id, "Forno em análise")

	// Balança: encerrada, mas na Loja B -- é o alvo do teste de escopo.
	encerrar(abrir(solB, balanca.Id, "Balança descalibrada"),
		"2026-06-01 08:00:00-03", "2026-06-01 09:00:00-03", "2026-06-01 10:00:00-03", "2026-06-01 11:00:00-03",
		"Predial", nil, 10.00)

	// Prensa: encerrada às 22h de 31/08 no horário de Brasília, que em UTC já
	// é 01/09. O mês do gráfico é o do Gestor, não o do servidor.
	encerrar(abrir(solA, prensa.Id, "Prensa travada"),
		"2026-08-31 20:00:00-03", "2026-08-31 20:30:00-03", "2026-08-31 21:00:00-03", "2026-08-31 22:00:00-03",
		"Corretiva", nil, 70.00)

	t.Run("agrega o histórico da máquina", func(t *testing.T) {
		ind, err := svc.ObterIndicadoresDaMaquina(ctx, tenantID, forno.Id, gestor.Id, "gestor")
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}

		if ind.MaquinaId != forno.Id {
			t.Errorf("maquinaId = %d, esperado %d", ind.MaquinaId, forno.Id)
		}
		if ind.HorasParadaTotal != 10 {
			t.Errorf("horasParadaTotal = %v, esperado 10 (6h + 4h, desde a solicitação)", ind.HorasParadaTotal)
		}
		if ind.MttrHoras != 3 {
			t.Errorf("mttrHoras = %v, esperado 3 (média de 4h e 2h)", ind.MttrHoras)
		}
		// 32 dias entre as duas aberturas encerradas. A OS ainda aberta é
		// posterior às duas: se ela entrasse na conta, este número mudava.
		if ind.MtbfHoras != 768 {
			t.Errorf("mtbfHoras = %v, esperado 768 (32 dias entre as duas OS encerradas)", ind.MtbfHoras)
		}
		if ind.CustoTotal != 350 {
			t.Errorf("custoTotal = %v, esperado 350 (100+200 e 50)", ind.CustoTotal)
		}

		esperadoDefeito := []model.IndicadorPorDefeito{{TipoDefeito: "Predial", HorasParada: 4}, {TipoDefeito: "Corretiva", HorasParada: 6}}
		if len(ind.PorTipoDefeito) != 2 {
			t.Fatalf("porTipoDefeito = %+v", ind.PorTipoDefeito)
		}
		for i, e := range esperadoDefeito {
			if ind.PorTipoDefeito[i] != e {
				t.Errorf("porTipoDefeito[%d] = %+v, esperado %+v", i, ind.PorTipoDefeito[i], e)
			}
		}

		esperadoMes := []model.IndicadorMensal{{Mes: "06/2026", CustoTotal: 300}, {Mes: "07/2026", CustoTotal: 50}}
		if len(ind.PorMes) != 2 {
			t.Fatalf("porMes = %+v, esperado 2 meses", ind.PorMes)
		}
		for i, e := range esperadoMes {
			if ind.PorMes[i] != e {
				t.Errorf("porMes[%d] = %+v, esperado %+v", i, ind.PorMes[i], e)
			}
		}
	})

	// Uma OS encerrada às 22h de 31/08 (BRT) é 01/09 em UTC. Sem o AT TIME ZONE
	// da query, o custo de agosto aparecia na barra de setembro.
	t.Run("o mês é o de America/Sao_Paulo", func(t *testing.T) {
		ind, err := svc.ObterIndicadoresDaMaquina(ctx, tenantID, prensa.Id, gestor.Id, "gestor")
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if len(ind.PorMes) != 1 || ind.PorMes[0].Mes != "08/2026" {
			t.Errorf("porMes = %+v, esperado [{08/2026 70}]", ind.PorMes)
		}
	})

	// Máquina existe, está no escopo e nunca teve OS encerrada: zeros, não 404.
	// É o estado de toda máquina recém-cadastrada.
	t.Run("máquina sem histórico devolve zeros", func(t *testing.T) {
		ind, err := svc.ObterIndicadoresDaMaquina(ctx, tenantID, nova.Id, gestor.Id, "gestor")
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if ind.HorasParadaTotal != 0 || ind.CustoTotal != 0 || ind.MttrHoras != 0 || ind.MtbfHoras != 0 {
			t.Errorf("esperado zerado, veio %+v", ind)
		}
		if len(ind.PorTipoDefeito) != 2 || ind.PorMes == nil {
			t.Errorf("as duas listas têm que vir montadas: %+v", ind)
		}
	})

	// O escopo é o do TOKEN e vira 404, não lista vazia: a Balança existe e tem
	// histórico, mas é de outra loja. Responder 200 com zeros confirmaria a
	// existência do id, que é o que a enumeração procura.
	t.Run("máquina fora do escopo é 404", func(t *testing.T) {
		_, err := svc.ObterIndicadoresDaMaquina(ctx, tenantID, balanca.Id, gestor.Id, "gestor")
		if !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("erro = %v, esperado ErrNaoEncontrado", err)
		}
	})

	// Administrador não tem escopo, e a ausência dele É o acesso total.
	t.Run("administrador enxerga qualquer máquina do tenant", func(t *testing.T) {
		ind, err := svc.ObterIndicadoresDaMaquina(ctx, tenantID, balanca.Id, admin.Id, "administrador")
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if ind.CustoTotal != 10 {
			t.Errorf("custoTotal = %v, esperado 10", ind.CustoTotal)
		}
	})

	t.Run("máquina inexistente é 404", func(t *testing.T) {
		_, err := svc.ObterIndicadoresDaMaquina(ctx, tenantID, 999999, admin.Id, "administrador")
		if !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("erro = %v, esperado ErrNaoEncontrado", err)
		}
	})
}
