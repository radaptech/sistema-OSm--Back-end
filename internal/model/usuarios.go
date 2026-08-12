package model

import (

	"github.com/radaptech/sistema-OSm--Back-end/config"
)

type Usuario struct {
	Nome               string
	Email              string
	Senha              string
	Telefone           string
	Perfil             string
	LojaId             []int
	SetorId            []int
	AcessoTotalSetores bool
	Area               string
}



type UsuarioReceiver struct {
	id int
	Usuario
	criadoEm config.DataBr
}
