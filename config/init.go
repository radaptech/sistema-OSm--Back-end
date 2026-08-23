package config

import (
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Init struct {
	Conexao Conexao
}

func (I *Init) InitAplicattion() (*pgxpool.Pool, error) {

	conf := NewVariaveisAmbiente()

	db, err := I.Conexao.Conn(conf)
	if err != nil {

		log.Printf("Falha ao conectar no banco: %v", err)
		return nil, err
	}
	log.Println("---")
	log.Println("Carregando informações do banco de dados.....")
	log.Println("ARQUIVOS .ENV CARREGADOS")
	log.Println("conexao feita com sucesso!!")
	return db, nil
}
