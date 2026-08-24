package config

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

type Conexao interface {
	Conn(configEnv *VariaveisDeAmbiente) (*pgxpool.Pool, error)
}

type ConnPostgresql struct {
	Pool *pgxpool.Pool
}

func (p *ConnPostgresql) Conn(configEnv *VariaveisDeAmbiente) (*pgxpool.Pool, error) {

	// Formato: postgres://usuario:senha@host:porta/database
	connString := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		configEnv.DB_SERVER,
		configEnv.DB_PORT,
		configEnv.DB_USER,
		configEnv.DB_PASSWORD,
		configEnv.DATABASE,
		configEnv.DB_SSLMODE,
	)

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("erro ao configurar Pool do banco de dados: %v", err)
	}

	// MinConns=0 é o default do pgx: sem conexão nenhuma mantida quente, o pool
	// abre uma nova a cada vez que não sobra conexão ociosa. Direto no Postgres
	// isso é barato -- mas em produção (Supabase Session Pooler, não conexão
	// direta) abrir conexão nova negocia com o pooler antes de chegar no
	// Postgres de verdade, e isso mede até 1.3s a mais por request "fria"
	// (medido em produção, request a request). MinConns mantém um punhado
	// sempre pronto -- 5 é folgado contra o teto real (Supabase: Database >
	// Observability > Peak Connections, 12/60 no pico do dia, CPU/mem/disco
	// todos <50%: sobra espaço, não é o pooler no limite).
	//
	// MaxConns também precisa ser explícito: o default do pgx é
	// max(4, runtime.NumCPU()) -- num container com poucas CPUs visíveis isso
	// pode cair pra 4, menor que o MinConns=5 daqui embaixo, e
	// NewWithConfig falha (MinConns não pode passar de MaxConns).
	config.MinConns = 5
	config.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("erro ao se conectar com o banco de dados, %v", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		return nil, fmt.Errorf("erro ao verificar se o banco de dados esta ativo: %v", err)
	}

	s := pool.Stat()
	log.Printf("conn totais: %d | em uso: %d | ociosas: %d\n", s.TotalConns(), s.AcquiredConns(), s.IdleConns())

	return pool, nil
}

func (p *ConnPostgresql) RunMigrationPostgress(db *pgxpool.Pool) error {

	sqlDB := stdlib.OpenDBFromPool(db)
	driver, err := pgx.WithInstance(sqlDB, &pgx.Config{})
	if err != nil {

		return fmt.Errorf("erro ao iniciar drive da migração, %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance(
		"file://database/migrate",
		"postgres", driver)
	if err != nil {
		return fmt.Errorf("erro ao instanciar migraççao no banco de dados")
	}
	dir, _ := os.Getwd()
	fmt.Println("O programa está rodando na pasta:", dir)
	fmt.Println("Tentando ler migrações de:", dir+"/database/migrate")
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("erro ao aplicar migrações: %w", err)
	}

	if err == migrate.ErrNoChange {
		log.Println("Nenhuma migração nova para aplicar.")
	} else {
		log.Println("Migrações aplicadas com sucesso!")
	}

	log.Println("Migrações aplicadas no banco de dados!....")
	return nil
}
