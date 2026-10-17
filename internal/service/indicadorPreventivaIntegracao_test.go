package service

import (
	"context"
	"testing"

	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// TestIndicadoresIgnoramPreventivaNoMtbfEMttr tranca, contra o banco, que a OS
// aberta pelo job de preventiva (origem 'preventiva') fica fora do MTBF e do MTTR
// e sai à parte do maquinário, na faixa das preventivas (custo e contagem
// próprios, como os pequenos reparos). Revisão programada não é falha nem
// conserto: contada, o MTBF encolheria a cada revisão e o indicador puniria quem
// faz a manutenção em dia. O teste de model cobre a conta; este cobre que a query
// devolve a origem (`s.origem`) e que as duas pontas -- e as duas listas -- conversam.
func TestIndicadoresIgnoramPreventivaNoMtbfEMttr(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcSolicitacao := NewRepoSolicitacao(pool)
	svcPrev := NewRepoPreventiva(pool)
	svc := NewRepoOrdemServico(pool)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("erro em %q: %v", sql, err)
		}
	}

	var tenantID, lojaID, setorID, maquinaID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('ind-prev', 'Empresa Ind Prev') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	tecnicoPrev := tecnicoParaPreventiva(t, ctx, pool, tenantID)
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaID).Scan(&setorID); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO maquina (tenant_id, setor_id, numero_patrimonio, nome, criticidade) VALUES ($1, $2, 'P-1', 'Forno', 'Alta') RETURNING id`, tenantID, setorID).Scan(&maquinaID); err != nil {
		t.Fatalf("erro ao criar máquina: %v", err)
	}

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
	admin := cadastrar("Ana", "ana@ind-prev.com", "administrador", model.NovoUsuarioPayload{})
	tecnico := cadastrar("Eder", "eder@ind-prev.com", "tecnico", model.NovoUsuarioPayload{LojasIds: []int64{lojaID}, Area: &area})
	sol := cadastrar("Bruno", "bruno@ind-prev.com", "solicitante", model.NovoUsuarioPayload{LojasIds: []int64{lojaID}, SetoresIds: []int64{setorID}})

	// As duas quebras, pelo caminho de verdade.
	quebra := func(descricao string) model.OrdemServico {
		t.Helper()
		s, err := svcSolicitacao.CadastrarSolicitacaoMaquinario(ctx, tenantID, sol.Id, model.NovaSolicitacaoMaquinarioPayload{
			MaquinaId: maquinaID, Descricao: descricao, Impactos: []string{"Afeta Produção"},
			FotoChave: "tenant/1/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 1,
		})
		if err != nil {
			t.Fatalf("erro ao criar solicitação: %v", err)
		}
		os, err := svcSolicitacao.AbrirOS(ctx, tenantID, admin.Id, "administrador", s.Id, model.AberturaOrdemServicoPayload{Urgencia: "Alta", TecnicoId: tecnico.Id})
		if err != nil {
			t.Fatalf("erro ao abrir OS: %v", err)
		}
		return os
	}
	q1 := quebra("Quebra 1")
	q2 := quebra("Quebra 2")

	// E a revisão, pelo job.
	exec(`INSERT INTO preventiva (tenant_id, maquina_id, descricao, intervalo_dias, proxima_data, ativa, tecnico_id)
	      VALUES ($1, $2, 'Revisão', 30, CURRENT_DATE - 1, true, $3)`, tenantID, maquinaID, tecnicoPrev)
	if criadas, err := svcPrev.AbrirSolicitacoesDePreventivasVencidas(ctx); err != nil || criadas != 1 {
		t.Fatalf("job: criadas = %d, err = %v", criadas, err)
	}
	var osPrev int64
	if err := pool.QueryRow(ctx, `SELECT os.id FROM ordem_servico os JOIN solicitacao_os s ON s.id = os.solicitacao_id WHERE s.origem = 'preventiva'`).Scan(&osPrev); err != nil {
		t.Fatalf("erro ao achar a OS de preventiva: %v", err)
	}

	// Linha do tempo pregada (datas fixas): quebras em 01/01 e 02/03 (60 dias =
	// 1440h entre elas), revisão em 21/01 no meio. Reparos de 4h, 6h e 1h.
	encerrar := func(osID int64, aberta, fim string, custo float64) {
		t.Helper()
		exec(`UPDATE ordem_servico SET status = 'Concluída', aberta_em = $2, iniciada_em = $2 WHERE id = $1`, osID, aberta)
		exec(`INSERT INTO os_encerramento (tenant_id, ordem_servico_id, tipo, tipo_defeito, encerrado_por_id, data_fim, defeito_constatado, causa_raiz, solucao)
		      VALUES ($1, $2, 'maquinario', 'Corretiva', $3, $4, 'x', 'y', 'z')`, tenantID, osID, tecnico.Id, fim)
		exec(`INSERT INTO os_custo (tenant_id, ordem_servico_id, tipo, custo_manutencao, lancado_por_id)
		      VALUES ($1, $2, 'maquinario', $3, $4)`, tenantID, osID, custo, admin.Id)
	}
	encerrar(q1.Id, "2026-01-01 08:00:00+00", "2026-01-01 12:00:00+00", 100)
	encerrar(osPrev, "2026-01-21 08:00:00+00", "2026-01-21 09:00:00+00", 30)
	encerrar(q2.Id, "2026-03-02 08:00:00+00", "2026-03-02 14:00:00+00", 200)

	ind, err := svc.ObterIndicadoresDaMaquina(ctx, tenantID, maquinaID, admin.Id, "administrador")
	if err != nil {
		t.Fatalf("erro ao obter indicadores: %v", err)
	}

	if ind.MtbfHoras != 1440 {
		t.Errorf("mtbfHoras = %v, esperado 1440 (60 dias entre as duas quebras; com a revisão contada daria 720)", ind.MtbfHoras)
	}
	if ind.MttrHoras != 5 {
		t.Errorf("mttrHoras = %v, esperado 5 ((4h + 6h) / 2; com a revisão de 1h daria 3.67)", ind.MttrHoras)
	}
	if ind.CustoTotal != 300 || ind.QuantidadeOs != 2 {
		t.Errorf("maquinário = %v em %d OS, esperado 300 em 2 -- a revisão sai à parte", ind.CustoTotal, ind.QuantidadeOs)
	}
	if ind.CustoPreventivas != 30 || ind.QuantidadePreventivas != 1 {
		t.Errorf("preventivas = %v em %d, esperado 30 em 1", ind.CustoPreventivas, ind.QuantidadePreventivas)
	}

	// O painel da loja usa a mesma conta (mesmo `resumir`).
	loja, err := svc.ObterIndicadoresDaLoja(ctx, tenantID, lojaID, admin.Id, "administrador")
	if err != nil {
		t.Fatalf("erro ao obter indicadores da loja: %v", err)
	}
	if loja.MtbfHoras != 1440 || loja.MttrHoras != 5 || loja.CustoTotal != 300 || loja.CustoPreventivas != 30 {
		t.Errorf("loja: mtbf %v, mttr %v, custo %v, preventivas %v, esperado 1440, 5, 300 e 30", loja.MtbfHoras, loja.MttrHoras, loja.CustoTotal, loja.CustoPreventivas)
	}

	// A lista por trás do card "Custo Maquinário": a soma dos itens é o número do card, e a
	// preventiva NÃO está nela (tem a faixa e a lista dela).
	t.Run("a lista do maquinário bate com o card, sem a preventiva", func(t *testing.T) {
		lista, err := svc.ListarOrdensDoPainelDaLoja(ctx, tenantID, lojaID, admin.Id, "administrador", model.GrupoMaquinario, FiltrosOrdensDoPainel{}, 1)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if lista.Total != int64(loja.QuantidadeOs) || len(lista.Dados) != loja.QuantidadeOs {
			t.Fatalf("lista tem %d (total %d), o card diz %d OS", len(lista.Dados), lista.Total, loja.QuantidadeOs)
		}
		var soma float64
		preventivas := 0
		for _, o := range lista.Dados {
			soma += o.CustoTotal
			if o.Origem == "preventiva" {
				preventivas++
			}
			if o.Item != "Forno" || o.Codigo == nil || *o.Codigo != "P-1" || o.MaquinaId == nil || *o.MaquinaId != maquinaID {
				t.Errorf("OS %d com a máquina errada: %+v", o.Id, o)
			}
		}
		if soma != loja.CustoTotal {
			t.Errorf("a soma dos itens é R$ %v, o card diz R$ %v", soma, loja.CustoTotal)
		}
		if preventivas != 0 {
			t.Errorf("%d OS de preventiva na lista do maquinário, esperado 0", preventivas)
		}
		// Mais novo primeiro: a quebra de março encerrou por último.
		if lista.Dados[0].Id != q2.Id {
			t.Errorf("primeira = OS %d, esperado a mais recente (%d)", lista.Dados[0].Id, q2.Id)
		}
	})

	t.Run("a lista do maquinário filtra por máquina, setor e mês", func(t *testing.T) {
		no := func(f FiltrosOrdensDoPainel) int64 {
			t.Helper()
			l, err := svc.ListarOrdensDoPainelDaLoja(ctx, tenantID, lojaID, admin.Id, "administrador", model.GrupoMaquinario, f, 1)
			if err != nil {
				t.Fatalf("erro ao listar: %v", err)
			}
			return l.Total
		}
		janeiro, marco, outraMaquina, outroSetor := "2026-01", "2026-03", maquinaID+1000, setorID+1000
		if n := no(FiltrosOrdensDoPainel{MaquinaId: &maquinaID}); n != 2 {
			t.Errorf("da máquina: %d, esperado 2", n)
		}
		if n := no(FiltrosOrdensDoPainel{MaquinaId: &outraMaquina}); n != 0 {
			t.Errorf("de outra máquina: %d, esperado 0", n)
		}
		if n := no(FiltrosOrdensDoPainel{SetorId: &outroSetor}); n != 0 {
			t.Errorf("de outro setor: %d, esperado 0", n)
		}
		// Mês de ENCERRAMENTO: janeiro tem a primeira quebra (a revisão é da outra lista);
		// março, a segunda quebra.
		if n := no(FiltrosOrdensDoPainel{Mes: &janeiro}); n != 1 {
			t.Errorf("janeiro: %d, esperado 1", n)
		}
		if n := no(FiltrosOrdensDoPainel{Mes: &marco}); n != 1 {
			t.Errorf("março: %d, esperado 1", n)
		}
	})

	// A faixa das preventivas tem a lista dela, com os mesmos recortes (inclusive a máquina,
	// que o reparo não tem): a soma dos itens é o número da faixa.
	t.Run("a lista das preventivas bate com a faixa e filtra como a do maquinário", func(t *testing.T) {
		listar := func(f FiltrosOrdensDoPainel) model.RespostaPaginada[model.OrdemDoPainel] {
			t.Helper()
			l, err := svc.ListarOrdensDoPainelDaLoja(ctx, tenantID, lojaID, admin.Id, "administrador", model.GrupoPreventivas, f, 1)
			if err != nil {
				t.Fatalf("erro ao listar: %v", err)
			}
			return l
		}
		lista := listar(FiltrosOrdensDoPainel{})
		if lista.Total != int64(loja.QuantidadePreventivas) || len(lista.Dados) != 1 || lista.Dados[0].Id != osPrev {
			t.Fatalf("lista = %+v, esperado só a OS de preventiva %d", lista.Dados, osPrev)
		}
		if lista.Dados[0].CustoTotal != loja.CustoPreventivas || lista.Dados[0].Origem != "preventiva" {
			t.Errorf("item = %+v, a faixa diz R$ %v", lista.Dados[0], loja.CustoPreventivas)
		}
		janeiro, marco := "2026-01", "2026-03"
		if n := listar(FiltrosOrdensDoPainel{MaquinaId: &maquinaID, Mes: &janeiro}).Total; n != 1 {
			t.Errorf("máquina em janeiro: %d, esperado 1", n)
		}
		if n := listar(FiltrosOrdensDoPainel{Mes: &marco}).Total; n != 0 {
			t.Errorf("março: %d, esperado 0", n)
		}
	})
}
