package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type VariaveisDeAmbiente struct {
	DB_SERVER   string
	DB_USER     string
	DB_PORT     string
	DATABASE    string
	DB_PASSWORD string
	DB_SSLMODE  string
}

func NewVariaveisAmbiente() *VariaveisDeAmbiente {

	dir, _ := os.Getwd()
	log.Println("ATENÇÃO: O Go está procurando o arquivo .env dentro desta pasta:", dir)
	// Tenta carregar. Se não achar, avisa UMA VEZ e segue.
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("Aviso: arquivo .env não encontrado. Continuando com variáveis de sistema...")
	}

	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {

		sslmode = "disable"
	}

	log.Println("=== INÍCIO DA ESPIONAGEM ===")
	// os.Environ() lista tudo que existe na memória do sistema
	for _, env := range os.Environ() {
		// Separa o nome da variável do valor dela
		chave := strings.Split(env, "=")[0]

		// Vamos filtrar só as nossas para não poluir o log
		if strings.HasPrefix(chave, "DB_") || chave == "DATABASE" {
			log.Println(" Achei esta variável no sistema:", chave)
		}
	}
	log.Println("=== FIM DA ESPIONAGEM ===")

	return &VariaveisDeAmbiente{
		DB_SERVER:   os.Getenv("DB_SERVER"),
		DB_USER:     os.Getenv("DB_USER"),
		DB_PORT:     os.Getenv("DB_PORT"),
		DATABASE:    os.Getenv("DATABASE"),
		DB_PASSWORD: os.Getenv("DB_PASSWORD"),
		DB_SSLMODE:  sslmode,
	}
}
