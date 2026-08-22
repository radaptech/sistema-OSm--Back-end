package model

// NovoUsuarioPayload é o corpo de POST /usuarios -- espelha
// NovoUsuarioPayload no front. Superfície única de escrita dos 4 perfis,
// inclusive Técnico (Area preenchida, LojasIds/SetoresIds ignorados pelo
// servidor nesse caso -- ver front-end/CLAUDE.md item 7).
//
// Cardinalidade de LojasIds/SetoresIds por perfil (solicitante: exatamente 1
// loja + 1 setor; técnico: N lojas, sem setor; gestor: N lojas, cada uma com
// setores ou acesso total; administrador: nenhuma) é regra de negócio e fica
// pro service validar -- ver docs/modelagem-banco-dados.md 3.8. As tags
// abaixo só cobrem formato.
type NovoUsuarioPayload struct {
	Nome               string  `json:"nome" binding:"required"`
	Telefone           *string `json:"telefone"`
	Email              string  `json:"email" binding:"required,email"`
	Senha              string  `json:"senha" binding:"required,min=6"`
	Perfil             string  `json:"perfil" binding:"required,oneof=solicitante tecnico gestor administrador"`
	LojasIds           []int64 `json:"lojasIds" binding:"dive,gt=0"`
	SetoresIds         []int64 `json:"setoresIds" binding:"dive,gt=0"`
	AcessoTotalSetores bool    `json:"acessoTotalSetores"`
	Area               *string `json:"area" binding:"required_if=Perfil tecnico"`
}

// AtualizarUsuarioPayload é o corpo de PUT /usuarios/:id -- igual a
// NovoUsuarioPayload, só que Senha é opcional: omitida, o servidor mantém o
// hash atual (ver front-end/CLAUDE.md item 7, "Senha na edição").
type AtualizarUsuarioPayload struct {
	Nome               string  `json:"nome" binding:"required"`
	Telefone           *string `json:"telefone"`
	Email              string  `json:"email" binding:"required,email"`
	Senha              *string `json:"senha" binding:"omitempty,min=6"`
	Perfil             string  `json:"perfil" binding:"required,oneof=solicitante tecnico gestor administrador"`
	LojasIds           []int64 `json:"lojasIds" binding:"dive,gt=0"`
	SetoresIds         []int64 `json:"setoresIds" binding:"dive,gt=0"`
	AcessoTotalSetores bool    `json:"acessoTotalSetores"`
	Area               *string `json:"area" binding:"required_if=Perfil tecnico"`
}

// Usuario é o corpo devolvido por GET/POST/PUT /usuarios -- espelha Usuario
// no front. Sem Senha de propósito: nem o hash volta pro cliente. Sem Area
// também: técnico é lido via GET /tecnicos (tipo Tecnico próprio), não por
// aqui -- mesma divisão do front (Usuario ≠ Tecnico).
type Usuario struct {
	Id                 int64   `json:"id"`
	Nome               string  `json:"nome"`
	Telefone           *string `json:"telefone,omitempty"`
	Email              string  `json:"email"`
	Perfil             string  `json:"perfil"`
	LojasIds           []int64 `json:"lojasIds"`
	SetoresIds         []int64 `json:"setoresIds"`
	AcessoTotalSetores bool    `json:"acessoTotalSetores"`
	Ativo              bool    `json:"ativo"`
}

// Tecnico é o corpo de GET /tecnicos -- espelha Tecnico no front
// (src/tipos/tecnico.ts). É projeção sobre `usuario`, não entidade própria:
// escrever técnico é POST/PUT /usuarios com perfil 'tecnico' (mesma tabela).
//
// Area é o NOME ("Refrigeração"), não o id: é o que o front exibe ao lado do
// nome no select de Técnico Responsável ("Roberto Alves — Refrigeração"). O
// caminho inverso (nome -> id, na escrita) é o resolverAreaTecnico.
//
// Sem Ativo: a listagem já filtra `ativo`, e técnico desativado não pode ser
// escolhido para uma OS nova -- devolver o campo só criaria a dúvida.
type Tecnico struct {
	Id       int64   `json:"id"`
	Nome     string  `json:"nome"`
	Email    string  `json:"email"`
	Telefone *string `json:"telefone,omitempty"`
	Area     string  `json:"area"`
	LojasIds []int64 `json:"lojasIds"`
}
