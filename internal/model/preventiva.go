package model

import (
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// PreventivaPayload é uma preventiva vinda do cliente. Serve os dois caminhos
// de escrita: o corpo de POST/PUT /preventivas e cada item da lista
// `preventivas` que viaja dentro de POST/PUT /maquinas -- espelha
// PreventivaManutencao no front (src/tipos/maquina.ts).
//
// MaquinaId é ignorado quando a preventiva chega junto da máquina: nesse
// momento a máquina ainda não tem id (o front manda 0), e quem manda é o id
// recém-inserido. Só POST /preventivas avulso usa o campo.
//
// ProximaData é ponteiro porque config.DataBr só tem MarshalJSON com receiver
// ponteiro -- como campo valor o encoding/json ignora o método e serializa {}.
type PreventivaPayload struct {
	MaquinaId     int64          `json:"maquinaId"`
	Descricao     string         `json:"descricao" binding:"required"`
	IntervaloDias int32          `json:"intervaloDias" binding:"required,gt=0"`
	ProximaData   *config.DataBr `json:"proximaData" binding:"required"`
	Ativa         bool           `json:"ativa"`
}

// Preventiva é o corpo de resposta de /preventivas -- espelha PreventivaListada
// no front. Os nomes (máquina, setor, loja) vêm denormalizados do JOIN em
// ListarPreventivas/ObterPreventivaPorID, mesmo padrão de Maquinario.
//
// Vencida é calculado no banco, nunca coluna: a preventiva está ativa e a
// proxima_data já chegou. É o que o front usa para o destaque em âmbar.
type Preventiva struct {
	Id            int64          `json:"id"`
	MaquinaId     int64          `json:"maquinaId"`
	MaquinaNome   string         `json:"maquinaNome"`
	Descricao     string         `json:"descricao"`
	IntervaloDias int32          `json:"intervaloDias"`
	ProximaData   *config.DataBr `json:"proximaData"`
	Ativa         bool           `json:"ativa"`
	SetorId       int64          `json:"setorId"`
	SetorNome     string         `json:"setorNome"`
	LojaId        int64          `json:"lojaId"`
	LojaNome      string         `json:"lojaNome"`
	Vencida       bool           `json:"vencida"`
}

// MontarPreventiva é a única tradução de linha de preventiva para resposta --
// mesmo motivo de montarSetor/MontarListaMaquinarios. As duas rows geradas pelo
// sqlc (listagem e por id) têm os mesmos campos, então uma função serve as duas.
func MontarPreventiva(p repository.ListarPreventivasRow) Preventiva {
	return Preventiva{
		Id:            p.ID,
		MaquinaId:     p.MaquinaID,
		MaquinaNome:   p.MaquinaNome,
		Descricao:     p.Descricao,
		IntervaloDias: p.IntervaloDias,
		ProximaData:   config.NewDataBrPtr(p.ProximaData.Time),
		Ativa:         p.Ativa,
		SetorId:       p.SetorID,
		SetorNome:     p.SetorNome,
		LojaId:        p.LojaID,
		LojaNome:      p.LojaNome,
		Vencida:       p.Vencida,
	}
}
