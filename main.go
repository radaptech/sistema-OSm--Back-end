package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/config"
)

func main() {

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


	router.Run(":8081")
}
