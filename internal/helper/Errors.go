package helper

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Definição de Erros Amigáveis (Negócio)
var (
	ErrId                  = errors.New("id invalido")
	ErrInternal            = errors.New("Erro Internal do banco de dados")
	ErrNaoEncontrado       = errors.New("registro não encontrado")
	ErrDadoDuplicado       = errors.New("este registro já existe no sistema")
	ErrConflitoIntegridade = errors.New("ID nao existe no sistema")
	ErrCampoObrigatorio    = errors.New("campo obrigatório não preenchido")
	ErrSessaoExpirada      = errors.New("sua sessão expirou, faça login novamente")
	// Mensagem única de propósito: e-mail inexistente, senha errada, usuário
	// inativo e perfil trocado devolvem todos isto, senão o login vira um
	// oráculo de quais e-mails existem no tenant.
	ErrCredenciaisInvalidas = errors.New("e-mail ou senha inválidos")
	ErrNomeCurto            = errors.New("deve ter ao minimo 2 caracteres")
	// Regra de negócio que as tags de binding não alcançam (cardinalidade de
	// escopo, setor de loja não selecionada, ...). Existe para o controller
	// distinguir "cliente mandou errado" (400) de falha de infra (500) sem
	// olhar o texto do erro.
	ErrValidacao = errors.New("dados inválidos")
	ErrToken     = errors.New("")
)

// Códigos de Erro Oficiais do PostgreSQL
const (
	PgUniqueViolation     = "23505" // Chave duplicada (Unique Constraint)
	PgForeignKeyViolation = "23503" // Chave estrangeira (ID que não existe ou está em uso)
	PgNotNullViolation    = "23502" // Tentativa de inserir nulo onde não pode

	PgCheckViolation   = "23514" // Violação de CHECK (ex: quantidade < 0)
	PgNumericOverflow  = "22003" // Valor numérico muito grande (ex: preço absurdo)
	PgDeadlockDetected = "40P01" // Conflito de transações (importante em sistemas com muito uso)
	PgInvalidTextRep   = "22P02" // Erro de conversão (ex: enviar 'abc' num campo int)
)

// TraduzErroPostgres converte erros técnicos do DB em erros de negócio do seu SaaS
func TraduzErroPostgres(err error) error {
	if err == nil {
		return nil
	}

	// Verifica se o erro veio do driver do Postgres
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case PgUniqueViolation:
			return ErrDadoDuplicado
		case PgForeignKeyViolation:
			return ErrConflitoIntegridade
		case PgNotNullViolation:
			return ErrCampoObrigatorio
		case PgCheckViolation:
			// Regra de negócio falhou no banco (ex: saldo < 0)
			return fmt.Errorf("regra de validação do banco violada")
		case PgDeadlockDetected:
			return fmt.Errorf("o sistema está ocupado, tente novamente em instantes")
		}
	}
	return fmt.Errorf("%v", err)
}
