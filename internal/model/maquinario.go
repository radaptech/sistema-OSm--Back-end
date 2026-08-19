package model

import (
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// Preventivas viaja junto da máquina de propósito: a regra de negócio exige ao
// menos uma (esquemaCadastrarMaquina, min(1)) e as duas coisas gravam na mesma
// transação -- máquina sem preventiva não deve chegar a existir. O MaquinaId de
// cada item é ignorado aqui: no cadastro a máquina ainda não tem id.
type MaquinarioInsert struct {
	ID               int64
	TenantID         int64
	SetorID          int64
	NumeroPatrimonio string
	NumeroSerie      *string
	Nome             string
	Descricao        *string
	Marca            *string
	Modelo           *string
	FotoChave        *string
	Ativa            bool
	CriadoEm         config.DataBr
	Criticidade      string
	Preventivas      []PreventivaPayload
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

// Preventivas substitui o conjunto inteiro, não faz merge incremental (é o que
// o front espera de PUT /maquinas/:id): o service desativa as atuais e insere
// estas, na mesma transação -- mesmo padrão do escopo em AtualizarUsuario.
type AtualizarMaquina struct {
	ID               int64
	TenantID         int64
	SetorID          int64
	Criticidade      string
	NumeroPatrimonio string
	NumeroSerie      *string
	Nome             string
	Descricao        *string
	Marca            *string
	Modelo           *string
	Preventivas      []PreventivaPayload
}
