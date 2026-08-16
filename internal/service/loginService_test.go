package service

import (
	"slices"
	"testing"

	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

func TestEscopoPorPerfil(t *testing.T) {
	area := "Elétrica"

	casos := []struct {
		perfil         string
		payload        model.NovoUsuarioPayload
		valido         bool
		lojas, setores int
		acessoTotal    bool
	}{
		{"admin sem escopo", model.NovoUsuarioPayload{Perfil: "administrador", LojasIds: []int64{1}, SetoresIds: []int64{2}}, true, 0, 0, false},
		{"solicitante 1x1", model.NovoUsuarioPayload{Perfil: "solicitante", LojasIds: []int64{1}, SetoresIds: []int64{2}}, true, 1, 1, false},
		{"solicitante 2 lojas", model.NovoUsuarioPayload{Perfil: "solicitante", LojasIds: []int64{1, 2}, SetoresIds: []int64{2}}, false, 0, 0, false},
		{"tecnico ignora setor", model.NovoUsuarioPayload{Perfil: "tecnico", LojasIds: []int64{1, 2}, SetoresIds: []int64{9}, Area: &area}, true, 2, 0, true},
		{"tecnico sem area", model.NovoUsuarioPayload{Perfil: "tecnico", LojasIds: []int64{1}}, false, 0, 0, false},
		{"gestor com setores", model.NovoUsuarioPayload{Perfil: "gestor", LojasIds: []int64{1, 2}, SetoresIds: []int64{3, 4}}, true, 2, 2, false},
		{"gestor acesso total", model.NovoUsuarioPayload{Perfil: "gestor", LojasIds: []int64{1}, AcessoTotalSetores: true}, true, 1, 0, true},
		{"gestor sem setor nem total", model.NovoUsuarioPayload{Perfil: "gestor", LojasIds: []int64{1}}, false, 0, 0, false},
	}

	for _, c := range casos {
		err := validarEscopo(c.payload)
		if (err == nil) != c.valido {
			t.Fatalf("%s: validarEscopo = %v, esperava valido=%v", c.perfil, err, c.valido)
		}
		if !c.valido {
			continue
		}
		lojas, setores, total := escopoDoPerfil(c.payload)
		if len(lojas) != c.lojas || len(setores) != c.setores || total != c.acessoTotal {
			t.Fatalf("%s: escopoDoPerfil = %v/%v/%v, esperava %d/%d/%v",
				c.perfil, lojas, setores, total, c.lojas, c.setores, c.acessoTotal)
		}
	}
}

// montarUsuario é o caminho inverso de escopoDoPerfil: o que sai daqui tem que
// ser o mesmo formato plano que o front mandou. O caso que importa é o do
// gestor misto -- acessoTotalSetores é um flag só, então basta uma loja
// parcial para ele ser false (senão o front apagaria os setores dela ao salvar).
func TestMontarUsuario(t *testing.T) {

	usuario := repository.Usuario{ID: 7, Nome: "Ana", Email: "ana@x.com", Perfil: "gestor", Ativo: true}

	casos := []struct {
		nome        string
		escopos     []repository.ObterEscoposSessaoPorUsuariosRow
		lojas       []int64
		setores     []int64
		acessoTotal bool
	}{
		{
			nome:    "administrador sem escopo",
			escopos: nil,
			lojas:   []int64{}, setores: []int64{}, acessoTotal: false,
		},
		{
			nome: "solicitante: 1 loja, 1 setor",
			escopos: []repository.ObterEscoposSessaoPorUsuariosRow{
				{UsuarioID: 7, LojaID: 1, SetoresIds: []int64{10}},
			},
			lojas: []int64{1}, setores: []int64{10}, acessoTotal: false,
		},
		{
			nome: "tecnico: N lojas, acesso total, sem setor",
			escopos: []repository.ObterEscoposSessaoPorUsuariosRow{
				{UsuarioID: 7, LojaID: 1, AcessoTotalSetores: true},
				{UsuarioID: 7, LojaID: 2, AcessoTotalSetores: true},
			},
			lojas: []int64{1, 2}, setores: []int64{}, acessoTotal: true,
		},
		{
			nome: "gestor misto: total numa loja, parcial noutra -> flag global false",
			escopos: []repository.ObterEscoposSessaoPorUsuariosRow{
				{UsuarioID: 7, LojaID: 1, AcessoTotalSetores: true},
				{UsuarioID: 7, LojaID: 2, SetoresIds: []int64{20, 21}},
			},
			lojas: []int64{1, 2}, setores: []int64{20, 21}, acessoTotal: false,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := montarUsuario(usuario, c.escopos)

			if !slices.Equal(got.LojasIds, c.lojas) {
				t.Errorf("lojasIds = %v, esperado %v", got.LojasIds, c.lojas)
			}
			if !slices.Equal(got.SetoresIds, c.setores) {
				t.Errorf("setoresIds = %v, esperado %v", got.SetoresIds, c.setores)
			}
			if got.AcessoTotalSetores != c.acessoTotal {
				t.Errorf("acessoTotalSetores = %v, esperado %v", got.AcessoTotalSetores, c.acessoTotal)
			}
			if got.Id != usuario.ID || got.Perfil != string(usuario.Perfil) {
				t.Errorf("campos do usuário não vieram: %+v", got)
			}
		})
	}
}
