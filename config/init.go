package config

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Init struct {
	Conexao Conexao
}

func (I *Init) InitAplicattion() (*pgxpool.Pool, error) {

	conf := NewVariaveisAmbiente()

	db, err := I.Conexao.Conn(conf)
	if err != nil {

		return nil, err
	}
	slog.Info("conectado ao banco")
	return db, nil
}
