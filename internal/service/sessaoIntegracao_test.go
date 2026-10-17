package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// TestSessaoNoBanco tranca a sessão da migration 000022 contra o Postgres: os
// dois prazos (14 dias sem uso, 60 desde o login), o uso esticando a sessão e
// cada jeito de derrubá-la -- logout (só aquele aparelho), desativar o usuário,
// trocar a senha (pelo Administrador ou pela recuperação) e desativar a
// empresa. Também que o perfil vem do banco a cada request, não do login.
func TestSessaoNoBanco(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcSessao := NewSessaoService(pool)
	svcRecuperacao := NewRepoRecuperacaoSenha(pool, nil)

	var tenantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('sessao', 'Empresa Sessão') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}

	senha := "senha-forte-123"
	cadastrar := func(nome, email string) model.Usuario {
		t.Helper()
		u, err := svcUsuario.CadastrarUsuario(ctx, model.NovoUsuarioPayload{Nome: nome, Email: email, Perfil: "administrador", Senha: senha}, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", nome, err)
		}
		return u
	}
	logar := func(email string) string {
		t.Helper()
		segredo, _, err := svcUsuario.Login(ctx, model.Login{Email: email, Senha: senha}, tenantID)
		if err != nil {
			t.Fatalf("erro no login de %s: %v", email, err)
		}
		return segredo
	}
	vale := func(segredo string) (bool, string) {
		t.Helper()
		ator, err := svcSessao.ValidarSessao(ctx, segredo)
		if errors.Is(err, helper.ErrSessaoExpirada) {
			return false, ""
		}
		if err != nil {
			t.Fatalf("erro ao validar sessão: %v", err)
		}
		return true, ator.Perfil
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("erro em %q: %v", sql, err)
		}
	}
	sessoesDe := func(usuarioId int64) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessao WHERE usuario_id = $1`, usuarioId).Scan(&n); err != nil {
			t.Fatalf("erro ao contar sessões: %v", err)
		}
		return n
	}

	ana := cadastrar("Ana", "ana@sessao.com")
	bia := cadastrar("Bia", "bia@sessao.com")

	t.Run("o login devolve um segredo que vale e diz quem é, e o banco não guarda o segredo", func(t *testing.T) {
		segredo := logar(ana.Email)
		ator, err := svcSessao.ValidarSessao(ctx, segredo)
		if err != nil || ator.UsuarioId != ana.Id || ator.TenantId != tenantID || ator.Perfil != "administrador" {
			t.Fatalf("ator = %+v, err %v", ator, err)
		}
		// Nem o texto do cookie nem os bytes aleatórios dele: só o hash.
		bruto, err := base64.RawURLEncoding.DecodeString(segredo)
		if err != nil {
			t.Fatalf("segredo fora do formato: %v", err)
		}
		var guardadoIgual bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessao WHERE token_hash = $1 OR token_hash = $2)`, []byte(segredo), bruto).Scan(&guardadoIgual); err != nil {
			t.Fatalf("erro ao conferir: %v", err)
		}
		if guardadoIgual {
			t.Error("o segredo está guardado em claro")
		}
	})

	t.Run("segredo inventado ou malformado não vale", func(t *testing.T) {
		for _, s := range []string{"", "abc", "jwt.de.antes.do.deploy", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
			if ok, _ := vale(s); ok {
				t.Errorf("%q valeu", s)
			}
		}
	})

	t.Run("14 dias sem uso derrubam; 13 não, e o uso estica a sessão", func(t *testing.T) {
		segredo := logar(ana.Email)
		exec(`UPDATE sessao SET ultimo_uso_em = now() - interval '13 days' WHERE token_hash = $1`, mustHash(t, segredo))
		if ok, _ := vale(segredo); !ok {
			t.Fatal("13 dias sem uso já derrubou")
		}
		// O request acima esticou: o último uso voltou para agora.
		var esticada bool
		if err := pool.QueryRow(ctx, `SELECT ultimo_uso_em > now() - interval '1 minute' FROM sessao WHERE token_hash = $1`, mustHash(t, segredo)).Scan(&esticada); err != nil {
			t.Fatalf("erro: %v", err)
		}
		if !esticada {
			t.Error("o uso não esticou a sessão")
		}

		exec(`UPDATE sessao SET ultimo_uso_em = now() - interval '14 days 1 minute' WHERE token_hash = $1`, mustHash(t, segredo))
		if ok, _ := vale(segredo); ok {
			t.Error("passou de 14 dias sem uso e seguiu valendo")
		}
	})

	t.Run("60 dias depois do login cai, mesmo usada todo dia", func(t *testing.T) {
		segredo := logar(ana.Email)
		exec(`UPDATE sessao SET criada_em = now() - interval '59 days' WHERE token_hash = $1`, mustHash(t, segredo))
		if ok, _ := vale(segredo); !ok {
			t.Fatal("59 dias já derrubou")
		}
		exec(`UPDATE sessao SET criada_em = now() - interval '60 days 1 minute' WHERE token_hash = $1`, mustHash(t, segredo))
		if ok, _ := vale(segredo); ok {
			t.Error("passou de 60 dias e seguiu valendo")
		}
	})

	t.Run("o login apaga as sessões vencidas de todo mundo", func(t *testing.T) {
		velha := logar(bia.Email)
		exec(`UPDATE sessao SET criada_em = now() - interval '61 days' WHERE token_hash = $1`, mustHash(t, velha))
		logar(ana.Email)
		var sobrou bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessao WHERE token_hash = $1)`, mustHash(t, velha)).Scan(&sobrou); err != nil {
			t.Fatalf("erro: %v", err)
		}
		if sobrou {
			t.Error("a sessão vencida da Bia não foi apagada no login da Ana")
		}
	})

	t.Run("logout derruba só o aparelho que saiu", func(t *testing.T) {
		celular, computador := logar(ana.Email), logar(ana.Email)
		if err := svcUsuario.EncerrarSessao(ctx, celular); err != nil {
			t.Fatalf("erro no logout: %v", err)
		}
		if ok, _ := vale(celular); ok {
			t.Error("a sessão do celular seguiu valendo depois do logout")
		}
		if ok, _ := vale(computador); !ok {
			t.Error("o logout do celular derrubou o computador")
		}
		// Sair de novo, ou com lixo, não é erro.
		for _, s := range []string{celular, "lixo"} {
			if err := svcUsuario.EncerrarSessao(ctx, s); err != nil {
				t.Errorf("logout de %q: %v", s, err)
			}
		}
	})

	t.Run("o perfil vem do banco a cada request, não do login", func(t *testing.T) {
		segredo := logar(bia.Email)
		exec(`UPDATE usuario SET perfil = 'gestor' WHERE id = $1`, bia.Id)
		if _, perfil := vale(segredo); perfil != "gestor" {
			t.Errorf("perfil = %q, esperado o novo (gestor)", perfil)
		}
		exec(`UPDATE usuario SET perfil = 'administrador' WHERE id = $1`, bia.Id)
	})

	t.Run("senha trocada pelo Administrador derruba todos os aparelhos", func(t *testing.T) {
		a, b := logar(bia.Email), logar(bia.Email)
		nova := senha // mesma senha: o que importa é que houve troca
		if _, err := svcUsuario.AtualizarUsuario(ctx, bia.Id, model.AtualizarUsuarioPayload{
			Nome: bia.Nome, Email: bia.Email, Perfil: "administrador", Senha: &nova,
		}, tenantID); err != nil {
			t.Fatalf("erro ao atualizar: %v", err)
		}
		for _, s := range []string{a, b} {
			if ok, _ := vale(s); ok {
				t.Error("sessão sobreviveu à troca de senha")
			}
		}
	})

	t.Run("editar sem trocar a senha não derruba ninguém", func(t *testing.T) {
		segredo := logar(bia.Email)
		if _, err := svcUsuario.AtualizarUsuario(ctx, bia.Id, model.AtualizarUsuarioPayload{
			Nome: "Bia Souza", Email: bia.Email, Perfil: "administrador",
		}, tenantID); err != nil {
			t.Fatalf("erro ao atualizar: %v", err)
		}
		if ok, _ := vale(segredo); !ok {
			t.Error("editar o nome derrubou a sessão")
		}
	})

	t.Run("senha redefinida pela recuperação derruba todos os aparelhos", func(t *testing.T) {
		segredo := logar(bia.Email)
		exec(`UPDATE usuario SET token_recuperacao_hash = $2, token_recuperacao_expira_em = now() + interval '30 minutes' WHERE id = $1`,
			bia.Id, hashTokenRecuperacao("token-de-teste"))
		if err := svcRecuperacao.RedefinirSenha(ctx, tenantID, "token-de-teste", senha); err != nil {
			t.Fatalf("erro ao redefinir: %v", err)
		}
		if ok, _ := vale(segredo); ok {
			t.Error("sessão sobreviveu à recuperação de senha")
		}
		// Token já queimado: o erro de sempre, sem 500.
		if err := svcRecuperacao.RedefinirSenha(ctx, tenantID, "token-de-teste", senha); !errors.Is(err, helper.ErrTokenRecuperacaoInvalido) {
			t.Errorf("token reusado: err = %v, esperado ErrTokenRecuperacaoInvalido", err)
		}
	})

	t.Run("desativar o usuário derruba e apaga as sessões dele", func(t *testing.T) {
		segredo := logar(bia.Email)
		if err := svcUsuario.DesativarUsuario(ctx, bia.Id, tenantID, ana.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}
		if ok, _ := vale(segredo); ok {
			t.Error("usuário desativado seguiu logado")
		}
		if n := sessoesDe(bia.Id); n != 0 {
			t.Errorf("sobraram %d sessões do usuário desativado", n)
		}
	})

	t.Run("desativar a empresa derruba quem está dentro", func(t *testing.T) {
		segredo := logar(ana.Email)
		exec(`UPDATE empresa SET ativa = false WHERE id = $1`, tenantID)
		defer exec(`UPDATE empresa SET ativa = true WHERE id = $1`, tenantID)
		if ok, _ := vale(segredo); ok {
			t.Error("empresa desativada e a sessão seguiu valendo")
		}
	})
}

func mustHash(t *testing.T, segredo string) []byte {
	t.Helper()
	hash, ok := hashSegredoSessao(segredo)
	if !ok {
		t.Fatalf("segredo malformado: %q", segredo)
	}
	return hash
}
