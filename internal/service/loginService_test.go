package service

import (
	"testing"

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
