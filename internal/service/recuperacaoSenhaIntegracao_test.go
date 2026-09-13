package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radaptech/sistema-OSm--Back-end/auth"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
)

type emailEnviado struct {
	destino, token, slug string
}

// emailFake entrega pelo channel porque o service envia numa goroutine.
type emailFake chan emailEnviado

func (e emailFake) RecuperaEmail(_ context.Context, destino, token, slug string) error {
	e <- emailEnviado{destino, token, slug}
	return nil
}

func TestRecuperacaoSenha(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	enviados := make(emailFake, 10)
	svc := NewRepoRecuperacaoSenha(pool, enviados)

	var tenantID, outroTenantID, usuarioID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('teste', 'Empresa Teste') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('outra', 'Outra Empresa') RETURNING id`).Scan(&outroTenantID); err != nil {
		t.Fatalf("erro ao criar outra empresa: %v", err)
	}
	senhaAntiga, _ := auth.HashPassword("senha-antiga")
	if err := pool.QueryRow(ctx, `
		INSERT INTO usuario (tenant_id, perfil, nome, email, senha_hash)
		VALUES ($1, 'administrador', 'Ana', 'ana@teste.com', $2) RETURNING id`, tenantID, string(senhaAntiga)).Scan(&usuarioID); err != nil {
		t.Fatalf("erro ao criar usuário: %v", err)
	}

	receber := func(t *testing.T) emailEnviado {
		t.Helper()
		select {
		case e := <-enviados:
			return e
		case <-time.After(2 * time.Second):
			t.Fatal("e-mail de recuperação não foi enviado")
			return emailEnviado{}
		}
	}
	nenhumEnvio := func(t *testing.T) {
		t.Helper()
		select {
		case e := <-enviados:
			t.Fatalf("não devia enviar e-mail, enviou para %q", e.destino)
		case <-time.After(200 * time.Millisecond):
		}
	}
	solicitar := func(t *testing.T) string {
		t.Helper()
		if err := svc.SolicitarRecuperacao(ctx, tenantID, "teste", "ana@teste.com"); err != nil {
			t.Fatalf("SolicitarRecuperacao: %v", err)
		}
		return receber(t).token
	}
	senhaConfere := func(t *testing.T, senha string) bool {
		t.Helper()
		var hash string
		if err := pool.QueryRow(ctx, `SELECT senha_hash FROM usuario WHERE id = $1`, usuarioID).Scan(&hash); err != nil {
			t.Fatalf("erro ao ler senha: %v", err)
		}
		ok, _ := auth.HashCompare([]byte(hash), senha)
		return ok
	}

	t.Run("e-mail inexistente responde igual e nao envia nada", func(t *testing.T) {
		if err := svc.SolicitarRecuperacao(ctx, tenantID, "teste", "ninguem@teste.com"); err != nil {
			t.Fatalf("esperava nil (sem oráculo de e-mail), veio %v", err)
		}
		nenhumEnvio(t)
	})

	t.Run("e-mail de outro tenant nao envia", func(t *testing.T) {
		if err := svc.SolicitarRecuperacao(ctx, outroTenantID, "outra", "ana@teste.com"); err != nil {
			t.Fatalf("esperava nil, veio %v", err)
		}
		nenhumEnvio(t)
	})

	t.Run("grava o hash e nao o token, e envia o link do tenant", func(t *testing.T) {
		// Maiúsculas no pedido: email é citext, e o e-mail sai para o endereço cadastrado.
		if err := svc.SolicitarRecuperacao(ctx, tenantID, "teste", "ANA@teste.com"); err != nil {
			t.Fatalf("SolicitarRecuperacao: %v", err)
		}
		e := receber(t)
		if e.destino != "ana@teste.com" || e.slug != "teste" || len(e.token) < 26 {
			t.Fatalf("envio inesperado: %+v", e)
		}

		var hash string
		var validadeCerta bool
		err := pool.QueryRow(ctx, `
			SELECT token_recuperacao_hash,
			       token_recuperacao_expira_em BETWEEN now() + interval '29 minutes' AND now() + interval '30 minutes'
			FROM usuario WHERE id = $1`, usuarioID).Scan(&hash, &validadeCerta)
		if err != nil {
			t.Fatalf("erro ao ler token: %v", err)
		}
		if hash == e.token || hash != hashTokenRecuperacao(e.token) {
			t.Fatalf("banco devia guardar o sha256 do token, guardou %q", hash)
		}
		if !validadeCerta {
			t.Fatal("validade devia ser de 30 minutos")
		}
	})

	t.Run("pedido novo invalida o link anterior", func(t *testing.T) {
		antigo := solicitar(t)
		solicitar(t)

		err := svc.RedefinirSenha(ctx, tenantID, antigo, "senha-nova")
		if !errors.Is(err, helper.ErrTokenRecuperacaoInvalido) {
			t.Fatalf("esperava ErrTokenRecuperacaoInvalido, veio %v", err)
		}
	})

	t.Run("token nao vale em outro tenant", func(t *testing.T) {
		token := solicitar(t)

		err := svc.RedefinirSenha(ctx, outroTenantID, token, "senha-nova")
		if !errors.Is(err, helper.ErrTokenRecuperacaoInvalido) {
			t.Fatalf("esperava ErrTokenRecuperacaoInvalido, veio %v", err)
		}
	})

	t.Run("token expirado nao vale e a senha fica", func(t *testing.T) {
		token := solicitar(t)
		if _, err := pool.Exec(ctx, `UPDATE usuario SET token_recuperacao_expira_em = now() - interval '1 minute' WHERE id = $1`, usuarioID); err != nil {
			t.Fatalf("erro ao expirar token: %v", err)
		}

		err := svc.RedefinirSenha(ctx, tenantID, token, "senha-nova")
		if !errors.Is(err, helper.ErrTokenRecuperacaoInvalido) {
			t.Fatalf("esperava ErrTokenRecuperacaoInvalido, veio %v", err)
		}
		if !senhaConfere(t, "senha-antiga") {
			t.Fatal("token expirado não pode trocar a senha")
		}
	})

	t.Run("redefine a senha e queima o token", func(t *testing.T) {
		token := solicitar(t)

		if err := svc.RedefinirSenha(ctx, tenantID, token, "senha-nova"); err != nil {
			t.Fatalf("RedefinirSenha: %v", err)
		}
		if !senhaConfere(t, "senha-nova") {
			t.Fatal("senha nova não foi gravada")
		}

		var tokenNulo bool
		if err := pool.QueryRow(ctx, `SELECT token_recuperacao_hash IS NULL AND token_recuperacao_expira_em IS NULL FROM usuario WHERE id = $1`, usuarioID).Scan(&tokenNulo); err != nil {
			t.Fatalf("erro ao ler token: %v", err)
		}
		if !tokenNulo {
			t.Fatal("token devia ser apagado depois do uso")
		}

		err := svc.RedefinirSenha(ctx, tenantID, token, "outra-senha")
		if !errors.Is(err, helper.ErrTokenRecuperacaoInvalido) {
			t.Fatalf("segundo uso do mesmo link: esperava ErrTokenRecuperacaoInvalido, veio %v", err)
		}
	})

	t.Run("usuario desativado nao pede nem redefine", func(t *testing.T) {
		token := solicitar(t)
		if _, err := pool.Exec(ctx, `UPDATE usuario SET ativo = false WHERE id = $1`, usuarioID); err != nil {
			t.Fatalf("erro ao desativar usuário: %v", err)
		}

		err := svc.RedefinirSenha(ctx, tenantID, token, "outra-senha")
		if !errors.Is(err, helper.ErrTokenRecuperacaoInvalido) {
			t.Fatalf("esperava ErrTokenRecuperacaoInvalido, veio %v", err)
		}

		if err := svc.SolicitarRecuperacao(ctx, tenantID, "teste", "ana@teste.com"); err != nil {
			t.Fatalf("esperava nil, veio %v", err)
		}
		nenhumEnvio(t)
	})
}
