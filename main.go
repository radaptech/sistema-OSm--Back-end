package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/bucketR2"
	"github.com/radaptech/sistema-OSm--Back-end/config"
	r "github.com/radaptech/sistema-OSm--Back-end/internal/router"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

func main() {

	if len(os.Args) > 1 && os.Args[1] == "provisionar-admin" {
		executarProvisionarAdmin(os.Args[2:])
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "backup-banco" {
		executarBackupBanco(os.Args[2:])
		return
	}

	router := gin.Default()
	postgressConnection := config.ConnPostgresql{}

	init := config.Init{Conexao: &postgressConnection}

	db, err := init.InitAplicattion()
	if err != nil {

		log.Fatal(err)
	}
	err = postgressConnection.RunMigrationPostgress(db)
	if err != nil {

		log.Fatal(err)
	}

	ctx := context.Background()
	bucketr2.InitR2_cloudflare(ctx)

	if err := router.SetTrustedProxies(proxiesConfiaveis()); err != nil {

		log.Fatal(err)
	}

	router.Use(middleware.CorsConfig())
	router.Use(middleware.Timeout(30 * time.Second))
	c := r.NewContainer(db)
	r.ConfigurarRotas(router, c)

	router.Run(":" + porta())
}

// porta lê PORT (o Railway injeta essa variável e roteia o domínio público pra
// ela -- sem ler, a API escuta numa porta fixa que o proxy nunca acerta, e todo
// request vira 502 mesmo com o container de pé). Sem a variável (dev local,
// onde o traefik já aponta pra 8081 fixo em docker-compose.yml), cai no default.
func porta() string {

	p := os.Getenv("PORT")
	if p == "" {

		return "8081"
	}

	return p
}

// proxiesConfiaveis lê TRUSTED_PROXIES (IPs/CIDRs separados por vírgula) e devolve
// quem pode ditar o X-Forwarded-For. Sem a variável, ninguém pode: ClientIP passa a
// ser o IP da conexão e o header é ignorado.
//
// O padrão do Gin é confiar em TODO mundo, e aí o X-Forwarded-For vira campo livre do
// cliente -- um header diferente a cada request ganha um bucket novo e o
// middleware.LimitarPorIP do login deixa de limitar qualquer coisa.
//
// Liste só o endereço do proxy, não a faixa inteira: o Gin caminha o
// X-Forwarded-For da direita pra esquerda e para no primeiro IP não-confiável, então
// uma faixa que também contenha o cliente faz a busca passar direto por ele e cair
// no valor forjado.
func proxiesConfiaveis() []string {

	valor := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if valor == "" {

		return nil
	}

	var proxies []string
	for p := range strings.SplitSeq(valor, ",") {

		if p = strings.TrimSpace(p); p != "" {

			proxies = append(proxies, p)
		}
	}

	return proxies
}
