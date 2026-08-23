package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/auth"
)

// DadosProvisionamento reune o que o operador informa na CLI para criar um
// tenant novo (ou o proximo administrador de um tenant ja existente).
type DadosProvisionamento struct {
	Subdominio  string
	NomeEmpresa string
	NomeAdmin   string
	EmailAdmin  string
	SenhaAdmin  string
}

// ProvisionarAdministrador cria a empresa (se o subdominio ainda nao
// existir) e o usuario administrador dela, numa unica transacao.
//
// E o unico caminho para o primeiro administrador de um tenant: POST
// /usuarios exige um administrador ja autenticado, que nao existe ate este
// comando rodar. Roda fora da API HTTP, direto contra o banco -- sem
// superficie de rede nova para proteger.
func ProvisionarAdministrador(ctx context.Context, pool *pgxpool.Pool, dados DadosProvisionamento) (empresaID, usuarioID int64, err error) {
	if dados.Subdominio == "" || dados.NomeAdmin == "" || dados.EmailAdmin == "" || dados.SenhaAdmin == "" {
		return 0, 0, errors.New("subdominio, nome, email e senha são obrigatórios")
	}
	if len(dados.SenhaAdmin) < 6 {
		return 0, 0, errors.New("senha deve ter ao menos 6 caracteres")
	}

	senhaHash, err := auth.HashPassword(dados.SenhaAdmin)
	if err != nil {
		return 0, 0, fmt.Errorf("erro ao gerar hash da senha: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `SELECT id FROM empresa WHERE subdominio = $1`, dados.Subdominio).Scan(&empresaID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if dados.NomeEmpresa == "" {
			return 0, 0, fmt.Errorf("subdomínio %q não existe ainda — informe --empresa com o nome comercial para criá-lo", dados.Subdominio)
		}
		err = tx.QueryRow(ctx,
			`INSERT INTO empresa (subdominio, nome) VALUES ($1, $2) RETURNING id`,
			dados.Subdominio, dados.NomeEmpresa,
		).Scan(&empresaID)
		if err != nil {
			return 0, 0, fmt.Errorf("erro ao criar empresa: %w", err)
		}
	case err != nil:
		return 0, 0, fmt.Errorf("erro ao consultar empresa: %w", err)
	default:
		// subdomínio já existe: cria só mais um administrador nele.
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO usuario (tenant_id, perfil, nome, email, senha_hash)
		 VALUES ($1, 'administrador', $2, $3, $4)
		 RETURNING id`,
		empresaID, dados.NomeAdmin, dados.EmailAdmin, string(senhaHash),
	).Scan(&usuarioID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, 0, fmt.Errorf("já existe um usuário com o e-mail %q neste tenant", dados.EmailAdmin)
		}
		return 0, 0, fmt.Errorf("erro ao criar administrador: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("erro ao confirmar transação: %w", err)
	}

	return empresaID, usuarioID, nil
}
