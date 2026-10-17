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

// TestResumoEFiltrosAvancados cobre o que deixa o Painel do Gestor paginar as OS
// Finalizadas no servidor: os filtros avançados (máquina, período, valor) agora
// no WHERE, o resumo por setor que monta os cards, e GET /ordens-servico/:id.
//
// O que mais importa aqui é a CONSISTÊNCIA: para os mesmos filtros, a soma do
// resumo tem que ser o total da listagem paginada. Se divergirem, o card diz 40
// e a lista mostra 12.
func TestResumoEFiltrosAvancados(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svcSolicitacao := NewRepoSolicitacao(pool)
	svcOS := NewRepoOrdemServico(pool)

	var tenantID, lojaID, setorA, setorB int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('os-resumo', 'Empresa Resumo') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	tecnicoPrev := tecnicoParaPreventiva(t, ctx, pool, tenantID)
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	for _, s := range []struct {
		nome string
		dest *int64
	}{{"Padaria", &setorA}, {"Açougue", &setorB}} {
		if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, $3) RETURNING id`, tenantID, lojaID, s.nome).Scan(s.dest); err != nil {
			t.Fatalf("erro ao criar setor: %v", err)
		}
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
	admin := cadastrar("Ana", "ana@os-resumo.com", "administrador", model.NovoUsuarioPayload{})
	tecnico := cadastrar("Eder", "eder@os-resumo.com", "tecnico", model.NovoUsuarioPayload{LojasIds: []int64{lojaID}, Area: &area})
	tecnicoOutro := cadastrar("Ivo", "ivo@os-resumo.com", "tecnico", model.NovoUsuarioPayload{LojasIds: []int64{lojaID}, Area: &area})
	gestorA := cadastrar("Carla", "carla@os-resumo.com", "gestor", model.NovoUsuarioPayload{LojasIds: []int64{lojaID}, SetoresIds: []int64{setorA}})
	solA := cadastrar("Bruno", "bruno@os-resumo.com", "solicitante", model.NovoUsuarioPayload{LojasIds: []int64{lojaID}, SetoresIds: []int64{setorA}})
	solB := cadastrar("Duda", "duda@os-resumo.com", "solicitante", model.NovoUsuarioPayload{LojasIds: []int64{lojaID}, SetoresIds: []int64{setorB}})

	// Cada OS: máquina própria (patrimônio identifica), no setor do solicitante;
	// `custo` > 0 encerra e lança, 0 deixa Em Andamento (não finalizada).
	nova := func(patrimonio string, setor int64, solicitante model.Usuario, custo float64) int64 {
		t.Helper()
		m, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
			SetorID: setor, Criticidade: "Alta", NumeroPatrimonio: patrimonio, Nome: "Máquina " + patrimonio,
			Preventivas: []model.PreventivaPayload{{
				TecnicoId: tecnicoPrev, Descricao: "Revisão", IntervaloDias: 30,
				ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
			}},
		})
		if err != nil {
			t.Fatalf("erro ao criar máquina: %v", err)
		}
		sol, err := svcSolicitacao.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoMaquinarioPayload{
			MaquinaId: m.Id, Descricao: "Defeito em " + patrimonio, Impactos: []string{"Afeta Produção"},
			FotoChave: "tenant/1/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 1,
		})
		if err != nil {
			t.Fatalf("erro ao criar solicitação: %v", err)
		}
		os, err := svcSolicitacao.AbrirOS(ctx, tenantID, admin.Id, "administrador", sol.Id, model.AberturaOrdemServicoPayload{Urgencia: "Alta", TecnicoId: tecnico.Id})
		if err != nil {
			t.Fatalf("erro ao abrir OS: %v", err)
		}
		if _, err := svcOS.Iniciar(ctx, tenantID, tecnico.Id, os.Id); err != nil {
			t.Fatalf("erro ao iniciar: %v", err)
		}
		if custo > 0 {
			if _, err := svcOS.Encerrar(ctx, tenantID, tecnico.Id, os.Id, model.EncerramentoOrdemServicoPayload{
				TipoDefeito: "Corretiva", DefeitoConstatado: "x", CausaRaiz: "y", Solucao: "z",
				Itens: itensSemHoraTecnica(custo),
			}); err != nil {
				t.Fatalf("erro ao encerrar: %v", err)
			}
		}
		return os.Id
	}

	osA1 := nova("PAT-A1", setorA, solA, 100)
	osA2 := nova("PAT-A2", setorA, solA, 500)
	osB1 := nova("PAT-B1", setorB, solB, 100)
	nova("PAT-A3", setorA, solA, 0) // ainda em andamento: nunca é finalizada

	sim := true
	finalizadas := FiltrosOrdemServico{Finalizada: &sim}
	str := func(v string) *string { return &v }
	num := func(v float64) *float64 { return &v }
	dia := func(d time.Time) *time.Time { return &d }

	resumoDe := func(t *testing.T, usuario int64, perfil string, f FiltrosOrdemServico) map[int64]int64 {
		t.Helper()
		linhas, err := svcOS.ResumoOrdensServicoPorSetor(ctx, tenantID, usuario, perfil, f)
		if err != nil {
			t.Fatalf("erro no resumo: %v", err)
		}
		// A linha é por máquina: o setor é a soma das linhas dele.
		porSetor := map[int64]int64{}
		for _, l := range linhas {
			porSetor[l.SetorId] += l.Quantidade
		}
		return porSetor
	}

	t.Run("resumo conta só as finalizadas, por setor", func(t *testing.T) {
		r := resumoDe(t, admin.Id, "administrador", finalizadas)
		if r[setorA] != 2 || r[setorB] != 1 || len(r) != 2 {
			t.Errorf("resumo = %v, esperado Padaria 2 e Açougue 1", r)
		}
	})

	t.Run("a soma do resumo é o total da listagem paginada, em qualquer filtro", func(t *testing.T) {
		filtros := map[string]FiltrosOrdemServico{
			"só finalizadas":   finalizadas,
			"valor mínimo 200": {Finalizada: &sim, ValorMinimo: num(200)},
			"valor máximo 100": {Finalizada: &sim, ValorMaximo: num(100)},
			"máquina":          {Finalizada: &sim, Maquina: str("PAT-B")},
			"período de hoje":  {Finalizada: &sim, DataInicio: dia(time.Now()), DataFim: dia(time.Now())},
			"período passado":  {Finalizada: &sim, DataFim: dia(time.Now().AddDate(-1, 0, 0))},
		}
		for nome, f := range filtros {
			var soma int64
			for _, q := range resumoDe(t, admin.Id, "administrador", f) {
				soma += q
			}
			pagina, err := svcOS.ListarOrdensServicoPaginada(ctx, tenantID, admin.Id, "administrador", f, 1)
			if err != nil {
				t.Fatalf("%s: erro ao listar: %v", nome, err)
			}
			if soma != pagina.Total {
				t.Errorf("%s: resumo soma %d, listagem tem %d", nome, soma, pagina.Total)
			}
		}
	})

	t.Run("filtros avançados recortam como a tela fazia", func(t *testing.T) {
		contar := func(f FiltrosOrdemServico) int {
			t.Helper()
			l, err := svcOS.ListarOrdensServico(ctx, tenantID, admin.Id, "administrador", f)
			if err != nil {
				t.Fatalf("erro ao listar: %v", err)
			}
			return len(l)
		}
		casos := []struct {
			nome   string
			filtro FiltrosOrdemServico
			quer   int
		}{
			{"valor mínimo 200 só pega a de 500", FiltrosOrdemServico{Finalizada: &sim, ValorMinimo: num(200)}, 1},
			{"valor máximo 100 pega as duas de 100", FiltrosOrdemServico{Finalizada: &sim, ValorMaximo: num(100)}, 2},
			{"faixa 100 a 500 pega as três (limites inclusivos)", FiltrosOrdemServico{Finalizada: &sim, ValorMinimo: num(100), ValorMaximo: num(500)}, 3},
			// Patrimônio, não só nome: máquinas iguais têm o mesmo nome e é pelo
			// patrimônio que o Gestor chega numa só. A `busca` antiga não olhava.
			{"máquina por patrimônio", FiltrosOrdemServico{Finalizada: &sim, Maquina: str("pat-a2")}, 1},
			{"máquina por nome pega as três finalizadas", FiltrosOrdemServico{Finalizada: &sim, Maquina: str("Máquina PAT")}, 3},
			{"busca também acha pelo patrimônio", FiltrosOrdemServico{Finalizada: &sim, Busca: str("PAT-B1")}, 1},
			{"período que inclui hoje", FiltrosOrdemServico{Finalizada: &sim, DataInicio: dia(time.Now().AddDate(0, 0, -1)), DataFim: dia(time.Now().AddDate(0, 0, 1))}, 3},
			{"período só no passado não pega nada", FiltrosOrdemServico{Finalizada: &sim, DataFim: dia(time.Now().AddDate(0, 0, -2))}, 0},
			// Sem custo lançado não combina com filtro de valor (a OS em andamento).
			{"filtro de valor ignora OS sem custo", FiltrosOrdemServico{ValorMinimo: num(0)}, 3},
		}
		for _, c := range casos {
			if got := contar(c.filtro); got != c.quer {
				t.Errorf("%s: %d OS, esperado %d", c.nome, got, c.quer)
			}
		}
	})

	t.Run("resumo respeita o escopo: o gestor do setor A não vê o B", func(t *testing.T) {
		r := resumoDe(t, gestorA.Id, "gestor", finalizadas)
		if r[setorA] != 2 || len(r) != 1 {
			t.Errorf("resumo do gestor = %v, esperado só a Padaria com 2", r)
		}
	})

	t.Run("a página de um setor traz só as OS dele", func(t *testing.T) {
		f := finalizadas
		f.SetorId = &setorA
		pagina, err := svcOS.ListarOrdensServicoPaginada(ctx, tenantID, gestorA.Id, "gestor", f, 1)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if pagina.Total != 2 || len(pagina.Dados) != 2 {
			t.Errorf("total %d / %d OS, esperado 2", pagina.Total, len(pagina.Dados))
		}
	})

	t.Run("GET por id respeita o escopo dos perfis", func(t *testing.T) {
		casos := []struct {
			nome    string
			usuario int64
			perfil  string
			os      int64
			ok      bool
		}{
			{"administrador", admin.Id, "administrador", osB1, true},
			{"gestor do setor", gestorA.Id, "gestor", osA1, true},
			{"técnico dono", tecnico.Id, "tecnico", osA2, true},
			{"gestor de outro setor", gestorA.Id, "gestor", osB1, false},
			{"técnico que não é dono", tecnicoOutro.Id, "tecnico", osA1, false},
			{"OS inexistente", admin.Id, "administrador", 999999, false},
		}
		for _, c := range casos {
			ordem, err := svcOS.ObterOrdemServico(ctx, tenantID, c.usuario, c.perfil, c.os)
			if c.ok && (err != nil || ordem.Id != c.os) {
				t.Errorf("%s: erro %v / id %d", c.nome, err, ordem.Id)
			}
			if !c.ok && !errors.Is(err, helper.ErrNaoEncontrado) {
				t.Errorf("%s: erro = %v, esperado ErrNaoEncontrado", c.nome, err)
			}
		}
	})

	// Por último: o reparo entra no Açougue e mudaria as contagens dos casos acima.
	t.Run("nível de máquina: uma linha por máquina, reparos juntos, e a lista bate com cada uma", func(t *testing.T) {
		sReparo, err := svcSolicitacao.CadastrarSolicitacaoReparo(ctx, tenantID, solB.Id, model.NovaSolicitacaoReparoPayload{
			Item: "Lâmpada", Descricao: "Queimou", FotoChave: "tenant/1/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 1,
		})
		if err != nil {
			t.Fatalf("erro ao criar reparo: %v", err)
		}
		osReparo, err := svcSolicitacao.AbrirOS(ctx, tenantID, admin.Id, "administrador", sReparo.Id, model.AberturaOrdemServicoPayload{Urgencia: "Baixa", TecnicoId: tecnico.Id})
		if err != nil {
			t.Fatalf("erro ao abrir OS do reparo: %v", err)
		}
		if _, err := svcOS.Iniciar(ctx, tenantID, tecnico.Id, osReparo.Id); err != nil {
			t.Fatalf("erro ao iniciar: %v", err)
		}
		if _, err := svcOS.Encerrar(ctx, tenantID, tecnico.Id, osReparo.Id, model.EncerramentoOrdemServicoPayload{
			TipoDefeito: "Predial", DefeitoConstatado: "x", CausaRaiz: "y", Solucao: "z", Itens: itensSemHoraTecnica(30),
		}); err != nil {
			t.Fatalf("erro ao encerrar: %v", err)
		}

		f := finalizadas
		f.SetorId = &setorB
		linhas, err := svcOS.ResumoOrdensServicoPorSetor(ctx, tenantID, admin.Id, "administrador", f)
		if err != nil {
			t.Fatalf("erro no resumo: %v", err)
		}
		if len(linhas) != 2 {
			t.Fatalf("resumo do Açougue = %+v, esperado 2 linhas (PAT-B1 e reparos)", linhas)
		}
		var maquinaB1 *int64
		reparos := 0
		for _, l := range linhas {
			switch {
			case l.MaquinaId == nil:
				reparos++
				if l.MaquinaNome != nil || l.Quantidade != 1 {
					t.Errorf("linha de reparos = %+v, esperado sem máquina e 1 OS", l)
				}
			case l.MaquinaCodigo != nil && *l.MaquinaCodigo == "PAT-B1":
				maquinaB1 = l.MaquinaId
			}
		}
		if reparos != 1 || maquinaB1 == nil {
			t.Fatalf("resumo do Açougue = %+v, esperado PAT-B1 e uma linha de reparos", linhas)
		}

		total := func(f FiltrosOrdemServico) (int64, []model.OrdemServico) {
			t.Helper()
			p, err := svcOS.ListarOrdensServicoPaginada(ctx, tenantID, admin.Id, "administrador", f, 1)
			if err != nil {
				t.Fatalf("erro ao listar: %v", err)
			}
			return p.Total, p.Dados
		}

		porMaquina := f
		porMaquina.MaquinaId = maquinaB1
		if n, dados := total(porMaquina); n != 1 || dados[0].Id != osB1 {
			t.Errorf("máquina PAT-B1: %d OS, esperado só a %d", n, osB1)
		}

		soReparos := f
		soReparos.Reparo = &sim
		if n, dados := total(soReparos); n != 1 || dados[0].Id != osReparo.Id {
			t.Errorf("reparos do Açougue: %d OS, esperado só a %d", n, osReparo.Id)
		}

		nao := false
		soMaquinario := finalizadas
		soMaquinario.Reparo = &nao
		if n, _ := total(soMaquinario); n != 3 {
			t.Errorf("reparo=false: %d OS, esperado as 3 de maquinário", n)
		}

		// Filtro estreita, nunca amplia: a máquina do Açougue pedida pela gestora da Padaria.
		escopo, err := svcOS.ListarOrdensServicoPaginada(ctx, tenantID, gestorA.Id, "gestor", porMaquina, 1)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if escopo.Total != 0 {
			t.Errorf("gestora da Padaria viu %d OS de uma máquina do Açougue", escopo.Total)
		}
	})
}
