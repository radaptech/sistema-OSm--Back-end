package service

import (
	"context"
	"testing"
	"time"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// O recorte por escopo é do WHERE do servidor, não do cliente: o front manda
// ?setorId=, mas quem impõe é a query. Este teste existe porque a falha aqui é
// silenciosa -- a listagem responde 200 com máquinas demais, e nenhuma tela
// reclama.
func TestEscopoDasListagens(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svcPreventiva := NewRepoPreventiva(pool)

	// Duas lojas, dois setores na Loja A e um na Loja B: é o setor da outra
	// loja que separa "gestor com acesso total à loja" de "vê o tenant".
	var tenantID, lojaA, lojaB, setorA, setorB, setorC int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('escopo', 'Empresa Escopo') RETURNING id`).Scan(&tenantID); err != nil {
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

	// Uma máquina por setor -- cada uma com a preventiva obrigatória, então a
	// listagem de preventivas herda exatamente o mesmo recorte.
	for _, m := range []struct {
		nome  string
		setor int64
	}{{"Forno", setorA}, {"Serra", setorB}, {"Balança", setorC}} {
		_, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
			SetorID: m.setor, Criticidade: "Alta", NumeroPatrimonio: "PAT-" + m.nome, Nome: m.nome,
			Preventivas: []model.PreventivaPayload{{
				Descricao: "Revisão", IntervaloDias: 30,
				ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
			}},
		})
		if err != nil {
			t.Fatalf("erro ao criar máquina %s: %v", m.nome, err)
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

	admin := cadastrar("Ana", "ana@escopo.com", "administrador", model.NovoUsuarioPayload{})
	solicitante := cadastrar("Bruno", "bruno@escopo.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
	})
	gestorParcial := cadastrar("Carla", "carla@escopo.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorB},
	})
	gestorTotal := cadastrar("Dora", "dora@escopo.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, AcessoTotalSetores: true,
	})
	tecnico := cadastrar("Eder", "eder@escopo.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaB}, Area: &area,
	})

	nomes := func(t *testing.T, ms []model.Maquinario) []string {
		t.Helper()
		out := make([]string, 0, len(ms))
		for _, m := range ms {
			out = append(out, m.Nome)
		}
		return out
	}
	iguais := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	casos := []struct {
		nome     string
		usuario  model.Usuario
		perfil   string
		esperado []string
	}{
		// Administrador não tem linha em usuario_escopo: filtrar por escopo
		// devolveria zero justamente para quem enxerga tudo.
		{"administrador vê o tenant inteiro", admin, "administrador", []string{"Balança", "Forno", "Serra"}},
		{"solicitante vê só o próprio setor", solicitante, "solicitante", []string{"Forno"}},
		{"gestor parcial vê só o setor do escopo", gestorParcial, "gestor", []string{"Serra"}},
		{"gestor total vê a loja inteira, não o tenant", gestorTotal, "gestor", []string{"Forno", "Serra"}},
		{"técnico vê as lojas dele", tecnico, "tecnico", []string{"Balança"}},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			lidas, err := svcMaquina.ListarMaquinario(ctx, tenantID, c.usuario.Id, c.perfil, nil, nil)
			if err != nil {
				t.Fatalf("erro ao listar: %v", err)
			}
			if got := nomes(t, lidas); !iguais(got, c.esperado) {
				t.Errorf("máquinas = %v, esperado %v", got, c.esperado)
			}
		})
	}

	// O ponto do exercício: o filtro do cliente estreita o escopo, nunca o
	// amplia. Sem o EXISTS no WHERE, este pedido devolveria a máquina da Loja B.
	t.Run("filtro do cliente não amplia o escopo", func(t *testing.T) {
		lidas, err := svcMaquina.ListarMaquinario(ctx, tenantID, solicitante.Id, "solicitante", &lojaB, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(lidas) != 0 {
			t.Errorf("solicitante alcançou a Loja B pedindo ?lojaId=: %v", nomes(t, lidas))
		}
	})

	t.Run("setor de fora do escopo também não passa", func(t *testing.T) {
		lidas, err := svcMaquina.ListarMaquinario(ctx, tenantID, gestorParcial.Id, "gestor", nil, &setorA)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(lidas) != 0 {
			t.Errorf("gestor alcançou setor fora do escopo: %v", nomes(t, lidas))
		}
	})

	// Preventiva chega na loja/setor pela máquina, então o recorte é o mesmo.
	t.Run("preventivas seguem o mesmo escopo", func(t *testing.T) {
		esperado := map[string]int{"administrador": 3, "solicitante": 1, "gestor": 2}
		for _, c := range []struct {
			usuario model.Usuario
			perfil  string
		}{{admin, "administrador"}, {solicitante, "solicitante"}, {gestorTotal, "gestor"}} {

			lidas, err := svcPreventiva.ListarPreventivas(ctx, tenantID, c.usuario.Id, c.perfil, nil)
			if err != nil {
				t.Fatalf("erro ao listar preventivas: %v", err)
			}
			if len(lidas) != esperado[c.perfil] {
				t.Errorf("%s: %d preventivas, esperado %d", c.perfil, len(lidas), esperado[c.perfil])
			}
		}
	})
}

// GET /tecnicos é projeção sobre `usuario`, com dois pedaços que só o banco
// prova: o `area` vindo do JOIN (o front exibe "Nome — Área" no select) e o
// `lojasIds` do array_agg. Mais os dois EXISTS -- o ?lojaId= do modal e o
// escopo de quem chama.
func TestListarTecnicos(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoUsuario(pool)

	var tenantID, lojaA, lojaB int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('tecnicos', 'Empresa Tecnicos') RETURNING id`).Scan(&tenantID); err != nil {
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
	var setorA int64
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaA).Scan(&setorA); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}

	refrigeracao, eletrica, senha := "Refrigeração", "Elétrica", "senha-forte-123"
	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) model.Usuario {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, senha
		u, err := svc.CadastrarUsuario(ctx, p, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
		return u
	}

	admin := cadastrar("Ana", "ana@tec.com", "administrador", model.NovoUsuarioPayload{})
	// Técnico das duas lojas -- é o que faz o array_agg valer alguma coisa.
	tecnicoAB := cadastrar("Bruno", "bruno@tec.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, Area: &refrigeracao,
	})
	cadastrar("Carlos", "carlos@tec.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaB}, Area: &eletrica,
	})
	// Gestor só da Loja A: não pode enxergar o técnico exclusivo da Loja B.
	gestorA := cadastrar("Dora", "dora@tec.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
	})

	nomes := func(ts []model.Tecnico) []string {
		out := make([]string, 0, len(ts))
		for _, t := range ts {
			out = append(out, t.Nome)
		}
		return out
	}

	t.Run("administrador vê os dois, com área e lojas", func(t *testing.T) {
		lidos, err := svc.ListarTecnicos(ctx, tenantID, admin.Id, "administrador", nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if got := nomes(lidos); len(got) != 2 || got[0] != "Bruno" || got[1] != "Carlos" {
			t.Fatalf("técnicos = %v, esperado [Bruno Carlos]", got)
		}
		// Área é o NOME, não o id: é o que o select exibe ao lado do nome.
		if lidos[0].Area != "Refrigeração" || lidos[1].Area != "Elétrica" {
			t.Errorf("áreas = %q/%q", lidos[0].Area, lidos[1].Area)
		}
		if len(lidos[0].LojasIds) != 2 {
			t.Errorf("lojasIds do Bruno = %v, esperado as duas lojas", lidos[0].LojasIds)
		}
		if len(lidos[1].LojasIds) != 1 || lidos[1].LojasIds[0] != lojaB {
			t.Errorf("lojasIds do Carlos = %v, esperado [%d]", lidos[1].LojasIds, lojaB)
		}
	})

	t.Run("?lojaId= devolve só quem atende a loja", func(t *testing.T) {
		lidos, err := svc.ListarTecnicos(ctx, tenantID, admin.Id, "administrador", &lojaA)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if got := nomes(lidos); len(got) != 1 || got[0] != "Bruno" {
			t.Fatalf("técnicos da Loja A = %v, esperado [Bruno]", got)
		}
	})

	// Sem o segundo EXISTS, o gestor da Loja A leria nome e e-mail do técnico
	// exclusivo da Loja B só chamando a rota sem filtro.
	t.Run("gestor não vê técnico de loja fora do escopo", func(t *testing.T) {
		lidos, err := svc.ListarTecnicos(ctx, tenantID, gestorA.Id, "gestor", nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if got := nomes(lidos); len(got) != 1 || got[0] != "Bruno" {
			t.Fatalf("técnicos do gestor = %v, esperado [Bruno]", got)
		}
	})

	t.Run("gestor pedindo loja fora do escopo recebe vazio", func(t *testing.T) {
		lidos, err := svc.ListarTecnicos(ctx, tenantID, gestorA.Id, "gestor", &lojaB)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(lidos) != 0 {
			t.Fatalf("gestor alcançou a Loja B: %v", nomes(lidos))
		}
	})

	t.Run("técnico desativado some da lista", func(t *testing.T) {
		if err := svc.DesativarUsuario(ctx, tecnicoAB.Id, tenantID, admin.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}
		lidos, err := svc.ListarTecnicos(ctx, tenantID, admin.Id, "administrador", nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if got := nomes(lidos); len(got) != 1 || got[0] != "Carlos" {
			t.Fatalf("técnicos = %v, esperado [Carlos]", got)
		}
	})
}
