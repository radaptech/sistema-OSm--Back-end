package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
)

// Chaves que Autenticacao injeta no contexto do Gin. Vêm da sessão, então
// são as autoritativas depois do login -- TenantId (header X-tenant-ID) só
// vale em POST /autenticacao/login, antes de existir sessão.
const (
	UserId       = "userId"
	UserPerfil   = "user_perfil"
	UserTenantId = "user_TenantId"
)

// NomeCookieSessao é o cookie HttpOnly com o segredo da sessão. O nome é o
// mesmo da época do JWT de propósito: o cookie velho de quem estava logado no
// deploy não tem o formato de um segredo, cai como sessão expirada e o front
// explica isso na tela de login, em vez de um 401 mudo.
const NomeCookieSessao = "token"

// DuracaoCookieSessao é o teto da sessão (60 dias, service.SessaoDuracaoMaxima):
// o navegador pode guardar o cookie até lá, e quem decide se ele ainda vale é
// a linha no banco.
const DuracaoCookieSessao = 60 * 24 * 60 * 60

// GravarCookieSessao escreve (ou, com maxAge -1, apaga) o cookie da sessão.
// Login, logout e o middleware passam por aqui de propósito: o browser só
// apaga um cookie se o Set-Cookie de remoção casar com o de criação, e um
// Set-Cookie sem Secure vindo de origem insegura nem sobrescreve um Secure.
//
// SameSite Lax: front e API ficam sob o mesmo domínio registrável
// (*.radaptech.com.br, localhost em dev), então o cookie viaja. Só vira
// SameSiteNone (que obriga Secure) se o front sair pra outro domínio.
func GravarCookieSessao(ctx *gin.Context, valor string, maxAge int) {
	ctx.SetSameSite(http.SameSiteLaxMode)
	ctx.SetCookie(NomeCookieSessao, valor, maxAge, "/", "", true, true)
}

// SegredoSessao lê o segredo da sessão do request: o cookie, ou (clientes
// fora do navegador, testes manuais) o header Authorization: Bearer.
func SegredoSessao(ctx *gin.Context) string {
	if cookie, err := ctx.Cookie(NomeCookieSessao); err == nil && cookie != "" {
		return cookie
	}
	const portador = "Bearer "
	if header := ctx.GetHeader("Authorization"); strings.HasPrefix(header, portador) {
		return strings.TrimPrefix(header, portador)
	}
	return ""
}

// Ator é o dono de uma sessão válida: o que o middleware põe no contexto.
type Ator struct {
	UsuarioId int64
	TenantId  int64
	Perfil    string
}

// ValidadorSessao é quem confere a sessão no banco (service.SessaoService).
// Interface aqui, e não o service direto, porque o service já importa este
// pacote -- e para os testes do middleware não precisarem de Postgres.
type ValidadorSessao interface {
	// helper.ErrSessaoExpirada quando a sessão não vale mais; qualquer outro
	// erro é falha de infraestrutura.
	ValidarSessao(ctx context.Context, segredo string) (Ator, error)
}

// CodigoSessaoExpirada vai no corpo do 401 quando havia uma sessão e ela não
// vale mais (prazo, logout em outro lugar, usuário desativado, senha trocada).
// É o que deixa o front dizer "sua sessão expirou" na tela de login em vez de
// só mostrar o formulário -- sem cookie nenhum o 401 vem sem código.
const CodigoSessaoExpirada = "sessao_expirada"

// Autenticacao barra o request sem sessão válida e põe o dono dela no
// contexto (UserId, UserPerfil, UserTenantId). A sessão mora no banco
// (migration 000022): cada request a confere, e é isso que faz logout,
// desativação e troca de perfil valerem na hora.
func Autenticacao(validador ValidadorSessao) gin.HandlerFunc {

	return func(ctx *gin.Context) {

		segredo := SegredoSessao(ctx)
		if segredo == "" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Acesso negado, sessão ausente"})
			return
		}

		ator, err := validador.ValidarSessao(ctx.Request.Context(), segredo)
		if err != nil {
			if errors.Is(err, helper.ErrSessaoExpirada) {
				// Apaga o cookie morto: o próximo request já sai sem ele, e o
				// aviso do front aparece uma vez só.
				GravarCookieSessao(ctx, "", -1)
				ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error":  helper.ErrSessaoExpirada.Error(),
					"codigo": CodigoSessaoExpirada,
				})
				return
			}
			// 500, não 401: um 401 desloga o usuário no front, e o banco
			// fora do ar não é motivo para isso.
			slog.ErrorContext(ctx.Request.Context(), "validar sessão", "err", err)
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "erro ao validar a sessão"})
			return
		}

		ctx.Set(UserId, ator.UsuarioId)
		ctx.Set(UserPerfil, ator.Perfil)
		ctx.Set(UserTenantId, ator.TenantId)

		ctx.Next()
	}
}

// GetTenantIDToken devolve o tenant da sessão -- o autoritativo em qualquer rota
// autenticada. Usar GetTenantID (header) aqui deixaria um administrador do
// tenant A escrever no tenant B só trocando o X-tenant-ID.
func GetTenantIDToken(c *gin.Context) (int64, bool) {
	val, exists := c.Get(UserTenantId)
	if !exists {
		return 0, false
	}
	id, ok := val.(int64)
	return id, ok
}

// GetUserPerfil devolve o perfil do dono da sessão (o atual, lido do banco). É o que decide se uma
// listagem filtra por escopo: administrador não tem escopo nenhum, os outros
// perfis só enxergam o que usuario_escopo alcança.
func GetUserPerfil(c *gin.Context) (string, bool) {
	val, exists := c.Get(UserPerfil)
	if !exists {
		return "", false
	}
	perfil, ok := val.(string)
	return perfil, ok
}

// GetUserID devolve o usuario.id do dono da sessão.
func GetUserID(c *gin.Context) (int64, bool) {
	val, exists := c.Get(UserId)
	if !exists {
		return 0, false
	}
	id, ok := val.(int64)
	return id, ok
}
