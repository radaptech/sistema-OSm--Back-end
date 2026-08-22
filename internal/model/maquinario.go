package model

import (
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// MaquinarioInsert é o corpo de POST /maquinas -- o JSON que vem na parte
// `dados` do multipart (montarMultipart no front), espelhando
// NovaMaquinaPayload (src/tipos/maquina.ts).
//
// Preventivas viaja junto da máquina de propósito: a regra de negócio exige ao
// menos uma (esquemaCadastrarMaquina, min(1)) e as duas coisas gravam na mesma
// transação -- máquina sem preventiva não deve chegar a existir. O MaquinaId de
// cada item é ignorado aqui: no cadastro a máquina ainda não tem id.
//
// ⚠️ Os `json:"-"` não são enfeite: ID, TenantID, FotoChave, Ativa e CriadoEm
// são derivados do servidor. Sem a tag o encoding/json casa por nome, sem
// diferenciar maiúscula -- um cliente mandando {"tenantId": 7} escreveria no
// tenant alheio se algum caminho passasse o payload adiante sem sobrescrever.
//
// ⚠️ `serie`, não `numeroSerie`: é o nome que o front manda. Sem a tag o
// Unmarshal não casaria nada e o campo chegaria nil, calado.
//
// As tags `binding` só valem se alguém rodar o validator: o corpo chega por
// json.Unmarshal (multipart), não pelo ShouldBindJSON, e o Unmarshal ignora
// tag de validação -- quem chama roda binding.Validator.ValidateStruct.
type MaquinarioInsert struct {
	ID               int64               `json:"-"`
	TenantID         int64               `json:"-"`
	SetorID          int64               `json:"setorId" binding:"required,gt=0"`
	NumeroPatrimonio string              `json:"numeroPatrimonio" binding:"required"`
	NumeroSerie      *string             `json:"serie"`
	Nome             string              `json:"nome" binding:"required"`
	Descricao        *string             `json:"descricao"`
	Marca            *string             `json:"marca"`
	Modelo           *string             `json:"modelo"`
	FotoChave        *string             `json:"-"`
	Ativa            bool                `json:"-"`
	CriadoEm         config.DataBr       `json:"-"`
	Criticidade      string              `json:"criticidade" binding:"required,oneof=Baixa Média Alta"`
	Preventivas      []PreventivaPayload `json:"preventivas" binding:"required,min=1,dive"`
}

// Maquinario é o corpo de resposta de /maquinas -- espelha Maquina no front
// (src/tipos/maquina.ts), então as tags são camelCase: sem elas o Go serializa
// "Nome" e o front lê maquina.nome como undefined. Já aconteceu com Loja e
// Setor.
//
// SetorNome/LojaId/LojaNome não são colunas de maquina: vêm do JOIN em
// ObterMaquinaPorID/ListarMaquinas (maquina só guarda setor_id; a loja só
// existe via setor). São obrigatórios no tipo do front -- é o padrão "nome vem
// denormalizado do servidor", para a tela não procurar o nome numa lista à
// parte.
//
// FotoUrl, não FotoChave: o banco guarda a chave do objeto no R2 e o service
// resolve numa URL assinada de leitura na hora (bucketR2.URLLeitura). A chave
// crua nunca sai na resposta.
//
// Sem TenantID: é sempre o tenant do próprio token, não acrescenta nada ao
// cliente -- mesma escolha de Loja/Setor.
type Maquinario struct {
	Id               int64   `json:"id"`
	Nome             string  `json:"nome"`
	NumeroPatrimonio string  `json:"numeroPatrimonio"`
	Serie            *string `json:"serie,omitempty"`
	Descricao        *string `json:"descricao,omitempty"`
	Marca            *string `json:"marca,omitempty"`
	Modelo           *string `json:"modelo,omitempty"`
	Criticidade      string  `json:"criticidade"`
	SetorId          int64   `json:"setorId"`
	SetorNome        string  `json:"setorNome"`
	LojaId           int64   `json:"lojaId"`
	LojaNome         string  `json:"lojaNome"`
	FotoUrl          *string `json:"fotoUrl,omitempty"`
	Ativa            bool    `json:"ativa"`
}

func MontarListaMaquinarios(m repository.ListarMaquinasRow) Maquinario {
	return Maquinario{

		Id:               m.ID,
		Nome:             m.Nome,
		NumeroPatrimonio: m.NumeroPatrimonio,
		Serie:            m.NumeroSerie,
		Descricao:        m.Descricao,
		Marca:            m.Marca,
		Modelo:           m.Modelo,
		Criticidade:      string(m.Criticidade),
		SetorId:          m.SetorID,
		SetorNome:        m.SetorNome,
		LojaId:           m.LojaID,
		LojaNome:         m.LojaNome,
		FotoUrl:          m.FotoChave,
		Ativa:            m.Ativa,
	}

}

// AtualizarMaquina é o corpo de PUT /maquinas/:id -- mesmo JSON de
// MaquinarioInsert (o front manda o mesmo objeto nos dois, AtualizarMaquinaPayload
// estende NovaMaquinaPayload), com o id vindo pela rota. As tags seguem a mesma
// regra explicada lá: `serie` é o nome do front e os derivados levam json:"-".
//
// Preventivas substitui o conjunto inteiro, não faz merge incremental (é o que
// o front espera de PUT /maquinas/:id): o service desativa as atuais e insere
// estas, na mesma transação -- mesmo padrão do escopo em AtualizarUsuario.
//
// FotoChave nil significa "não mandou foto nova", e a query preserva a atual
// (COALESCE em maquina.sql) -- não é "apagar a foto". O front não tem ação de
// remover foto, só de trocar.
type AtualizarMaquina struct {
	ID               int64               `json:"-"`
	TenantID         int64               `json:"-"`
	SetorID          int64               `json:"setorId" binding:"required,gt=0"`
	Criticidade      string              `json:"criticidade" binding:"required,oneof=Baixa Média Alta"`
	NumeroPatrimonio string              `json:"numeroPatrimonio" binding:"required"`
	NumeroSerie      *string             `json:"serie"`
	Nome             string              `json:"nome" binding:"required"`
	Descricao        *string             `json:"descricao"`
	Marca            *string             `json:"marca"`
	Modelo           *string             `json:"modelo"`
	FotoChave        *string             `json:"-"`
	Preventivas      []PreventivaPayload `json:"preventivas" binding:"required,min=1,dive"`
}
