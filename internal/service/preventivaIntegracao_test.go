package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

func TestPreventivaCrud(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcLoja := NewRepoLojas(pool)
	svcSetor := NewRepoSetor(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svc := NewRepoPreventiva(pool)

	var tenantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('prev', 'Empresa Prev') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}

	loja, err := svcLoja.CadastrarLoja(ctx, tenantID, model.NovaLojaPayload{Nome: "Loja A"})
	if err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	setor, err := svcSetor.CadastrarSetor(ctx, tenantID, model.NovoSetorPayload{Nome: "Padaria", LojaId: loja.Id})
	if err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}

	emDias := func(dias int) *config.DataBr {
		return config.NewDataBrPtr(time.Now().AddDate(0, 0, dias))
	}

	maquina, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
		SetorID:          setor.Id,
		Criticidade:      "Alta",
		NumeroPatrimonio: "P1",
		Nome:             "Forno",
		Preventivas: []model.PreventivaPayload{{
			Descricao:     "Trocar filtro",
			IntervaloDias: 30,
			ProximaData:   emDias(10),
			Ativa:         true,
		}},
	})
	if err != nil {
		t.Fatalf("erro ao criar máquina: %v", err)
	}

	t.Run("a preventiva do cadastro da máquina já está listada", func(t *testing.T) {
		lidas, err := svc.ListarPreventivas(ctx, tenantID, 0, "administrador", &maquina.Id)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(lidas) != 1 {
			t.Fatalf("esperava 1 preventiva, veio %d", len(lidas))
		}
		p := lidas[0]
		// Denormalizados: o front tipa maquinaNome/setorNome/lojaId como parte
		// de PreventivaListada e não vai buscar numa segunda lista.
		if p.MaquinaNome != "Forno" || p.SetorNome != "Padaria" || p.LojaId != loja.Id || p.LojaNome != "Loja A" {
			t.Errorf("nomes denormalizados não vieram: %+v", p)
		}
		if p.Vencida {
			t.Errorf("preventiva com data futura não devia estar vencida: %+v", p)
		}
	})

	t.Run("cadastra avulsa e devolve com os nomes resolvidos", func(t *testing.T) {
		criada, err := svc.CadastrarPreventiva(ctx, tenantID, model.PreventivaPayload{
			MaquinaId:     maquina.Id,
			Descricao:     "  Lubrificar  ",
			IntervaloDias: 15,
			ProximaData:   emDias(-3),
			Ativa:         true,
		})
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		if criada.Id == 0 || criada.MaquinaNome != "Forno" || criada.LojaNome != "Loja A" {
			t.Errorf("resposta do POST incompleta: %+v", criada)
		}
		if criada.Descricao != "Lubrificar" {
			t.Errorf("descrição devia vir aparada: %q", criada.Descricao)
		}
		// Data já passou e está ativa: é isso que acende o destaque em âmbar.
		if !criada.Vencida {
			t.Errorf("preventiva com data passada devia estar vencida: %+v", criada)
		}
	})

	t.Run("máquina inexistente é conflito, não 500", func(t *testing.T) {
		_, err := svc.CadastrarPreventiva(ctx, tenantID, model.PreventivaPayload{
			MaquinaId:     maquina.Id + 1000,
			Descricao:     "Órfã",
			IntervaloDias: 30,
			ProximaData:   emDias(5),
			Ativa:         true,
		})
		if !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("esperado ErrConflitoIntegridade, veio %v", err)
		}
	})

	t.Run("intervalo e descrição inválidos são recusados antes do banco", func(t *testing.T) {
		base := model.PreventivaPayload{
			MaquinaId: maquina.Id, Descricao: "Válida", IntervaloDias: 30,
			ProximaData: emDias(5), Ativa: true,
		}

		semIntervalo := base
		semIntervalo.IntervaloDias = 0
		if _, err := svc.CadastrarPreventiva(ctx, tenantID, semIntervalo); !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("intervalo zero: esperado ErrValidacao, veio %v", err)
		}

		semDescricao := base
		semDescricao.Descricao = "   "
		if _, err := svc.CadastrarPreventiva(ctx, tenantID, semDescricao); !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("descrição em branco: esperado ErrValidacao, veio %v", err)
		}

		semData := base
		semData.ProximaData = nil
		if _, err := svc.CadastrarPreventiva(ctx, tenantID, semData); !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("sem próxima data: esperado ErrValidacao, veio %v", err)
		}
	})

	t.Run("atualiza mantendo a máquina e devolvendo os nomes", func(t *testing.T) {
		lidas, err := svc.ListarPreventivas(ctx, tenantID, 0, "administrador", &maquina.Id)
		if err != nil || len(lidas) == 0 {
			t.Fatalf("erro ao listar: %v", err)
		}
		alvo := lidas[0]

		atualizada, err := svc.AtualizarPreventiva(ctx, tenantID, alvo.Id, model.PreventivaPayload{
			MaquinaId:     maquina.Id + 999, // ignorado de propósito
			Descricao:     "Revisar motor",
			IntervaloDias: 45,
			ProximaData:   emDias(20),
			Ativa:         true,
		})
		if err != nil {
			t.Fatalf("erro ao atualizar: %v", err)
		}
		if atualizada.Descricao != "Revisar motor" || atualizada.IntervaloDias != 45 {
			t.Errorf("atualização não refletiu: %+v", atualizada)
		}
		// O PUT manda maquinaId, mas mover de máquina deixaria as solicitações
		// já geradas apontando para outra máquina -- o service ignora o campo.
		if atualizada.MaquinaId != maquina.Id {
			t.Errorf("preventiva trocou de máquina: %+v", atualizada)
		}
		if atualizada.MaquinaNome != "Forno" {
			t.Errorf("PUT devolveu sem os nomes: %+v", atualizada)
		}
	})

	t.Run("desativada some da listagem", func(t *testing.T) {
		antes, err := svc.ListarPreventivas(ctx, tenantID, 0, "administrador", &maquina.Id)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(antes) == 0 {
			t.Fatal("nada para desativar")
		}

		if err := svc.DesativarPreventiva(ctx, tenantID, antes[0].Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}

		depois, err := svc.ListarPreventivas(ctx, tenantID, 0, "administrador", &maquina.Id)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(depois) != len(antes)-1 {
			t.Errorf("desativada continua na listagem: antes %d, depois %d", len(antes), len(depois))
		}
		// A tela de edição precisa ler mesmo desativada.
		if _, err := svc.ObterPreventiva(ctx, tenantID, antes[0].Id); err != nil {
			t.Errorf("obter desativada por id: %v", err)
		}
	})

	t.Run("id inexistente e de outro tenant são não encontrado", func(t *testing.T) {
		if _, err := svc.ObterPreventiva(ctx, tenantID, 9999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("obter inexistente: %v", err)
		}
		if _, err := svc.AtualizarPreventiva(ctx, tenantID, 9999, model.PreventivaPayload{
			MaquinaId: maquina.Id, Descricao: "X", IntervaloDias: 1,
			ProximaData: emDias(1), Ativa: true,
		}); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("atualizar inexistente: %v", err)
		}
		if err := svc.DesativarPreventiva(ctx, tenantID, 9999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("desativar inexistente: %v", err)
		}
	})

	// PUT /maquinas/:id substitui o conjunto inteiro, sem merge: é o que o
	// front espera ao salvar o formulário de máquina com a lista editada.
	t.Run("editar a máquina substitui o conjunto de preventivas", func(t *testing.T) {
		_, err := svcMaquina.AtualizarMaquina(ctx, tenantID, maquina.Id, model.AtualizarMaquina{
			SetorID:          setor.Id,
			Criticidade:      "Baixa",
			NumeroPatrimonio: "P1",
			Nome:             "Forno",
			Preventivas: []model.PreventivaPayload{
				{Descricao: "Nova A", IntervaloDias: 10, ProximaData: emDias(4), Ativa: true},
				{Descricao: "Nova B", IntervaloDias: 20, ProximaData: emDias(8), Ativa: true},
			},
		})
		if err != nil {
			t.Fatalf("erro ao atualizar máquina: %v", err)
		}

		lidas, err := svc.ListarPreventivas(ctx, tenantID, 0, "administrador", &maquina.Id)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(lidas) != 2 {
			t.Fatalf("esperava só as 2 novas, veio %d -- as antigas não foram desativadas?", len(lidas))
		}
		for _, p := range lidas {
			if p.Descricao != "Nova A" && p.Descricao != "Nova B" {
				t.Errorf("preventiva antiga sobreviveu: %+v", p)
			}
		}
	})
}
