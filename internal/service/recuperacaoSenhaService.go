package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/auth"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
)

// EnviadorEmailRecuperacao existe para o teste trocar o Resend por um fake, mesmo motivo de NotificadorInterface.
type EnviadorEmailRecuperacao interface {
	RecuperaEmail(ctx context.Context, emailDestino, token, slugEmpresa string) error
}

type RecuperacaoSenhaService struct {
	Pool  *pgxpool.Pool
	Email EnviadorEmailRecuperacao
}

func NewRepoRecuperacaoSenha(pool *pgxpool.Pool, email EnviadorEmailRecuperacao) *RecuperacaoSenhaService {

	return &RecuperacaoSenhaService{
		Pool:  pool,
		Email: email,
	}
}

// SolicitarRecuperacao é POST /autenticacao/esqueci-senha. Devolve nil tanto para e-mail
// cadastrado quanto para inexistente -- senão a rota vira oráculo de quais e-mails existem,
// o mesmo motivo de ErrCredenciaisInvalidas ser um erro só.
func (s *RecuperacaoSenhaService) SolicitarRecuperacao(ctx context.Context, tenantId int64, subdominio, email string) error {

	token := rand.Text()

	destino, err := repository.New(s.Pool).SalvarTokenRecuperacaoSenha(ctx, repository.SalvarTokenRecuperacaoSenhaParams{
		TokenHash: hashTokenRecuperacao(token),
		TenantID:  tenantId,
		Email:     email,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return helper.TraduzErroPostgres(err)
	}

	// Em goroutine pelo mesmo oráculo: esperar o Resend deixaria a resposta do e-mail existente centenas de ms mais lenta.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := s.Email.RecuperaEmail(ctx, destino, token, subdominio); err != nil {
			log.Printf("enviar e-mail de recuperação de senha tenant=%d: %v", tenantId, err)
		}
	}()

	return nil
}

// RedefinirSenha é POST /autenticacao/redefinir-senha. O tenant vem do header, como no login:
// ainda não existe token de sessão, e ele impede que um token vazado valha no subdomínio de outra empresa.
func (s *RecuperacaoSenhaService) RedefinirSenha(ctx context.Context, tenantId int64, token, novaSenha string) error {

	senhaHash, err := auth.HashPassword(novaSenha)
	if err != nil {
		return fmt.Errorf("erro ao gerar hash da senha: %w", err)
	}

	linhas, err := repository.New(s.Pool).RedefinirSenhaPorToken(ctx, repository.RedefinirSenhaPorTokenParams{
		SenhaHash: string(senhaHash),
		TenantID:  tenantId,
		TokenHash: hashTokenRecuperacao(token),
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}

	if linhas == 0 {
		return helper.ErrTokenRecuperacaoInvalido
	}

	return nil
}

// SHA-256 sem salt basta aqui: o token já tem 130 bits aleatórios (rand.Text), não é senha de humano.
func hashTokenRecuperacao(token string) string {
	soma := sha256.Sum256([]byte(token))
	return hex.EncodeToString(soma[:])
}
