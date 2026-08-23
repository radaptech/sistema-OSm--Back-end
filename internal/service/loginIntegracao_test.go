package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// Default: a porta publicada no host pelo docker-compose (5431 -> 5432 do
// container). Sobrescreva com TEST_DB_DSN pra rodar de dentro da rede docker
// (host `postgres`) ou em CI.
const dsnPadraoTeste = "postgres://postgres:postgres@localhost:5431/postgres?sslmode=disable"

// bancoDeTeste cria um banco descartável, aplica as migrations nele e devolve
// o pool. Pula o teste inteiro se não houver Postgres alcançável.
func bancoDeTeste(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = dsnPadraoTeste
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err == nil {
		err = admin.Ping(ctx)
	}
	if err != nil {
		t.Skipf("Postgres indisponível em %s (%v) -- suba o compose ou exporte TEST_DB_DSN", dsn, err)
	}
	defer admin.Close()

	// t.Name() no nome: dois testes no mesmo binário compartilham o pid e um
	// dropava o banco do outro.
	nome := strings.ToLower(fmt.Sprintf("teste_%s_%d", t.Name(), os.Getpid()))
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+nome); err != nil {
		t.Fatalf("erro ao limpar banco de teste: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+nome); err != nil {
		t.Fatalf("erro ao criar banco de teste: %v", err)
	}

	pool, err := pgxpool.New(ctx, strings.Replace(dsn, "/postgres?", "/"+nome+"?", 1))
	if err != nil {
		t.Fatalf("erro ao conectar no banco de teste: %v", err)
	}

	// Registrado antes do cleanup do migrate: t.Cleanup roda em LIFO, então
	// o migrate devolve a conexão dele ao pool primeiro. Fora dessa ordem,
	// pool.Close() espera para sempre por uma conexão que ninguém solta.
	t.Cleanup(func() {
		pool.Close()
		limpeza, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return
		}
		defer limpeza.Close()
		limpeza.Exec(ctx, "DROP DATABASE IF EXISTS "+nome)
	})

	sqlDB := stdlib.OpenDBFromPool(pool)
	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	if err != nil {
		t.Fatalf("erro ao iniciar driver de migração: %v", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://../../database/migrate", "postgres", driver)
	if err != nil {
		t.Fatalf("erro ao instanciar migração: %v", err)
	}
	t.Cleanup(func() { m.Close() })

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("erro ao aplicar migrações: %v", err)
	}

	return pool
}

func TestLogin(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-nao-usar-em-producao")

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoUsuario(pool)

	// Tenant de teste: 1 empresa, 2 lojas, 2 setores na Loja A + 1 na Loja B
	// (o setor da outra loja é o que prova a distribuição por loja), 1 área.
	var tenantID, lojaA, lojaB, setorA, setorB, setorC int64
	linha := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('teste', 'Empresa Teste') RETURNING id`)
	if err := linha.Scan(&tenantID); err != nil {
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

	area := "Elétrica"
	senha := "senha-forte-123"
	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) model.Usuario {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, senha
		u, err := svc.CadastrarUsuario(ctx, p, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
		return u
	}

	admin := cadastrar("Ana", "ana@teste.com", "administrador", model.NovoUsuarioPayload{})
	solicitante := cadastrar("Bruno", "bruno@teste.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
	})
	// Setores das duas lojas na mesma lista plana: cada um tem que cair no
	// escopo da sua própria loja.
	gestor := cadastrar("Carla", "carla@teste.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, SetoresIds: []int64{setorA, setorB, setorC},
	})
	gestorTotal := cadastrar("Dora", "dora@teste.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, AcessoTotalSetores: true,
	})
	tecnico := cadastrar("Eder", "eder@teste.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, Area: &area,
	})

	login := func(email, perfil, senha string) (string, model.SessaoUsuario, error) {
		return svc.Login(ctx, model.Login{Email: email, Perfil: perfil, Senha: senha}, tenantID)
	}

	t.Run("administrador não tem escopo nenhum", func(t *testing.T) {
		token, sessao, err := login("ana@teste.com", "administrador", senha)
		if err != nil {
			t.Fatalf("login falhou: %v", err)
		}
		if token == "" {
			t.Fatal("token vazio")
		}
		if sessao.Id != admin.Id || sessao.Nome != "Ana" {
			t.Fatalf("sessão errada: %+v", sessao)
		}
		if sessao.LojaId != nil || sessao.SetorId != nil || sessao.TecnicoId != nil || sessao.EscoposGestor != nil {
			t.Fatalf("administrador devia ter tudo nulo: %+v", sessao)
		}
	})

	t.Run("solicitante traz loja, setor e nome do setor", func(t *testing.T) {
		_, sessao, err := login("bruno@teste.com", "solicitante", senha)
		if err != nil {
			t.Fatalf("login falhou: %v", err)
		}
		if sessao.Id != solicitante.Id {
			t.Fatalf("id errado: %d", sessao.Id)
		}
		if sessao.LojaId == nil || *sessao.LojaId != lojaA {
			t.Fatalf("lojaId errado: %v", sessao.LojaId)
		}
		if sessao.SetorId == nil || *sessao.SetorId != setorA {
			t.Fatalf("setorId errado: %v", sessao.SetorId)
		}
		if sessao.SetorNome == nil || *sessao.SetorNome != "Padaria" {
			t.Fatalf("setorNome errado: %v", sessao.SetorNome)
		}
		if sessao.EscoposGestor != nil || sessao.TecnicoId != nil {
			t.Fatalf("solicitante não tem escoposGestor nem tecnicoId: %+v", sessao)
		}
	})

	t.Run("gestor recebe cada setor no escopo da propria loja", func(t *testing.T) {
		_, sessao, err := login("carla@teste.com", "gestor", senha)
		if err != nil {
			t.Fatalf("login falhou: %v", err)
		}
		if sessao.Id != gestor.Id {
			t.Fatalf("id errado: %d", sessao.Id)
		}
		if len(sessao.EscoposGestor) != 2 {
			t.Fatalf("esperava 2 escopos, veio %d: %+v", len(sessao.EscoposGestor), sessao.EscoposGestor)
		}

		esperado := map[int64][]int64{lojaA: {setorA, setorB}, lojaB: {setorC}}
		for _, e := range sessao.EscoposGestor {
			if e.SetoresIds.AcessoTotal {
				t.Fatalf("loja %d não é acesso total", e.LojaId)
			}
			ids := slices.Clone(e.SetoresIds.Ids)
			slices.Sort(ids)
			if !slices.Equal(ids, esperado[e.LojaId]) {
				t.Fatalf("loja %d: setores %v, esperava %v — setor vazou para a loja errada",
					e.LojaId, ids, esperado[e.LojaId])
			}
		}
		if sessao.LojaId != nil {
			t.Fatal("gestor não usa lojaId")
		}
	})

	t.Run("cadastro recusa setor fora das lojas selecionadas", func(t *testing.T) {
		// setorC é da Loja B; o payload só marca a Loja A.
		_, err := svc.CadastrarUsuario(ctx, model.NovoUsuarioPayload{
			Nome: "Fabio", Email: "fabio@teste.com", Perfil: "gestor", Senha: senha,
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorC},
		}, tenantID)
		if err == nil {
			t.Fatal("esperava recusa do setor de outra loja")
		}
	})

	t.Run("cadastro recusa loja sem nenhum setor", func(t *testing.T) {
		// Loja B marcada, mas só setores da Loja A na lista.
		_, err := svc.CadastrarUsuario(ctx, model.NovoUsuarioPayload{
			Nome: "Gina", Email: "gina@teste.com", Perfil: "gestor", Senha: senha,
			LojasIds: []int64{lojaA, lojaB}, SetoresIds: []int64{setorA},
		}, tenantID)
		if err == nil {
			t.Fatal("esperava recusa: Loja B ficaria com escopo sem setor nenhum")
		}
	})

	t.Run("gestor com acesso total serializa setoresIds como todos", func(t *testing.T) {
		_, sessao, err := login("dora@teste.com", "gestor", senha)
		if err != nil {
			t.Fatalf("login falhou: %v", err)
		}
		if sessao.Id != gestorTotal.Id || len(sessao.EscoposGestor) != 1 {
			t.Fatalf("sessão errada: %+v", sessao)
		}
		json, err := sessao.EscoposGestor[0].SetoresIds.MarshalJSON()
		if err != nil || string(json) != `"todos"` {
			t.Fatalf("esperava \"todos\", veio %s (%v)", json, err)
		}
	})

	t.Run("tecnico traz tecnicoId igual ao proprio id", func(t *testing.T) {
		_, sessao, err := login("eder@teste.com", "tecnico", senha)
		if err != nil {
			t.Fatalf("login falhou: %v", err)
		}
		if sessao.TecnicoId == nil || *sessao.TecnicoId != tecnico.Id {
			t.Fatalf("tecnicoId errado: %v (esperava %d)", sessao.TecnicoId, tecnico.Id)
		}
		if sessao.EscoposGestor != nil || sessao.LojaId != nil {
			t.Fatalf("técnico não usa escoposGestor nem lojaId: %+v", sessao)
		}
	})

	t.Run("credenciais invalidas nao vazam qual campo errou", func(t *testing.T) {
		casos := []struct {
			nome, email, perfil, senha string
		}{
			{"senha errada", "bruno@teste.com", "solicitante", "senha-errada-123"},
			{"email inexistente", "ninguem@teste.com", "solicitante", senha},
			{"perfil trocado", "bruno@teste.com", "administrador", senha},
		}
		for _, c := range casos {
			_, _, err := login(c.email, c.perfil, c.senha)
			if !errors.Is(err, helper.ErrCredenciaisInvalidas) {
				t.Fatalf("%s: esperava ErrCredenciaisInvalidas, veio %v", c.nome, err)
			}
		}
	})

	t.Run("usuario desativado nao loga", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE usuario SET ativo = false WHERE id = $1`, tecnico.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}
		if _, _, err := login("eder@teste.com", "tecnico", senha); !errors.Is(err, helper.ErrCredenciaisInvalidas) {
			t.Fatalf("esperava ErrCredenciaisInvalidas, veio %v", err)
		}
	})

	t.Run("login registra ultimo acesso", func(t *testing.T) {
		var antes *string
		if err := pool.QueryRow(ctx, `SELECT ultimo_acesso::text FROM usuario WHERE id = $1`, admin.Id).Scan(&antes); err != nil {
			t.Fatalf("erro ao ler ultimo_acesso: %v", err)
		}
		if antes == nil {
			t.Fatal("ultimo_acesso continuou nulo depois do login")
		}
	})
}

// TestListarUsuarios cobre o que o teste de unidade não alcança: o WHERE.
// Em especial o filtro por loja, que é EXISTS sobre usuario_escopo -- um JOIN
// no lugar dele traria o usuário com N escopos N vezes na mesma página.
func TestListarUsuarios(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoUsuario(pool)

	var tenantID, lojaA, lojaB, setorA int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('listar', 'Empresa Listar') RETURNING id`).Scan(&tenantID); err != nil {
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
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaA).Scan(&setorA); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}

	area := "Elétrica"
	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, "senha-forte-123"
		if _, err := svc.CadastrarUsuario(ctx, p, tenantID); err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
	}

	cadastrar("Ana", "ana@listar.com", "administrador", model.NovoUsuarioPayload{})
	cadastrar("Bruno", "bruno@listar.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
	})
	cadastrar("Carla", "carla@listar.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaB}, AcessoTotalSetores: true,
	})
	// Escopo nas duas lojas: é este que duplicaria com JOIN em vez de EXISTS.
	cadastrar("Dora", "dora@listar.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, Area: &area,
	})

	nomes := func(r model.RespostaPaginada[model.Usuario]) []string {
		out := make([]string, 0, len(r.Dados))
		for _, u := range r.Dados {
			out = append(out, u.Nome)
		}
		return out
	}
	perfilGestor := "gestor"
	busca := "dor"

	casos := []struct {
		nome     string
		perfil   *string
		busca    *string
		lojaId   *int64
		esperado []string // ORDER BY nome
		total    int64
	}{
		{nome: "sem filtro devolve o tenant inteiro", esperado: []string{"Ana", "Bruno", "Carla", "Dora"}, total: 4},
		{nome: "loja A: só quem tem escopo nela, sem o administrador", lojaId: &lojaA, esperado: []string{"Bruno", "Dora"}, total: 2},
		{nome: "loja B", lojaId: &lojaB, esperado: []string{"Carla", "Dora"}, total: 2},
		{nome: "perfil combina com loja", perfil: &perfilGestor, lojaId: &lojaB, esperado: []string{"Carla"}, total: 1},
		{nome: "perfil que não tem ninguém na loja A", perfil: &perfilGestor, lojaId: &lojaA, esperado: []string{}, total: 0},
		{nome: "busca combina com loja", busca: &busca, lojaId: &lojaA, esperado: []string{"Dora"}, total: 1},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			resp, err := svc.ListarUsuarios(ctx, tenantID, 1, c.perfil, c.busca, c.lojaId)
			if err != nil {
				t.Fatalf("erro ao listar: %v", err)
			}
			if got := nomes(resp); !slices.Equal(got, c.esperado) {
				t.Errorf("nomes = %v, esperado %v", got, c.esperado)
			}
			// total é query própria (ContarUsuarios): se divergir da página, a
			// paginação do front mostra páginas que não existem.
			if resp.Total != c.total {
				t.Errorf("total = %d, esperado %d", resp.Total, c.total)
			}
			if resp.Pagina != 1 {
				t.Errorf("pagina = %d, esperado 1", resp.Pagina)
			}
		})
	}

	t.Run("página além do fim é lista vazia, não erro", func(t *testing.T) {
		resp, err := svc.ListarUsuarios(ctx, tenantID, 99, nil, nil, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(resp.Dados) != 0 || resp.Total != 4 || resp.TotalPaginas != 1 {
			t.Errorf("esperado página vazia com total 4 e 1 página: %+v", resp)
		}
	})

	t.Run("escopo volta achatado no formato do front", func(t *testing.T) {
		resp, err := svc.ListarUsuarios(ctx, tenantID, 1, nil, nil, &lojaA)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		bruno, dora := resp.Dados[0], resp.Dados[1]
		if !slices.Equal(bruno.LojasIds, []int64{lojaA}) || !slices.Equal(bruno.SetoresIds, []int64{setorA}) || bruno.AcessoTotalSetores {
			t.Errorf("solicitante: %+v", bruno)
		}
		// Técnico: escopo nas duas lojas mesmo o filtro sendo só a Loja A --
		// o filtro escolhe QUEM aparece, não recorta o escopo de quem apareceu.
		if !slices.Equal(dora.LojasIds, []int64{lojaA, lojaB}) || len(dora.SetoresIds) != 0 || !dora.AcessoTotalSetores {
			t.Errorf("tecnico: %+v", dora)
		}
	})
}

// Atualizar e desativar mexem em transação e esbarram nos gatilhos deferred de
// administrador-sem-escopo, que só disparam no commit -- nada disso aparece
// sem banco de verdade.
func TestAtualizarEDesativarUsuario(t *testing.T) {

	// Os casos de senha conferem o resultado via Login, que assina um JWT.
	t.Setenv("JWT_SECRET", "segredo-de-teste-nao-usar-em-producao")

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoUsuario(pool)

	var tenantID, lojaA, lojaB, setorA, setorB, setorC int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('editar', 'Empresa Editar') RETURNING id`).Scan(&tenantID); err != nil {
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

	area := "Elétrica"
	const senhaOriginal = "senha-forte-123"
	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) model.Usuario {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, senhaOriginal
		u, err := svc.CadastrarUsuario(ctx, p, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
		return u
	}
	// Lê o escopo direto do banco: a resposta do service é o que ele achou que
	// gravou, e é justamente isso que o teste não pode acreditar.
	escopoGravado := func(usuarioID int64) []repository.ObterEscopoSessaoPorUsuarioRow {
		t.Helper()
		linhas, err := repository.New(pool).ObterEscopoSessaoPorUsuario(ctx, usuarioID)
		if err != nil {
			t.Fatalf("erro ao ler escopo: %v", err)
		}
		return linhas
	}

	admin := cadastrar("Ana", "ana@editar.com", "administrador", model.NovoUsuarioPayload{})

	t.Run("gestor troca de loja: escopo é substituído, não somado", func(t *testing.T) {
		u := cadastrar("Bia", "bia@editar.com", "gestor", model.NovoUsuarioPayload{
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA, setorB},
		})

		atualizado, err := svc.AtualizarUsuario(ctx, u.Id, model.AtualizarUsuarioPayload{
			Nome: "Bia Souza", Email: "bia@editar.com", Perfil: "gestor",
			LojasIds: []int64{lojaB}, SetoresIds: []int64{setorC},
		}, tenantID)
		if err != nil {
			t.Fatalf("erro ao atualizar: %v", err)
		}
		if atualizado.Nome != "Bia Souza" {
			t.Errorf("nome não atualizou: %q", atualizado.Nome)
		}

		escopos := escopoGravado(u.Id)
		if len(escopos) != 1 || escopos[0].LojaID != lojaB || !slices.Equal(escopos[0].SetoresIds, []int64{setorC}) {
			t.Fatalf("escopo devia ter só a Loja B com o setor C: %+v", escopos)
		}
	})

	t.Run("senha omitida mantém o hash; senha nova passa a valer", func(t *testing.T) {
		u := cadastrar("Caio", "caio@editar.com", "solicitante", model.NovoUsuarioPayload{
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
		})
		base := model.AtualizarUsuarioPayload{
			Nome: "Caio", Email: "caio@editar.com", Perfil: "solicitante",
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
		}

		if _, err := svc.AtualizarUsuario(ctx, u.Id, base, tenantID); err != nil {
			t.Fatalf("erro ao atualizar sem senha: %v", err)
		}
		if _, _, err := svc.Login(ctx, model.Login{Email: "caio@editar.com", Perfil: "solicitante", Senha: senhaOriginal}, tenantID); err != nil {
			t.Fatalf("senha antiga devia continuar valendo: %v", err)
		}

		nova := "outra-senha-forte-456"
		comSenha := base
		comSenha.Senha = &nova
		if _, err := svc.AtualizarUsuario(ctx, u.Id, comSenha, tenantID); err != nil {
			t.Fatalf("erro ao atualizar com senha: %v", err)
		}
		if _, _, err := svc.Login(ctx, model.Login{Email: "caio@editar.com", Perfil: "solicitante", Senha: nova}, tenantID); err != nil {
			t.Fatalf("senha nova devia valer: %v", err)
		}
		if _, _, err := svc.Login(ctx, model.Login{Email: "caio@editar.com", Perfil: "solicitante", Senha: senhaOriginal}, tenantID); !errors.Is(err, helper.ErrCredenciaisInvalidas) {
			t.Fatalf("senha antiga devia ter morrido, erro = %v", err)
		}
	})

	t.Run("técnico vira gestor: área é zerada e o escopo passa a ter setor", func(t *testing.T) {
		u := cadastrar("Davi", "davi@editar.com", "tecnico", model.NovoUsuarioPayload{
			LojasIds: []int64{lojaA, lojaB}, Area: &area,
		})

		if _, err := svc.AtualizarUsuario(ctx, u.Id, model.AtualizarUsuarioPayload{
			Nome: "Davi", Email: "davi@editar.com", Perfil: "gestor",
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
		}, tenantID); err != nil {
			// ck_usuario_area_tecnico proíbe area_tecnico_id fora do perfil técnico.
			t.Fatalf("erro ao trocar técnico por gestor: %v", err)
		}

		var areaID *int16
		if err := pool.QueryRow(ctx, `SELECT area_tecnico_id FROM usuario WHERE id = $1`, u.Id).Scan(&areaID); err != nil {
			t.Fatalf("erro ao ler área: %v", err)
		}
		if areaID != nil {
			t.Errorf("área devia ter sido zerada: %v", *areaID)
		}
	})

	t.Run("gestor vira administrador: escopo antigo tem que sumir junto", func(t *testing.T) {
		u := cadastrar("Elis", "elis@editar.com", "gestor", model.NovoUsuarioPayload{
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
		})

		// trg_usuario_admin_sem_escopo é DEFERRABLE: se o service não apagasse
		// o escopo, isto estouraria no commit, não antes.
		if _, err := svc.AtualizarUsuario(ctx, u.Id, model.AtualizarUsuarioPayload{
			Nome: "Elis", Email: "elis@editar.com", Perfil: "administrador",
		}, tenantID); err != nil {
			t.Fatalf("erro ao promover a administrador: %v", err)
		}
		if escopos := escopoGravado(u.Id); len(escopos) != 0 {
			t.Fatalf("administrador não pode ter escopo: %+v", escopos)
		}
	})

	t.Run("escopo inválido não grava nada (transação inteira volta)", func(t *testing.T) {
		u := cadastrar("Fabio", "fabio@editar.com", "gestor", model.NovoUsuarioPayload{
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
		})

		// setorC é da Loja B: o service recusa antes de gravar.
		_, err := svc.AtualizarUsuario(ctx, u.Id, model.AtualizarUsuarioPayload{
			Nome: "Fabio Alterado", Email: "fabio@editar.com", Perfil: "gestor",
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorC},
		}, tenantID)
		if !errors.Is(err, helper.ErrValidacao) {
			t.Fatalf("esperado ErrValidacao, veio %v", err)
		}

		var nome string
		if err := pool.QueryRow(ctx, `SELECT nome FROM usuario WHERE id = $1`, u.Id).Scan(&nome); err != nil {
			t.Fatalf("erro ao reler usuário: %v", err)
		}
		if nome != "Fabio" {
			t.Errorf("nome não podia ter mudado com escopo inválido: %q", nome)
		}
		if escopos := escopoGravado(u.Id); len(escopos) != 1 || escopos[0].LojaID != lojaA {
			t.Errorf("escopo antigo devia estar intacto: %+v", escopos)
		}
	})

	t.Run("id inexistente e id de outro tenant são não encontrado", func(t *testing.T) {
		payload := model.AtualizarUsuarioPayload{
			Nome: "Ninguém", Email: "ninguem@editar.com", Perfil: "administrador",
		}
		if _, err := svc.AtualizarUsuario(ctx, 999999, payload, tenantID); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("id inexistente: esperado ErrNaoEncontrado, veio %v", err)
		}
		if _, err := svc.AtualizarUsuario(ctx, admin.Id, payload, tenantID+1000); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("outro tenant: esperado ErrNaoEncontrado, veio %v", err)
		}
	})

	t.Run("desativar: some do login e da listagem, e é idempotente", func(t *testing.T) {
		u := cadastrar("Gil", "gil@editar.com", "solicitante", model.NovoUsuarioPayload{
			LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
		})

		if err := svc.DesativarUsuario(ctx, u.Id, tenantID, admin.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}
		if _, _, err := svc.Login(ctx, model.Login{Email: "gil@editar.com", Perfil: "solicitante", Senha: senhaOriginal}, tenantID); !errors.Is(err, helper.ErrCredenciaisInvalidas) {
			t.Errorf("desativado não pode logar, erro = %v", err)
		}
		if _, err := svc.ObterSessao(ctx, u.Id, tenantID); !errors.Is(err, helper.ErrSessaoExpirada) {
			t.Errorf("sessão de desativado devia morrer, erro = %v", err)
		}

		lista, err := svc.ListarUsuarios(ctx, tenantID, 1, nil, nil, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		for _, item := range lista.Dados {
			if item.Id == u.Id {
				t.Errorf("desativado não pode aparecer na listagem: %+v", item)
			}
		}

		// Soft delete: some o cadastro, fica a linha (e o histórico que aponta pra ela).
		var existe bool
		if err := pool.QueryRow(ctx, `SELECT ativo FROM usuario WHERE id = $1`, u.Id).Scan(&existe); err != nil {
			t.Fatalf("linha devia continuar existindo: %v", err)
		}
		if existe {
			t.Error("ativo devia ser false")
		}

		if err := svc.DesativarUsuario(ctx, u.Id, tenantID, admin.Id); err != nil {
			t.Errorf("desativar de novo devia ser idempotente: %v", err)
		}
	})

	t.Run("desativar a si mesmo é recusado", func(t *testing.T) {
		if err := svc.DesativarUsuario(ctx, admin.Id, tenantID, admin.Id); !errors.Is(err, helper.ErrValidacao) {
			t.Fatalf("esperado ErrValidacao, veio %v", err)
		}
		var ativo bool
		if err := pool.QueryRow(ctx, `SELECT ativo FROM usuario WHERE id = $1`, admin.Id).Scan(&ativo); err != nil {
			t.Fatalf("erro ao reler admin: %v", err)
		}
		if !ativo {
			t.Error("o administrador continua ativo")
		}
	})

	t.Run("desativar id inexistente ou de outro tenant é não encontrado", func(t *testing.T) {
		if err := svc.DesativarUsuario(ctx, 999999, tenantID, admin.Id); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("id inexistente: esperado ErrNaoEncontrado, veio %v", err)
		}
		if err := svc.DesativarUsuario(ctx, admin.Id, tenantID+1000, 0); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("outro tenant: esperado ErrNaoEncontrado, veio %v", err)
		}
	})
}

// ObterUsuario alimenta a tela de edição: se o escopo vier errado daqui, o
// formulário abre com as lojas/setores errados e o próximo save grava isso.
func TestObterUsuario(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoUsuario(pool)

	var tenantID, lojaA, lojaB, setorA int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('obter', 'Empresa Obter') RETURNING id`).Scan(&tenantID); err != nil {
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
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaA).Scan(&setorA); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}

	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) model.Usuario {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, "senha-forte-123"
		u, err := svc.CadastrarUsuario(ctx, p, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
		return u
	}

	admin := cadastrar("Ana", "ana@obter.com", "administrador", model.NovoUsuarioPayload{})
	gestor := cadastrar("Bia", "bia@obter.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, AcessoTotalSetores: true,
	})
	solicitante := cadastrar("Caio", "caio@obter.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA}, SetoresIds: []int64{setorA},
	})

	t.Run("devolve o mesmo escopo que o cadastro gravou", func(t *testing.T) {
		u, err := svc.ObterUsuario(ctx, gestor.Id, tenantID)
		if err != nil {
			t.Fatalf("erro ao obter: %v", err)
		}
		if !slices.Equal(u.LojasIds, []int64{lojaA, lojaB}) || len(u.SetoresIds) != 0 || !u.AcessoTotalSetores {
			t.Errorf("escopo do gestor: %+v", u)
		}
		if u.Nome != "Bia" || u.Email != "bia@obter.com" || u.Perfil != "gestor" || !u.Ativo {
			t.Errorf("campos do usuário: %+v", u)
		}
	})

	t.Run("solicitante traz loja e setor", func(t *testing.T) {
		u, err := svc.ObterUsuario(ctx, solicitante.Id, tenantID)
		if err != nil {
			t.Fatalf("erro ao obter: %v", err)
		}
		if !slices.Equal(u.LojasIds, []int64{lojaA}) || !slices.Equal(u.SetoresIds, []int64{setorA}) || u.AcessoTotalSetores {
			t.Errorf("escopo do solicitante: %+v", u)
		}
	})

	t.Run("administrador vem com escopo vazio, não nulo", func(t *testing.T) {
		u, err := svc.ObterUsuario(ctx, admin.Id, tenantID)
		if err != nil {
			t.Fatalf("erro ao obter: %v", err)
		}
		// O front tipa number[]: nil viraria `null` no JSON e quebraria o .map.
		if u.LojasIds == nil || u.SetoresIds == nil || len(u.LojasIds) != 0 || len(u.SetoresIds) != 0 {
			t.Errorf("esperado arrays vazios: %+v", u)
		}
	})

	t.Run("id inexistente e id de outro tenant são não encontrado", func(t *testing.T) {
		if _, err := svc.ObterUsuario(ctx, 999999, tenantID); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("id inexistente: esperado ErrNaoEncontrado, veio %v", err)
		}
		if _, err := svc.ObterUsuario(ctx, gestor.Id, tenantID+1000); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("outro tenant: esperado ErrNaoEncontrado, veio %v", err)
		}
	})

	t.Run("desativado continua legível, com ativo false", func(t *testing.T) {
		if err := svc.DesativarUsuario(ctx, solicitante.Id, tenantID, admin.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}
		u, err := svc.ObterUsuario(ctx, solicitante.Id, tenantID)
		if err != nil {
			t.Fatalf("desativado devia continuar legível: %v", err)
		}
		if u.Ativo {
			t.Error("ativo devia ser false")
		}
	})
}

// Desativar um tenant precisa derrubar quem já está dentro. Antes disto o
// AND ativa só existia no login: token emitido antes seguia valendo até o exp.
func TestSessaoMorreComTenantDesativado(t *testing.T) {

	t.Setenv("JWT_SECRET", "segredo-de-teste-nao-usar-em-producao")

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoUsuario(pool)

	var tenantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('morre', 'Empresa Morre') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}

	admin, err := svc.CadastrarUsuario(ctx, model.NovoUsuarioPayload{
		Nome: "Ana", Email: "ana@morre.com", Perfil: "administrador", Senha: "senha-forte-123",
	}, tenantID)
	if err != nil {
		t.Fatalf("erro ao cadastrar admin: %v", err)
	}

	// Sessão viva enquanto a empresa está ativa.
	if _, err := svc.ObterSessao(ctx, admin.Id, tenantID); err != nil {
		t.Fatalf("sessão devia estar viva: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE empresa SET ativa = false WHERE id = $1`, tenantID); err != nil {
		t.Fatalf("erro ao desativar empresa: %v", err)
	}

	// O usuário continua ativo -- é a empresa que derruba a sessão.
	if _, err := svc.ObterSessao(ctx, admin.Id, tenantID); !errors.Is(err, helper.ErrSessaoExpirada) {
		t.Fatalf("esperado ErrSessaoExpirada, veio %v", err)
	}

	// Login novo já era barrado antes (ObterEmpresaPorSubdominio filtra ativa),
	// mas o TenantMiddleware nem resolve o subdomínio -- aqui só confirmamos
	// que a empresa sumiu da resolução.
	if _, err := repository.New(pool).ObterEmpresaPorSubdominio(ctx, "morre"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("subdomínio de empresa inativa não devia resolver: %v", err)
	}

	// Empresa apagada de vez cai no mesmo lugar, não em 500.
	if _, err := svc.ObterSessao(ctx, admin.Id, tenantID+1000); !errors.Is(err, helper.ErrSessaoExpirada) {
		t.Errorf("tenant inexistente: esperado ErrSessaoExpirada, veio %v", err)
	}
}
