package service
import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// A porta publicada no host (5431) está mal mapeada no docker-compose -- o
// Postgres escuta em 5432 dentro do container --, então o default aqui é o IP
// do container na rede do compose. Sobrescreva com TEST_DB_DSN quando o IP
// mudar ou for rodar de dentro da rede docker (host `postgres`).
//
// ponytail: IP fixo; se rodar em CI, exporte TEST_DB_DSN.
const dsnPadraoTeste = "postgres://postgres:postgres@172.20.0.3:5432/postgres?sslmode=disable"

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

	nome := fmt.Sprintf("teste_login_%d", os.Getpid())
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

	// Tenant de teste: 1 empresa, 2 lojas, 2 setores na primeira, 1 área.
	var tenantID, lojaA, lojaB, setorA, setorB int64
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
		dest *int64
	}{{"Padaria", &setorA}, {"Açougue", &setorB}} {
		if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, $3) RETURNING id`, tenantID, lojaA, s.nome).Scan(s.dest); err != nil {
			t.Fatalf("erro ao criar setor: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO area_tecnico (tenant_id, nome) VALUES ($1, 'Elétrica')`, tenantID); err != nil {
		t.Fatalf("erro ao criar área técnica: %v", err)
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
	gestor := cadastrar("Carla", "carla@teste.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaA, lojaB}, SetoresIds: []int64{setorA, setorB},
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

	t.Run("gestor traz um escopo por loja", func(t *testing.T) {
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
		for _, e := range sessao.EscoposGestor {
			if e.SetoresIds.AcessoTotal || len(e.SetoresIds.Ids) != 2 {
				t.Fatalf("escopo da loja %d devia ter 2 setores: %+v", e.LojaId, e.SetoresIds)
			}
		}
		if sessao.LojaId != nil {
			t.Fatal("gestor não usa lojaId")
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
