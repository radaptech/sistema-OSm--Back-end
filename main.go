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

	if despacharSubcomando(os.Args[1:]) {
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

// subcomandos são os comandos de CLI que rodam fora da API HTTP -- cada um
// existe porque o que ele faz não cabe num endpoint: provisionar-admin cria o
// primeiro administrador (que POST /usuarios exigiria já autenticado),
// backup-banco despeja o banco pro R2 e preventivas-vencidas abre as
// solicitações automáticas. Os dois últimos são chamados pelo Railway Cron.
var subcomandos = map[string]func([]string){
	"provisionar-admin":    executarProvisionarAdmin,
	"backup-banco":         executarBackupBanco,
	"preventivas-vencidas": executarPreventivasVencidas,
}

// despacharSubcomando roda o subcomando pedido em args[0], se for um dos
// conhecidos, e diz se rodou. false = ninguém reconheceu, siga e suba a API.
//
// ⚠️ Argumento desconhecido cai no false e a API sobe -- é o comportamento de
// sempre, e é uma armadilha conhecida em produção: o Custom Start Command do
// Railway substitui o CMD inteiro do dockerfile, então um comando escrito pela
// metade (`backup-banco` em vez de `./main backup-banco`) ou com typo faz o
// container subir o servidor Gin no lugar do job, sem erro nenhum. Já
// aconteceu com o Cron-BACKUP. Se um dia isso doer de novo, o conserto é
// recusar arg desconhecido aqui -- mas aí `./main` com qualquer lixo no fim
// para de subir a API, o que é uma decisão, não uma correção óbvia.
func despacharSubcomando(args []string) bool {

	if len(args) == 0 {
		return false
	}

	executar, conhecido := subcomandos[args[0]]
	if !conhecido {
		return false
	}

	executar(args[1:])

	return true
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
