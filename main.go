package main

import (
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/config"
	r "github.com/radaptech/sistema-OSm--Back-end/internal/router"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

func main() {

	if len(os.Args) > 1 && os.Args[1] == "provisionar-admin" {
		executarProvisionarAdmin(os.Args[2:])
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

	if err := router.SetTrustedProxies(proxiesConfiaveis()); err != nil {

		log.Fatal(err)
	}

	router.Use(middleware.CorsConfig())
	c := r.NewContainer(db)
	r.ConfigurarRotas(router, c)

	router.Run(":8081")
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
