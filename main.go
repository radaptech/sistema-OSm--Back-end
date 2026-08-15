package main

import (
	"log"
	"os"

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

	router.Use(middleware.CorsConfig())
	c := r.NewContainer(db)
	r.ConfigurarRotas(router, c)

	router.Run(":8081")
}
