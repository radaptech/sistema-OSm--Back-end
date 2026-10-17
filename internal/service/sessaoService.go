package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

// Os dois prazos da sessão (migration 000022). Usada todo dia, ela vai sendo
// esticada pelo uso, mas nunca passa do teto: 60 dias depois do login a pessoa
// entra de novo. Parada por 14 dias, cai antes.
const (
	SessaoInatividadeMaxima = 14 * 24 * time.Hour
	SessaoDuracaoMaxima     = 60 * 24 * time.Hour
	// O ultimo_uso_em só é regravado quando o último toque já tem mais que isso:
	// sem o intervalo, cada request do app viraria um UPDATE, e um minuto de
	// folga não muda nada num prazo de 14 dias.
	intervaloToqueSessao = time.Minute
	// 256 bits aleatórios: o segredo é a sessão, e ninguém adivinha isso.
	bytesSegredoSessao = 32
)

// SessaoService é o lado da sessão que o middleware de autenticação consome
// (middleware.ValidadorSessao). Criar e apagar moram em funções do pacote
// (criarSessao, apagarSessoesDoUsuario) porque quem chama é o UsuarioService
// (login, logout, desativação, troca de senha) e o RecuperacaoSenhaService,
// dentro das próprias transações.
type SessaoService struct {
	Pool *pgxpool.Pool
}

func NewSessaoService(pool *pgxpool.Pool) *SessaoService {
	return &SessaoService{Pool: pool}
}

// ValidarSessao é o que roda a cada request autenticado: o segredo do cookie
// vira o dono da sessão (usuário, tenant e o perfil ATUAL dele), ou
// helper.ErrSessaoExpirada quando a sessão não existe mais, passou de um dos
// prazos, ou o usuário/empresa foi desativado. Qualquer outro erro é falha do
// banco -- o middleware responde 500, e não 401, para não deslogar ninguém por
// um soluço do Postgres.
func (s *SessaoService) ValidarSessao(ctx context.Context, segredo string) (middleware.Ator, error) {

	hash, ok := hashSegredoSessao(segredo)
	if !ok {
		return middleware.Ator{}, helper.ErrSessaoExpirada
	}

	repo := repository.New(s.Pool)

	agora := time.Now()
	linha, err := repo.ValidarSessao(ctx, repository.ValidarSessaoParams{
		TokenHash:         hash,
		LimiteInatividade: pgTimestamptz(agora.Add(-SessaoInatividadeMaxima)),
		LimiteAbsoluto:    pgTimestamptz(agora.Add(-SessaoDuracaoMaxima)),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return middleware.Ator{}, helper.ErrSessaoExpirada
		}
		return middleware.Ator{}, helper.TraduzErroPostgres(err)
	}

	// Esticar a sessão é acessório: se o UPDATE falhar, o request segue (a
	// sessão era válida) e o próximo tenta de novo.
	if agora.Sub(linha.UltimoUsoEm.Time) > intervaloToqueSessao {
		if err := repo.TocarSessao(ctx, linha.ID); err != nil {
			slog.WarnContext(ctx, "esticar sessão", "sessao", linha.ID, "err", err)
		}
	}

	return middleware.Ator{
		UsuarioId: linha.UsuarioID,
		TenantId:  linha.TenantID,
		Perfil:    string(linha.Perfil),
	}, nil
}

// criarSessao grava uma sessão nova e devolve o segredo que vai no cookie --
// a única vez que ele existe fora do navegador. Aproveita para apagar as
// sessões vencidas de todo mundo: a tabela é pequena, e sem limpeza ela só
// cresceria.
func criarSessao(ctx context.Context, repo *repository.Queries, tenantId, usuarioId int64) (string, error) {

	bruto := make([]byte, bytesSegredoSessao)
	if _, err := rand.Read(bruto); err != nil {
		return "", err
	}
	segredo := base64.RawURLEncoding.EncodeToString(bruto)
	soma := sha256.Sum256(bruto)

	agora := time.Now()
	if err := repo.ApagarSessoesVencidas(ctx, repository.ApagarSessoesVencidasParams{
		LimiteInatividade: pgTimestamptz(agora.Add(-SessaoInatividadeMaxima)),
		LimiteAbsoluto:    pgTimestamptz(agora.Add(-SessaoDuracaoMaxima)),
	}); err != nil {
		return "", helper.TraduzErroPostgres(err)
	}

	if err := repo.CriarSessao(ctx, repository.CriarSessaoParams{
		TenantID:  tenantId,
		UsuarioID: usuarioId,
		TokenHash: soma[:],
	}); err != nil {
		return "", helper.TraduzErroPostgres(err)
	}

	return segredo, nil
}

// apagarSessoesDoUsuario derruba o usuário de todos os aparelhos: desativação
// e troca de senha.
func apagarSessoesDoUsuario(ctx context.Context, repo *repository.Queries, tenantId, usuarioId int64) error {
	if err := repo.ApagarSessoesDoUsuario(ctx, repository.ApagarSessoesDoUsuarioParams{
		TenantID:  tenantId,
		UsuarioID: usuarioId,
	}); err != nil {
		return helper.TraduzErroPostgres(err)
	}
	return nil
}

// hashSegredoSessao devolve o SHA-256 do segredo, que é o que o banco guarda.
// SHA-256 sem salt basta: são 256 bits aleatórios, não uma senha de humano
// (mesmo raciocínio de hashTokenRecuperacao). Segredo que não tem o formato
// de um gerado aqui nem vai ao banco.
func hashSegredoSessao(segredo string) ([]byte, bool) {
	bruto, err := base64.RawURLEncoding.DecodeString(segredo)
	if err != nil || len(bruto) != bytesSegredoSessao {
		return nil, false
	}
	soma := sha256.Sum256(bruto)
	return soma[:], true
}

func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
