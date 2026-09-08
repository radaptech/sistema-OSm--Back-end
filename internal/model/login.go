package model

import "encoding/json"

// Login é o corpo de POST /autenticacao/login -- espelha CredenciaisLogin em
// front-end/src/tipos/autenticacao.ts.
//
// ⚠️ NÃO existe campo `perfil`, e a ausência é deliberada. Ele existiu até aqui,
// e o service o comparava com usuario.perfil devolvendo ErrCredenciaisInvalidas
// quando não batia -- ou seja, ele nunca autorizou nada: quem manda no token e
// na sessão é sempre a linha do banco. O que ele fazia, na prática, era
// transformar "escolhi a aba errada" em "e-mail ou senha inválidos", que é a
// mensagem genérica de propósito e não tinha como explicar o erro real.
//
// Campo extra no corpo é ignorado pelo binding, então um front antigo mandando
// `perfil` continua logando normalmente -- por isso o back pode subir antes.
type Login struct {
	Email string `json:"email" binding:"required,email"`
	Senha string `json:"senha" binding:"required,min=6"`
}

// SetoresIds representa o `number[] | 'todos'` de EscopoAcessoGestor.setoresIds
// no front: AcessoTotal vira a string "todos" no JSON, senão vira o array de
// ids (nunca null -- lista vazia quando o escopo ainda não tem setor).
type SetoresIds struct {
	AcessoTotal bool
	Ids         []int64
}

func (s SetoresIds) MarshalJSON() ([]byte, error) {
	if s.AcessoTotal {
		return json.Marshal("todos")
	}
	if s.Ids == nil {
		return json.Marshal([]int64{})
	}
	return json.Marshal(s.Ids)
}

// EscopoAcessoGestor espelha EscopoAcessoGestor no front.
type EscopoAcessoGestor struct {
	LojaId     int64      `json:"lojaId"`
	SetoresIds SetoresIds `json:"setoresIds"`
}

// SessaoUsuario é o corpo devolvido por POST /autenticacao/login e por
// GET /autenticacao/sessao -- espelha SessaoUsuario no front. LojaId em
// diante são campos exclusivos de um perfil: pointer/slice nil vira `null`
// no JSON pros perfis em que não se aplicam, do jeito que o front espera
// (`| null`, não chave ausente).
type SessaoUsuario struct {
	Id            int64                `json:"id"`
	Nome          string               `json:"nome"`
	Email         string               `json:"email"`
	Perfil        string               `json:"perfil"`
	LojaId        *int64               `json:"lojaId"`
	SetorId       *int64               `json:"setorId"`
	SetorNome     *string              `json:"setorNome"`
	EscoposGestor []EscopoAcessoGestor `json:"escoposGestor"`
	TecnicoId     *int64               `json:"tecnicoId"`
}
