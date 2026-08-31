package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// TestListarOrdensServico cobre a query de GET /ordens-servico antes de
// existir service em cima dela: o recorte por escopo (que é o WHERE, não o
// RBAC -- a rota é aberta a gestor/técnico/administrador) e os filtros que os
// três painéis mandam.
//
// Falha de escopo aqui é silenciosa em produção -- a listagem responde 200
// com OS de setor alheio e nenhuma tela reclama. Mesmo motivo de
// TestEscopoDasListagens existir para máquina/preventiva.
func TestListarOrdensServico(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	repo := repository.New(pool)
	svcUsuario := NewRepoUsuario(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svc := NewRepoSolicitacao(pool)

	// Duas lojas e três setores: é o setor da outra loja que separa "gestor
	// com acesso total à Loja A" de "enxerga o tenant".
	var tenantID, lojaA, lojaB, setorA, setorB, setorC int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('os', 'Empresa OS') RETURNING id`).Scan(&tenantID); err != nil {
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
	}{{"Padaria", lojaA, &setorA}, {"Açougue", lojaA, &setorB}, {"Hortifruti", lojaB, &setorC}} {
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
	serra := maquina("Serra", setorB)
	balanca := maquina("Balanca", setorC)

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

	admin := cadastrar("Ana", "ana@os.com", "administrador", model.NovoUsuarioPayload{})
	gestorParcial := cadastrar("Carla", "carla@os.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorB},
	})
	gestorTotal := cadastrar("Dora", "dora@os.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, AcessoTotalSetores: true,
	})
	tecnicoA := cadastrar("Eder", "eder@os.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, Area: &area,
	})
	tecnicoB := cadastrar("Ivo", "ivo@os.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaB}, Area: &area,
	})
	solA := cadastrar("Bruno", "bruno@os.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
	})
	solB := cadastrar("Bia", "bia@os.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorB},
	})
	solC := cadastrar("Caio", "caio@os.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaB}, SetoresIds: []int64{setorC},
	})

	// Uma OS por setor. Vão todas pelo caminho de verdade (solicitação humana
	// -> AbrirOS) porque não existe outro: ordem_servico só nasce da aprovação
	// do Gestor, e um INSERT na mão aqui testaria uma linha que a API nunca
	// produz.
	abrirMaquinario := func(solicitante model.Usuario, maquinaID int64, descricao string, tecnico model.Usuario) model.OrdemServico {
		t.Helper()
		sol, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoMaquinarioPayload{
			MaquinaId: maquinaID, Descricao: descricao, Impactos: []string{"Afeta Produção"},
			FotoChave: "tenant/1/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 1,
		})
		if err != nil {
			t.Fatalf("erro ao criar solicitação de %s: %v", descricao, err)
		}
		os, err := svc.AbrirOS(ctx, tenantID, admin.Id, "administrador", sol.Id, model.AberturaOrdemServicoPayload{
			Urgencia: "Alta", TecnicoId: tecnico.Id,
		})
		if err != nil {
			t.Fatalf("erro ao abrir OS de %s: %v", descricao, err)
		}
		return os
	}

	osForno := abrirMaquinario(solA, forno.Id, "Forno não aquece", tecnicoA)
	osSerra := abrirMaquinario(solB, serra.Id, "Serra travando", tecnicoA)
	osBalanca := abrirMaquinario(solC, balanca.Id, "Balança descalibrada", tecnicoB)

	// Um reparo: prova que a listagem aguenta OS sem máquina (LEFT JOIN
	// maquina) e serve ao filtro por tipo.
	solReparo, err := svc.CadastrarSolicitacaoReparo(ctx, tenantID, solA.Id, model.NovaSolicitacaoReparoPayload{
		Item: "Lâmpada queimada", Descricao: "Corredor principal",
		FotoChave: "tenant/1/reparo.jpg", FotoMime: "image/jpeg", FotoTamanho: 1,
	})
	if err != nil {
		t.Fatalf("erro ao criar reparo: %v", err)
	}
	osReparo, err := svc.AbrirOS(ctx, tenantID, admin.Id, "administrador", solReparo.Id, model.AberturaOrdemServicoPayload{
		Urgencia: "Baixa", TecnicoId: tecnicoA.Id,
	})
	if err != nil {
		t.Fatalf("erro ao abrir OS do reparo: %v", err)
	}

	listar := func(t *testing.T, p repository.ListarOrdensServicoParams) []repository.ListarOrdensServicoRow {
		t.Helper()
		p.TenantID = tenantID
		linhas, err := repo.ListarOrdensServico(ctx, p)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		return linhas
	}
	ids := func(linhas []repository.ListarOrdensServicoRow) []int64 {
		out := make([]int64, 0, len(linhas))
		for _, l := range linhas {
			out = append(out, l.ID)
		}
		slices.Sort(out)
		return out
	}
	ordenados := func(v ...int64) []int64 {
		slices.Sort(v)
		return v
	}

	t.Run("escopo recorta por loja E setor", func(t *testing.T) {
		casos := []struct {
			nome     string
			escopo   *int64
			esperado []int64
		}{
			// Administrador não tem linha em usuario_escopo
			// (trg_usuario_escopo_nao_admin recusa): filtrar por escopo
			// devolveria zero justamente para quem enxerga tudo.
			{"administrador vê o tenant inteiro", nil, ordenados(osForno.Id, osSerra.Id, osBalanca.Id, osReparo.Id)},
			// Açougue da Loja A: a Padaria (Forno + o reparo, que herda o
			// setor do solicitante) e a Loja B ficam de fora.
			{"gestor parcial vê só o setor dele", &gestorParcial.Id, ordenados(osSerra.Id)},
			// Loja A inteira, sem a Loja B.
			{"gestor total vê a loja inteira", &gestorTotal.Id, ordenados(osForno.Id, osSerra.Id, osReparo.Id)},
			// Escopo do técnico é acesso_total_setores nas lojas dele.
			{"técnico das duas lojas vê tudo", &tecnicoA.Id, ordenados(osForno.Id, osSerra.Id, osBalanca.Id, osReparo.Id)},
			{"técnico de uma loja só vê a dele", &tecnicoB.Id, ordenados(osBalanca.Id)},
			// Escopo não é "as minhas OS": o solicitante da Padaria enxerga
			// as OS do setor dele, inclusive a que ele não abriu.
			{"solicitante vê o próprio setor", &solA.Id, ordenados(osForno.Id, osReparo.Id)},
			{"solicitante de outro setor", &solC.Id, ordenados(osBalanca.Id)},
		}
		for _, caso := range casos {
			t.Run(caso.nome, func(t *testing.T) {
				got := ids(listar(t, repository.ListarOrdensServicoParams{EscopoUsuarioID: caso.escopo}))
				if !slices.Equal(got, caso.esperado) {
					t.Errorf("ids = %v, esperado %v", got, caso.esperado)
				}
			})
		}
	})

	t.Run("filtro do cliente estreita, nunca amplia", func(t *testing.T) {
		// ?lojaId= de uma loja fora do escopo devolve vazio, não a loja.
		// Mesma trava de TestEscopoDasListagens.
		got := listar(t, repository.ListarOrdensServicoParams{EscopoUsuarioID: &gestorTotal.Id, LojaID: &lojaB})
		if len(got) != 0 {
			t.Errorf("gestor da Loja A pedindo ?lojaId=B recebeu %d OS, esperado 0", len(got))
		}
		// E dentro do escopo ele estreita de verdade.
		got = listar(t, repository.ListarOrdensServicoParams{EscopoUsuarioID: nil, LojaID: &lojaB})
		if !slices.Equal(ids(got), ordenados(osBalanca.Id)) {
			t.Errorf("?lojaId=B para o admin = %v, esperado só a Balança", ids(got))
		}
	})

	t.Run("filtros combináveis", func(t *testing.T) {
		// Status entra como text[] e o SQL casta para status_os -- ver a nota
		// na query. Os rótulos são os mesmos que o front manda em ?status=.
		aberta, concluida := string(repository.StatusOsAberta), string(repository.StatusOsConcluda)
		tipoReparo, tipoMaquinario := repository.TipoOsReparo, repository.TipoOsMaquinario
		busca := "travando"

		casos := []struct {
			nome     string
			params   repository.ListarOrdensServicoParams
			esperado []int64
		}{
			// Toda OS nasce 'Aberta' -- o ciclo de vida é fase 2.
			{"status Aberta pega todas", repository.ListarOrdensServicoParams{Status: []string{aberta}}, ordenados(osForno.Id, osSerra.Id, osBalanca.Id, osReparo.Id)},
			{"status Concluída não pega nenhuma", repository.ListarOrdensServicoParams{Status: []string{concluida}}, []int64{}},
			{"status aceita mais de um valor", repository.ListarOrdensServicoParams{Status: []string{aberta, concluida}}, ordenados(osForno.Id, osSerra.Id, osBalanca.Id, osReparo.Id)},
			{"tipo reparo", repository.ListarOrdensServicoParams{Tipo: &tipoReparo}, ordenados(osReparo.Id)},
			{"tipo maquinário", repository.ListarOrdensServicoParams{Tipo: &tipoMaquinario}, ordenados(osForno.Id, osSerra.Id, osBalanca.Id)},
			{"tecnicoId", repository.ListarOrdensServicoParams{TecnicoID: &tecnicoB.Id}, ordenados(osBalanca.Id)},
			{"busca na descrição", repository.ListarOrdensServicoParams{Busca: &busca}, ordenados(osSerra.Id)},
			// Combinar estreita: o técnico A atende a Loja B, mas nenhuma OS
			// dele está lá.
			{"tecnicoId + lojaId", repository.ListarOrdensServicoParams{TecnicoID: &tecnicoA.Id, LojaID: &lojaB}, []int64{}},
		}
		for _, caso := range casos {
			t.Run(caso.nome, func(t *testing.T) {
				got := ids(listar(t, caso.params))
				if !slices.Equal(got, caso.esperado) {
					t.Errorf("ids = %v, esperado %v", got, caso.esperado)
				}
			})
		}
	})

	t.Run("busca cobre o item do reparo, que não tem máquina", func(t *testing.T) {
		item := "Lâmpada"
		got := ids(listar(t, repository.ListarOrdensServicoParams{Busca: &item}))
		if !slices.Equal(got, ordenados(osReparo.Id)) {
			t.Errorf("ids = %v, esperado só o reparo", got)
		}
	})

	t.Run("denormalizados que o front tipa como obrigatórios", func(t *testing.T) {
		linhas := listar(t, repository.ListarOrdensServicoParams{TecnicoID: &tecnicoB.Id})
		if len(linhas) != 1 {
			t.Fatalf("esperava 1 OS, veio %d", len(linhas))
		}
		l := linhas[0]

		if l.SetorNome != "Hortifruti" || l.LojaNome != "Loja B" || l.LojaID != lojaB {
			t.Errorf("setor/loja não vieram denormalizados: %q / %q / %d", l.SetorNome, l.LojaNome, l.LojaID)
		}
		if l.TecnicoNome != "Ivo" {
			t.Errorf("tecnicoNome = %q, esperado Ivo", l.TecnicoNome)
		}
		if l.TecnicoArea == nil || *l.TecnicoArea != area {
			t.Errorf("tecnicoArea = %v, esperado %q", l.TecnicoArea, area)
		}
		if l.MaquinaNome == nil || *l.MaquinaNome != "Balanca" {
			t.Errorf("maquinaNome = %v", l.MaquinaNome)
		}
		if l.SolicitanteNome == nil || *l.SolicitanteNome != "Caio" {
			t.Errorf("solicitanteNome = %v", l.SolicitanteNome)
		}
		if !l.AfetaProducao {
			t.Error("afetaProducao devia vir true (o solicitante marcou o impacto)")
		}
		if l.Urgencia != repository.NivelUrgenciaAlta {
			t.Errorf("urgencia = %v, esperado Alta", l.Urgencia)
		}
		if l.Status != repository.StatusOsAberta {
			t.Errorf("status = %v, esperado Aberta", l.Status)
		}
		// dataSolicitacao é o criado_em da solicitação de origem, e é dela que
		// parte o relógio de máquina parada -- tem que vir antes da abertura.
		if !l.DataSolicitacao.Valid || !l.AbertaEm.Valid {
			t.Fatalf("datas inválidas: solicitacao=%v abertura=%v", l.DataSolicitacao, l.AbertaEm)
		}
		if l.DataSolicitacao.Time.After(l.AbertaEm.Time) {
			t.Errorf("dataSolicitacao (%v) depois da dataAbertura (%v)", l.DataSolicitacao.Time, l.AbertaEm.Time)
		}
		// Ninguém acionou terceiro: os dois campos NULL é o que satisfaz
		// ck_os_executor pela metade "não-terceiros".
		if l.EmpresaTerceirizadaNome != nil || l.EmpresaTerceirizadaID != nil {
			t.Errorf("empresa terceirizada não devia existir: %v / %v", l.EmpresaTerceirizadaID, l.EmpresaTerceirizadaNome)
		}
	})

	t.Run("reparo vem sem máquina e com item", func(t *testing.T) {
		tipoReparo := repository.TipoOsReparo
		linhas := listar(t, repository.ListarOrdensServicoParams{Tipo: &tipoReparo})
		if len(linhas) != 1 {
			t.Fatalf("esperava 1 reparo, veio %d", len(linhas))
		}
		l := linhas[0]
		if l.MaquinaID != nil || l.MaquinaNome != nil || l.MaquinaCodigo != nil {
			t.Errorf("reparo não tem máquina: %v / %v / %v", l.MaquinaID, l.MaquinaNome, l.MaquinaCodigo)
		}
		if l.ItemDescricao == nil || *l.ItemDescricao != "Lâmpada queimada" {
			t.Errorf("itemDescricao = %v", l.ItemDescricao)
		}
		// O setor do reparo vem do escopo do solicitante, não de uma máquina.
		if l.SetorNome != "Padaria" {
			t.Errorf("setorNome do reparo = %q, esperado Padaria", l.SetorNome)
		}
	})

	t.Run("mais nova primeiro", func(t *testing.T) {
		linhas := listar(t, repository.ListarOrdensServicoParams{})
		for i := 1; i < len(linhas); i++ {
			if linhas[i-1].AbertaEm.Time.Before(linhas[i].AbertaEm.Time) {
				t.Fatalf("ordem quebrada em %d: %v antes de %v", i, linhas[i-1].AbertaEm.Time, linhas[i].AbertaEm.Time)
			}
		}
	})

	// ------------------------------------------------------------------
	// Sub-dados do ciclo de vida: encerramento, custo, horas e pausas.
	//
	// Daqui pra baixo o estado das OS muda -- os subtestes acima contam com
	// todas 'Aberta', que é como AbrirOS as deixa.
	//
	// Os INSERTs são na mão, diferente das OS (que vão por AbrirOS): não
	// existe caminho de escrita para os_encerramento/os_custo/os_pausa
	// ainda -- iniciar/pausar/encerrar/custo são a fase 2. Quando existirem,
	// esta montagem vira chamada de service, como já é a de cima.
	// ------------------------------------------------------------------
	t.Run("prepara o ciclo de vida das OS", func(t *testing.T) {
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatalf("erro em %q: %v", sql, err)
			}
		}

		// Forno: encerrada e com custo -- a única "finalizada" de maquinário.
		// A linha do tempo é escolhida para separar os dois relógios: o
		// Solicitante relatou 8h atrás, o Gestor demorou e a OS só começou 5h
		// atrás, com 1h de pausa no meio. Trabalhadas ~4h, parada ~8h. Trocar
		// s.criado_em por os.aberta_em em vw_os_horas (o que a migration 000002
		// desfez) derrubaria a parada para ~5h e o teste pega.
		exec(`UPDATE solicitacao_os SET criado_em = now() - interval '8 hours' WHERE id = $1`, osForno.SolicitacaoId)
		exec(`UPDATE ordem_servico SET status = 'Concluída', iniciada_em = now() - interval '5 hours' WHERE id = $1`, osForno.Id)
		exec(`INSERT INTO os_pausa (ordem_servico_id, status_anterior, motivo, pausada_em, retomada_em)
		      VALUES ($1, 'Em Andamento', 'aguardando peça', now() - interval '4 hours', now() - interval '3 hours')`, osForno.Id)
		exec(`INSERT INTO os_encerramento (tenant_id, ordem_servico_id, tipo, tipo_defeito, encerrado_por_id, data_fim, defeito_constatado, causa_raiz, solucao)
		      VALUES ($1, $2, 'maquinario', 'Corretiva', $3, now(), 'Resistência queimada', 'Fim de vida útil', 'Troca da resistência')`, tenantID, osForno.Id, tecnicoA.Id)
		exec(`INSERT INTO os_custo (tenant_id, ordem_servico_id, tipo, custo_hora_tecnico, custo_manutencao, lancado_por_id)
		      VALUES ($1, $2, 'maquinario', 150.50, 320.25, $3)`, tenantID, osForno.Id, admin.Id)

		// Serra: pausada agora, com a pausa em aberto -- é o pausaAtual que o
		// Gestor vê em destaque na aba "OS em Andamento".
		exec(`UPDATE ordem_servico SET status = 'Pausada', iniciada_em = now() - interval '2 hours' WHERE id = $1`, osSerra.Id)
		exec(`INSERT INTO os_pausa (ordem_servico_id, status_anterior, motivo, pausada_em)
		      VALUES ($1, 'Em Andamento', 'peça em falta no estoque', now() - interval '30 minutes')`, osSerra.Id)

		// Balança: encerrada e SEM custo -- Concluída não é o mesmo que
		// finalizada. É exatamente a fila de "Custos Pendentes".
		exec(`UPDATE ordem_servico SET status = 'Concluída', iniciada_em = now() - interval '3 hours' WHERE id = $1`, osBalanca.Id)
		exec(`INSERT INTO os_encerramento (tenant_id, ordem_servico_id, tipo, tipo_defeito, encerrado_por_id, data_fim, defeito_constatado, causa_raiz, solucao)
		      VALUES ($1, $2, 'maquinario', 'Predial', $3, now(), 'Descalibrada', 'Uso', 'Recalibração')`, tenantID, osBalanca.Id, tecnicoB.Id)

		// Reparo: encerrada e com custo, mas sem custo_hora_tecnico --
		// ck_custo_por_tipo proíbe hora técnica fora de 'maquinario'. E sem
		// impacto marcado, então afeta_producao é falsa: é a OS que prova
		// horasParada nula ("Não se aplica").
		exec(`UPDATE ordem_servico SET status = 'Concluída', iniciada_em = now() - interval '1 hour' WHERE id = $1`, osReparo.Id)
		exec(`INSERT INTO os_encerramento (tenant_id, ordem_servico_id, tipo, tipo_defeito, encerrado_por_id, data_fim, defeito_constatado, causa_raiz, solucao)
		      VALUES ($1, $2, 'reparo', 'Predial', $3, now(), 'Lâmpada queimada', 'Fim de vida útil', 'Substituição')`, tenantID, osReparo.Id, tecnicoA.Id)
		exec(`INSERT INTO os_custo (tenant_id, ordem_servico_id, tipo, custo_manutencao, lancado_por_id)
		      VALUES ($1, $2, 'reparo', 18.90, $3)`, tenantID, osReparo.Id, admin.Id)
	})

	porID := func(t *testing.T, id int64) repository.ListarOrdensServicoRow {
		t.Helper()
		for _, l := range listar(t, repository.ListarOrdensServicoParams{}) {
			if l.ID == id {
				return l
			}
		}
		t.Fatalf("OS %d não veio na listagem", id)
		return repository.ListarOrdensServicoRow{}
	}

	// "Finalizada" é derivado, não status: o Técnico encerrou E o custo foi
	// lançado. É a regra que separa a aba "OS Finalizadas" do Gestor da fila
	// "Custos Pendentes" do Administrador, e as duas telas leem a mesma rota.
	t.Run("finalizada é encerramento MAIS custo", func(t *testing.T) {
		casos := []struct {
			nome     string
			id       int64
			esperado bool
		}{
			{"encerrada com custo", osForno.Id, true},
			{"encerrada com custo, sem hora técnica", osReparo.Id, true},
			{"encerrada sem custo ainda", osBalanca.Id, false},
			{"nem encerrada", osSerra.Id, false},
		}
		for _, caso := range casos {
			t.Run(caso.nome, func(t *testing.T) {
				if got := porID(t, caso.id).Finalizada; got != caso.esperado {
					t.Errorf("finalizada = %v, esperado %v", got, caso.esperado)
				}
			})
		}
	})

	t.Run("filtro finalizada", func(t *testing.T) {
		sim, nao := true, false
		got := ids(listar(t, repository.ListarOrdensServicoParams{Finalizada: &sim}))
		if !slices.Equal(got, ordenados(osForno.Id, osReparo.Id)) {
			t.Errorf("?finalizada=true = %v, esperado Forno+Reparo", got)
		}
		got = ids(listar(t, repository.ListarOrdensServicoParams{Finalizada: &nao}))
		if !slices.Equal(got, ordenados(osSerra.Id, osBalanca.Id)) {
			t.Errorf("?finalizada=false = %v, esperado Serra+Balança", got)
		}
		// A projeção e o filtro repetem a mesma expressão -- se divergirem, a
		// listagem se contradiz. Aqui é onde isso apareceria.
		for _, l := range listar(t, repository.ListarOrdensServicoParams{Finalizada: &sim}) {
			if !l.Finalizada {
				t.Errorf("OS %d veio no filtro finalizada=true com o campo false", l.ID)
			}
		}
	})

	// A fila de Custos Pendentes do Administrador é ?status=Concluída, não
	// ?finalizada=false: ela lista TODA OS concluída, com ou sem custo, porque
	// virou fila de conferência contra a nota fiscal.
	t.Run("status Concluída pega finalizada e pendente de custo", func(t *testing.T) {
		got := ids(listar(t, repository.ListarOrdensServicoParams{Status: []string{string(repository.StatusOsConcluda)}}))
		if !slices.Equal(got, ordenados(osForno.Id, osBalanca.Id, osReparo.Id)) {
			t.Errorf("?status=Concluída = %v", got)
		}
	})

	t.Run("encerramento e custo denormalizados", func(t *testing.T) {
		l := porID(t, osForno.Id)

		if l.TipoDefeito == nil || *l.TipoDefeito != repository.TipoDefeitoCorretiva {
			t.Errorf("tipoDefeito = %v, esperado Corretiva", l.TipoDefeito)
		}
		if l.DefeitoConstatado == nil || *l.DefeitoConstatado != "Resistência queimada" {
			t.Errorf("defeitoConstatado = %v", l.DefeitoConstatado)
		}
		if l.CausaRaiz == nil || l.Solucao == nil {
			t.Errorf("causaRaiz/solucao vazios: %v / %v", l.CausaRaiz, l.Solucao)
		}
		if l.EncerradoPorNome == nil || *l.EncerradoPorNome != "Eder" {
			t.Errorf("encerradoPorNome = %v, esperado Eder", l.EncerradoPorNome)
		}
		if !l.DataFim.Valid {
			t.Error("dataFim devia estar preenchida")
		}
		if !l.CustoHoraTecnico.Valid || l.CustoHoraTecnico.Float64 != 150.50 {
			t.Errorf("custoHoraTecnico = %+v, esperado 150.50", l.CustoHoraTecnico)
		}
		if !l.CustoManutencao.Valid || l.CustoManutencao.Float64 != 320.25 {
			t.Errorf("custoManutencao = %+v, esperado 320.25", l.CustoManutencao)
		}
		if l.LancadoPorNome == nil || *l.LancadoPorNome != "Ana" {
			t.Errorf("lancadoPorNome = %v, esperado Ana", l.LancadoPorNome)
		}
		if !l.LancadoEm.Valid {
			t.Error("lancadoEm devia estar preenchida")
		}
	})

	// As quatro grandezas numéricas são pgtype.Float8 justamente por isto: as
	// quatro são NULL em algum estado legítimo, e um float64 cru quebraria o
	// Scan no primeiro. Ver a nota no sqlc.yaml.
	t.Run("nulo é estado legítimo em horas e custo", func(t *testing.T) {
		aberta := porID(t, osSerra.Id) // nem encerrada: sem horas, sem custo
		if aberta.HorasTrabalhadas.Valid || aberta.HorasParada.Valid {
			t.Errorf("OS não encerrada não tem horas: %+v / %+v", aberta.HorasTrabalhadas, aberta.HorasParada)
		}
		if aberta.CustoManutencao.Valid || aberta.CustoHoraTecnico.Valid {
			t.Errorf("OS sem custo lançado: %+v / %+v", aberta.CustoManutencao, aberta.CustoHoraTecnico)
		}
		if aberta.TipoDefeito != nil || aberta.DataFim.Valid || aberta.EncerradoPorNome != nil {
			t.Error("OS não encerrada não tem encerramento")
		}

		semCusto := porID(t, osBalanca.Id) // encerrada: tem horas, não tem custo
		if !semCusto.HorasTrabalhadas.Valid {
			t.Error("OS encerrada tem horas trabalhadas")
		}
		if semCusto.CustoManutencao.Valid || semCusto.LancadoPorNome != nil {
			t.Error("custo ainda não lançado devia vir nulo")
		}

		// Reparo não cobra hora técnica (ck_custo_por_tipo), mas cobra
		// manutenção: os dois campos são independentes.
		reparo := porID(t, osReparo.Id)
		if reparo.CustoHoraTecnico.Valid {
			t.Errorf("reparo não tem hora técnica: %+v", reparo.CustoHoraTecnico)
		}
		if !reparo.CustoManutencao.Valid || reparo.CustoManutencao.Float64 != 18.90 {
			t.Errorf("custoManutencao do reparo = %+v, esperado 18.90", reparo.CustoManutencao)
		}
	})

	// REGRA DE NEGÓCIO (docs/modelagem seção 4 + migration 000002): parada
	// corre desde a SOLICITAÇÃO, sem descontar pausa; trabalhadas correm desde
	// iniciada_em, descontando pausa. Só conta parada quem parou.
	t.Run("os dois relógios", func(t *testing.T) {
		forno := porID(t, osForno.Id)
		// Iniciada 5h atrás, 1h de pausa no meio -> ~4h trabalhadas.
		if !forno.HorasTrabalhadas.Valid {
			t.Fatal("horasTrabalhadas nula numa OS encerrada")
		}
		if h := forno.HorasTrabalhadas.Float64; h < 3.9 || h > 4.1 {
			t.Errorf("horasTrabalhadas = %v, esperado ~4 (5h de OS menos 1h de pausa)", h)
		}
		// Solicitada 8h atrás. Parada corre desde AÍ (não desde a abertura da
		// OS, 5h atrás) e não desconta a pausa: ~8h, não ~5h nem ~4h. Este é
		// o número que a migration 000002 mudou.
		if !forno.HorasParada.Valid {
			t.Fatal("horasParada nula numa OS que afeta produção")
		}
		if h := forno.HorasParada.Float64; h < 7.9 || h > 8.1 {
			t.Errorf("horasParada = %v, esperado ~8 (desde a solicitação, sem descontar pausa)", h)
		}

		// O reparo não teve "Afeta Produção" marcado: a máquina seguiu
		// operando e não há parada para medir. O front exibe "Não se aplica"
		// -- que é diferente de zero, e é por isso que a coluna é nullable.
		reparo := porID(t, osReparo.Id)
		if reparo.AfetaProducao {
			t.Fatal("o reparo não deveria afetar produção")
		}
		if reparo.HorasParada.Valid {
			t.Errorf("horasParada = %+v, esperado nulo (não afeta produção)", reparo.HorasParada)
		}
		if !reparo.HorasTrabalhadas.Valid {
			t.Error("horasTrabalhadas existe mesmo sem afetar produção")
		}
	})

	t.Run("pausas vêm em lote, sem N+1", func(t *testing.T) {
		linhas := listar(t, repository.ListarOrdensServicoParams{})
		todosIds := make([]int64, 0, len(linhas))
		for _, l := range linhas {
			todosIds = append(todosIds, l.ID)
		}

		pausas, err := repo.ObterPausasDasOrdensServico(ctx, todosIds)
		if err != nil {
			t.Fatalf("erro ao buscar pausas: %v", err)
		}
		if len(pausas) != 2 {
			t.Fatalf("esperava 2 pausas (Forno fechada, Serra aberta), veio %d", len(pausas))
		}

		porOS := map[int64][]repository.OsPausa{}
		for _, p := range pausas {
			porOS[p.OrdemServicoID] = append(porOS[p.OrdemServicoID], p)
		}

		fechada := porOS[osForno.Id]
		if len(fechada) != 1 || !fechada[0].RetomadaEm.Valid {
			t.Errorf("pausa do Forno devia estar fechada: %+v", fechada)
		}
		if fechada[0].Motivo != "aguardando peça" || fechada[0].StatusAnterior != repository.StatusOsEmAndamento {
			t.Errorf("pausa do Forno: %+v", fechada[0])
		}

		// pausaAtual é a de retomada_em nula -- uq_pausa_aberta garante no
		// máximo uma por OS.
		aberta := porOS[osSerra.Id]
		if len(aberta) != 1 || aberta[0].RetomadaEm.Valid {
			t.Errorf("pausa da Serra devia estar aberta: %+v", aberta)
		}
		if aberta[0].Motivo != "peça em falta no estoque" {
			t.Errorf("motivo da pausa atual = %q", aberta[0].Motivo)
		}

		// A listagem NÃO duplica a OS por pausa -- é o motivo de as pausas
		// virem numa query separada em vez de um JOIN.
		if len(ids(linhas)) != 4 {
			t.Errorf("a listagem devia ter 4 OS, veio %d", len(linhas))
		}
	})

	// ------------------------------------------------------------------
	// Service. O escopo e os filtros já estão trancados nos subtestes da
	// query acima -- aqui só o que o service acrescenta: traduzir perfil em
	// escopo, agrupar as pausas na OS certa e nunca devolver nil.
	// ------------------------------------------------------------------
	svcOS := NewRepoOrdemServico(pool)
	listarPeloService := func(t *testing.T, ator model.Usuario, perfil string, filtros FiltrosOrdemServico) []model.OrdemServico {
		t.Helper()
		ordens, err := svcOS.ListarOrdensServico(ctx, tenantID, ator.Id, perfil, filtros)
		if err != nil {
			t.Fatalf("erro ao listar pelo service: %v", err)
		}
		return ordens
	}

	// O escopo vem do PERFIL, não do filtro: é escopoDe que decide entre
	// "não filtra" (administrador, que não tem linha em usuario_escopo) e o
	// usuario.id. Passar o id do administrador como escopo devolveria zero
	// justamente para quem enxerga tudo.
	t.Run("service traduz perfil em escopo", func(t *testing.T) {
		if n := len(listarPeloService(t, admin, "administrador", FiltrosOrdemServico{})); n != 4 {
			t.Errorf("administrador viu %d OS, esperado 4", n)
		}
		ordens := listarPeloService(t, gestorParcial, "gestor", FiltrosOrdemServico{})
		if len(ordens) != 1 || ordens[0].Id != osSerra.Id {
			t.Errorf("gestor parcial viu %d OS, esperado só a Serra", len(ordens))
		}
	})

	// O front tipa OrdemServico[] -- `null` quebraria o .map da tela, e a aba
	// vazia é o estado normal de um gestor sem OS no escopo.
	t.Run("lista vazia é slice, não nil", func(t *testing.T) {
		naoExiste := int64(-1)
		ordens := listarPeloService(t, admin, "administrador", FiltrosOrdemServico{TecnicoId: &naoExiste})
		if ordens == nil {
			t.Fatal("lista vazia veio nil")
		}
		if len(ordens) != 0 {
			t.Errorf("esperava lista vazia, veio %d", len(ordens))
		}
	})

	// A montagem em lote busca as pausas de todas as OS numa query só e
	// distribui por id -- um erro de agrupamento aqui daria a pausa de uma OS
	// para outra, e o Gestor leria "aguardando peça" no card errado.
	t.Run("pausas vão para a OS certa", func(t *testing.T) {
		porId := map[int64]model.OrdemServico{}
		for _, o := range listarPeloService(t, admin, "administrador", FiltrosOrdemServico{}) {
			porId[o.Id] = o
		}

		forno := porId[osForno.Id]
		if len(forno.Pausas) != 1 || forno.Pausas[0].Motivo != "aguardando peça" {
			t.Errorf("pausas do Forno = %+v", forno.Pausas)
		}
		if forno.PausaAtual != nil {
			t.Errorf("o Forno foi retomado e encerrado: pausaAtual devia ser nil, veio %+v", forno.PausaAtual)
		}

		serra := porId[osSerra.Id]
		if serra.PausaAtual == nil || serra.PausaAtual.Motivo != "peça em falta no estoque" {
			t.Errorf("pausaAtual da Serra = %+v", serra.PausaAtual)
		}

		for _, id := range []int64{osBalanca.Id, osReparo.Id} {
			if len(porId[id].Pausas) != 0 {
				t.Errorf("OS %d não tem pausa, veio %+v", id, porId[id].Pausas)
			}
		}
	})

	// Spot check de ponta a ponta: query -> model -> resposta, com os blocos
	// opcionais nascendo só onde a linha existe.
	t.Run("service monta a OS inteira", func(t *testing.T) {
		sim := true
		ordens := listarPeloService(t, admin, "administrador", FiltrosOrdemServico{Finalizada: &sim})
		var forno model.OrdemServico
		for _, o := range ordens {
			if o.Id == osForno.Id {
				forno = o
			}
		}
		if forno.Id == 0 {
			t.Fatal("Forno não veio em ?finalizada=true")
		}

		if forno.Custo == nil || forno.Custo.CustoTotal != 470.75 {
			t.Errorf("custo = %+v, esperado total 470.75", forno.Custo)
		}
		if forno.Encerramento == nil || forno.Encerramento.EncerradoPorNome != "Eder" {
			t.Errorf("encerramento = %+v", forno.Encerramento)
		}
		if forno.TipoDefeito == nil || *forno.TipoDefeito != "Corretiva" {
			t.Errorf("tipoDefeito = %v", forno.TipoDefeito)
		}
		if forno.HorasParada == nil || forno.HorasTrabalhadas == nil {
			t.Errorf("horas = %v / %v", forno.HorasParada, forno.HorasTrabalhadas)
		}
		if forno.TecnicoNome == nil || *forno.TecnicoNome != "Eder" {
			t.Errorf("tecnicoNome = %v", forno.TecnicoNome)
		}
		if forno.DataAbertura == nil || forno.DataSolicitacao == nil || forno.DataFim == nil {
			t.Error("as três datas deviam estar preenchidas numa OS encerrada")
		}
	})

	// area_tecnico é LEFT JOIN de propósito: fk_os_tecnico não checa perfil, e
	// AtualizarUsuario zera area_tecnico_id ao tirar alguém do perfil técnico
	// (é a única forma de a coluna ficar nula -- ck_usuario_area_tecnico
	// impede zerar mantendo o perfil). Com INNER, promover a gestor um técnico
	// com OS aberta apagaria essas OS da listagem inteira, calado -- e é
	// justamente o Gestor quem olha a listagem.
	//
	// Por último de propósito: promover tecnicoB troca o perfil e o escopo
	// dele, e os subtestes acima o usam como técnico.
	t.Run("técnico promovido a gestor não some com a OS dele", func(t *testing.T) {
		if _, err := svcUsuario.AtualizarUsuario(ctx, tecnicoB.Id, model.AtualizarUsuarioPayload{
			Nome: "Ivo", Email: "ivo@os.com", Perfil: "gestor",
			LojasIds: []int64{lojaB}, AcessoTotalSetores: true,
		}, tenantID); err != nil {
			t.Fatalf("erro ao promover o técnico: %v", err)
		}

		linhas := listar(t, repository.ListarOrdensServicoParams{TecnicoID: &tecnicoB.Id})
		if len(linhas) != 1 {
			t.Fatalf("a OS sumiu da listagem: veio %d, esperado 1", len(linhas))
		}
		if linhas[0].TecnicoArea != nil {
			t.Errorf("tecnicoArea = %v, esperado nil", linhas[0].TecnicoArea)
		}
		if linhas[0].TecnicoNome != "Ivo" {
			t.Errorf("tecnicoNome = %q, esperado Ivo", linhas[0].TecnicoNome)
		}
	})
}
