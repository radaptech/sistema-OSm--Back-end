package model

// Loja é a unidade/filial dentro do tenant -- espelha Loja no front
// (src/tipos/loja.ts), então as tags são camelCase: sem elas o Go serializa
// "Nome" e o front lê loja.nome como undefined.
//
// O Id é obrigatório na resposta, não enfeite: é o value dos selects de loja,
// o /cadastrar-loja/:id do botão editar, o ?lojaId= do filtro de usuários e o
// que vai para usuario_escopo.
//
// EmpresaId não é coluna: é o tenant_id da linha. loja.tenant_id referencia
// empresa direto, ou seja, tenant É a empresa -- decisão registrada, e é o que
// faz GET /empresas devolver uma lista de um item só. O front precisa do campo
// porque filtra "lojas já cadastradas dessa empresa" comparando
// loja.empresaId com o valor do select (CadastrarLoja.tsx).
type Loja struct {
	Id        int64  `json:"id"`
	Nome      string `json:"nome"`
	EmpresaId int64  `json:"empresaId"`
	Ativa     bool   `json:"ativa"`
}

// Empresa é o corpo de GET /empresas -- espelha Empresa no front
// (src/tipos/empresa.ts). Id é o tenant_id: ver Loja acima.
type Empresa struct {
	Id   int64  `json:"id"`
	Nome string `json:"nome"`
}

// NovaLojaPayload é o corpo de POST /lojas e PUT /lojas/:id (no front,
// AtualizarLojaPayload é o mesmo corpo mais o id, que aqui vem pela rota).
//
// Sem EmpresaId de propósito: o front manda, mas só existe uma empresa por
// tenant e ela já vem do token -- aceitar o valor do cliente só criaria uma
// forma de errar. Campo extra no JSON é ignorado pelo binding.
type NovaLojaPayload struct {
	Nome string `json:"nome" binding:"required"`
}
