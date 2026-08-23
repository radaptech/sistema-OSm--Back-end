package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	bucketr2 "github.com/radaptech/sistema-OSm--Back-end/bucketR2"
	"github.com/radaptech/sistema-OSm--Back-end/config"
)

// executarBackupBanco despeja o banco inteiro (pg_dump --format=custom) num
// arquivo temporário e sobe pro R2. Mesmo papel do provisionar-admin:
// subcomando de CLI que não faz parte da API HTTP, chamado pelo Railway Cron
// (Hobby não libera pg_cron -- ver "Abertura automática de solicitação por
// preventiva" no CLAUDE.md, mesma razão).
//
// Formato --custom em vez de texto puro porque já sai comprimido e permite
// restore seletivo (pg_restore -t tabela); não precisa de gzip por fora.
// --no-owner/--no-privileges porque o restore de teste ("restore ensaiado")
// roda contra um Postgres qualquer, com um dono diferente do de produção --
// sem essas duas flags o pg_restore para em erros de ALTER OWNER/GRANT para
// um role que não existe no banco de destino.
//
//	Uso: go run . backup-banco
func executarBackupBanco(args []string) {
	conf := config.NewVariaveisAmbiente()

	bucket := os.Getenv("R2_BUCKET_NAME_BACKUPS")
	if bucket == "" {
		log.Fatal("R2_BUCKET_NAME_BACKUPS não configurado")
	}

	ctx := context.Background()
	bucketr2.InitR2_cloudflare(ctx)

	// Arquivo temporário em vez de pipe direto pro upload: o SDK da AWS
	// calcula checksum/assinatura melhor sobre um io.ReadSeeker, e um dump
	// de tenant novo é pequeno o bastante pra caber tranquilo no disco do
	// Railway. Se o banco crescer a ponto de doer, well: você já vai ter
	// coisa mais importante pra resolver que isso aqui.
	tmp, err := os.CreateTemp("", "backup-*.dump")
	if err != nil {
		log.Fatalf("erro ao criar arquivo temporário: %v", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	cmd := exec.CommandContext(ctx, "pg_dump",
		"--host="+conf.DB_SERVER,
		"--port="+conf.DB_PORT,
		"--username="+conf.DB_USER,
		"--dbname="+conf.DATABASE,
		"--format=custom",
		"--no-owner",
		"--no-privileges",
	)
	cmd.Env = append(os.Environ(),
		"PGPASSWORD="+conf.DB_PASSWORD,
		"PGSSLMODE="+conf.DB_SSLMODE,
	)
	cmd.Stdout = tmp
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("pg_dump falhou: %v", err)
	}

	if _, err := tmp.Seek(0, 0); err != nil {
		log.Fatalf("erro ao rebobinar o dump: %v", err)
	}

	key := fmt.Sprintf("backups/%s.dump", time.Now().UTC().Format("20060102-150405"))
	if err := bucketr2.UploadArquivo(ctx, bucket, key, tmp, "application/octet-stream"); err != nil {
		log.Fatalf("erro ao subir backup pro R2: %v", err)
	}

	fmt.Printf("backup salvo em %s/%s\n", bucket, key)
}
