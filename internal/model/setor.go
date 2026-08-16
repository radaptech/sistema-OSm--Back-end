package model

// Setor pertence a uma loja -- espelha SetorCadastrado no front
// (src/tipos/setor.ts), daí as tags camelCase.
//
// O Id é o campo mais importante daqui: não existe lista fixa de setores e
// dois "Padaria" em lojas diferentes são registros distintos, então TUDO
// referencia setor_id e nunca o nome -- usuario_escopo_setor, maquina,
// solicitacao_os, e o agrupamento do painel do gestor
// (acessoGestor.ts procura o setor por id para nomear o bloco).
type Setor struct {
	Id     int64  `json:"id"`
	Nome   string `json:"nome"`
	LojaId int64  `json:"lojaId"`
	Ativo  bool   `json:"ativo"`
}

// NovoSetorPayload é o corpo de POST /setores e PUT /setores/:id.
//
// No PUT o LojaId é ignorado: mover setor de loja arrastaria junto máquinas,
// histórico de OS e o escopo de quem tem acesso a ele (ver AtualizarSetor em
// database/queries/setor.sql). O front manda o campo nos dois verbos, e é o
// service que decide o que fazer com ele.
type NovoSetorPayload struct {
	Nome   string `json:"nome" binding:"required"`
	LojaId int64  `json:"lojaId" binding:"required,gt=0"`
}
