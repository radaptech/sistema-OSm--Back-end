package model

type Login struct {
	Perfil string
	Email  string
	Senha  string
}

type LoginReceive struct {
	Id        int
	Nome      string
	Email     string
	Perfil    string
	LojaId    *int
	SetorId   *int
	SetorNome *string
	TecnicoId *int
}
