package config

import (
	"log/slog"
	"os"

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

	// Tenta carregar. Se não achar, avisa UMA VEZ e segue.
	err := godotenv.Load(".env")
	if err != nil {
		dir, _ := os.Getwd()
		slog.Warn(".env não encontrado, usando variáveis do sistema", "dir", dir)
	}

	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {

		sslmode = "disable"
	}

	return &VariaveisDeAmbiente{
		DB_SERVER:   os.Getenv("DB_SERVER"),
		DB_USER:     os.Getenv("DB_USER"),
		DB_PORT:     os.Getenv("DB_PORT"),
		DATABASE:    os.Getenv("DATABASE"),
		DB_PASSWORD: os.Getenv("DB_PASSWORD"),
		DB_SSLMODE:  sslmode,
	}
}
