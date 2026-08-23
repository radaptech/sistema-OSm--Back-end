package model

// EmpresaTerceirizada é a prestadora externa que o Técnico aciona quando decide
// não resolver a OS internamente -- espelha EmpresaTerceirizada no front
// (src/tipos/empresaTerceirizada.ts), então as tags são camelCase.
//
// Entidade rasa de propósito: não tem loja nem setor. Ela é do tenant inteiro,
// e é por isso que a listagem dela é a única do sistema que não recorta por
// escopo de acesso.
//
// Especialidade e Telefone são ponteiros porque as colunas são nulláveis e o
// front tipa os dois como opcionais. Com `omitempty`, nulo some do JSON em vez
// de virar "", que é o que o front espera para esconder a linha no card.
//
// Ativa vai na resposta mesmo o tipo do front não declarando o campo (mesma
// escolha de Loja e Setor): a listagem só devolve ativas, mas GET /:id devolve
// a desativada para a tela de edição, e sem o campo não há como a tela saber.
// Campo extra no JSON é ignorado pelo TypeScript.
type EmpresaTerceirizada struct {
	Id            int64   `json:"id"`
	Nome          string  `json:"nome"`
	Especialidade *string `json:"especialidade,omitempty"`
	Telefone      *string `json:"telefone,omitempty"`
	Ativa         bool    `json:"ativa"`
}

// NovaEmpresaTerceirizadaPayload é o corpo de POST /empresas-terceirizadas e de
// PUT /empresas-terceirizadas/:id (no front, AtualizarEmpresaTerceirizadaPayload
// é o mesmo corpo mais o id, que aqui vem pela rota).
//
// ⚠️ Especialidade e Telefone chegam como string vazia, não ausentes: o
// formulário nasce com defaultValues vazios e o React Hook Form manda a string
// vazia do input que ninguém tocou. Sem normalizar, o banco guarda "" numa coluna nullable e a
// resposta volta com o campo presente e vazio -- o service apara e transforma
// em NULL, mesmo tratamento que nomeValido dá ao nome.
//
// `binding:"required"` no nome não basta sozinho (passa numa string de espaços,
// e não há CHECK no banco): quem recusa o nome em branco é o nomeValido do
// service, compartilhado com loja e setor.
type NovaEmpresaTerceirizadaPayload struct {
	Nome          string  `json:"nome" binding:"required"`
	Especialidade *string `json:"especialidade"`
	Telefone      *string `json:"telefone"`
}
