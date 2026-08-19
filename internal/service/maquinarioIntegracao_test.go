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

func TestMaquinarioCrud(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcLoja := NewRepoLojas(pool)
	svcSetor := NewRepoSetor(pool)
	svc := NewRepoMaquinario(pool)

	var tenantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('maquinas', 'Empresa Maquinas') RETURNING id`).Scan(&tenantID); err != nil {
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

	// Toda máquina nasce com pelo menos uma preventiva -- é regra de negócio, e
	// o service recusa a lista vazia.
	novaMaquina := func(patrimonio, nome string) model.MaquinarioInsert {
		return model.MaquinarioInsert{
			SetorID:          setor.Id,
			Criticidade:      "Alta",
			NumeroPatrimonio: patrimonio,
			Nome:             nome,
			Preventivas: []model.PreventivaPayload{{
				Descricao:     "Trocar filtro",
				IntervaloDias: 30,
				ProximaData:   config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)),
				Ativa:         true,
			}},
		}
	}

	// Localiza pela listagem em vez de confiar no retorno do cadastro: é o que
	// prova que a transação commitou de verdade, e não só que a função montou
	// uma resposta bonita antes do rollback.
	buscarPorPatrimonio := func(t *testing.T, patrimonio string) (model.Maquinario, bool) {
		t.Helper()
		lidas, err := svc.ListarMaquinario(ctx, tenantID, nil, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		for _, m := range lidas {
			if m.NumeroPatrimonio == patrimonio {
				return m, true
			}
		}
		return model.Maquinario{}, false
	}

	t.Run("cadastra e a máquina persiste depois da transação", func(t *testing.T) {
		resposta, err := svc.CadastrarMaquina(ctx, tenantID, novaMaquina("P1", "  Forno  "))
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		// O front consome POST e GET pelo mesmo tipo Maquina: a resposta da
		// criação já tem que vir com id e nomes resolvidos.
		if resposta.Id == 0 || resposta.SetorNome != "Padaria" || resposta.LojaNome != "Loja A" {
			t.Errorf("resposta do POST incompleta: %+v", resposta)
		}

		criada, ok := buscarPorPatrimonio(t, "P1")
		if !ok {
			t.Fatal("máquina não persistiu -- transação sem commit?")
		}
		if criada.Nome != "Forno" {
			t.Errorf("nome devia vir aparado: %q", criada.Nome)
		}
		// A listagem é a única que traz os nomes: o front tipa setorNome/lojaId
		// como obrigatórios e maquina não guarda loja_id.
		if criada.SetorNome != "Padaria" || criada.LojaId != loja.Id || criada.LojaNome != "Loja A" {
			t.Errorf("nomes denormalizados não vieram: %+v", criada)
		}
	})

	// uq_maquina_patrim é UNIQUE (tenant_id, numero_patrimonio): o INSERT falha
	// com 23505 e precisa chegar no controller como ErrDadoDuplicado (409).
	//
	// Sem checar o erro de CriarMaquina, a transação segue abortada até o
	// tx.Commit, e o que sobe é o ErrTxCommitRollback do pgx ("commit
	// unexpectedly resulted in rollback") -- que não casa com nenhum sentinela,
	// cai no ramo genérico do controller e vira 500. O 23505 já tinha a
	// tradução pronta em TraduzErroPostgres; ele só nunca é consultado.
	t.Run("patrimônio duplicado é dado duplicado, não sucesso silencioso", func(t *testing.T) {
		_, err := svc.CadastrarMaquina(ctx, tenantID, novaMaquina("P1", "Outro Forno"))
		if !errors.Is(err, helper.ErrDadoDuplicado) {
			t.Errorf("esperado ErrDadoDuplicado, veio %v", err)
		}
		if _, ok := buscarPorPatrimonio(t, "P1"); !ok {
			t.Error("a máquina original sumiu")
		}
	})

	t.Run("setor inexistente é conflito, não 500", func(t *testing.T) {
		payload := novaMaquina("P9", "Serra")
		payload.SetorID = setor.Id + 1000
		if _, err := svc.CadastrarMaquina(ctx, tenantID, payload); !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("esperado ErrConflitoIntegridade, veio %v", err)
		}
	})

	t.Run("nome vazio é recusado", func(t *testing.T) {
		if _, err := svc.CadastrarMaquina(ctx, tenantID, novaMaquina("P8", "   ")); !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("esperado ErrValidacao, veio %v", err)
		}
	})

	t.Run("atualiza devolvendo a linha com os nomes resolvidos", func(t *testing.T) {
		alvo, ok := buscarPorPatrimonio(t, "P1")
		if !ok {
			t.Fatal("máquina P1 não encontrada")
		}

		atualizada, err := svc.AtualizarMaquina(ctx, tenantID, alvo.Id, model.AtualizarMaquina{
			SetorID:          setor.Id,
			Criticidade:      "Baixa",
			NumeroPatrimonio: "P1",
			Nome:             "Forno Novo",
			Preventivas: []model.PreventivaPayload{{
				Descricao:     "Lubrificar",
				IntervaloDias: 15,
				ProximaData:   config.NewDataBrPtr(time.Now().AddDate(0, 0, 3)),
				Ativa:         true,
			}},
		})
		if err != nil {
			t.Fatalf("erro ao atualizar: %v", err)
		}
		if atualizada.Id == 0 {
			t.Fatalf("resposta do PUT sem id: %+v", atualizada)
		}
		if atualizada.Nome != "Forno Novo" || atualizada.Criticidade != "Baixa" {
			t.Errorf("atualização não refletiu: %+v", atualizada)
		}
		// Mesma exigência do GET: o front consome os dois pelo tipo Maquina.
		if atualizada.SetorNome != "Padaria" || atualizada.LojaId != loja.Id || atualizada.LojaNome != "Loja A" {
			t.Errorf("PUT devolveu sem os nomes denormalizados: %+v", atualizada)
		}

		relida, err := svc.ObterMaquina(ctx, tenantID, alvo.Id)
		if err != nil {
			t.Fatalf("erro ao reler: %v", err)
		}
		if relida.Nome != "Forno Novo" {
			t.Errorf("atualização não persistiu: %+v", relida)
		}
	})

	// O motivo de CadastrarMaquina ser transacional: máquina e preventivas
	// gravam juntas ou nenhuma grava. Sem isso sobraria máquina sem preventiva,
	// que o front nem consegue editar (o formulário exige min(1)).
	t.Run("preventiva inválida desfaz a máquina junto", func(t *testing.T) {
		payload := novaMaquina("P7", "Fritadeira")
		payload.Preventivas[0].IntervaloDias = 0

		if _, err := svc.CadastrarMaquina(ctx, tenantID, payload); !errors.Is(err, helper.ErrValidacao) {
			t.Fatalf("esperado ErrValidacao, veio %v", err)
		}
		if _, ok := buscarPorPatrimonio(t, "P7"); ok {
			t.Error("máquina persistiu apesar da preventiva inválida -- transação não voltou")
		}
	})

	t.Run("máquina sem preventiva é recusada", func(t *testing.T) {
		payload := novaMaquina("P6", "Balança")
		payload.Preventivas = nil

		if _, err := svc.CadastrarMaquina(ctx, tenantID, payload); !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("esperado ErrValidacao, veio %v", err)
		}
	})

	t.Run("id inexistente e de outro tenant são não encontrado", func(t *testing.T) {
		if _, err := svc.ObterMaquina(ctx, tenantID, 9999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("obter inexistente: %v", err)
		}
		if _, err := svc.AtualizarMaquina(ctx, tenantID, 9999, model.AtualizarMaquina{
			SetorID: setor.Id, Criticidade: "Baixa", NumeroPatrimonio: "PX", Nome: "X",
			Preventivas: []model.PreventivaPayload{{
				Descricao: "X", IntervaloDias: 1,
				ProximaData: config.NewDataBrPtr(time.Now()), Ativa: true,
			}},
		}); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("atualizar inexistente: %v", err)
		}
		if err := svc.DesativarMaquina(ctx, tenantID, 9999); !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("desativar inexistente: %v", err)
		}
	})

	t.Run("desativada some da listagem mas ainda é legível por id", func(t *testing.T) {
		if _, err := svc.CadastrarMaquina(ctx, tenantID, novaMaquina("P2", "Batedeira")); err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		criada, ok := buscarPorPatrimonio(t, "P2")
		if !ok {
			t.Fatal("máquina P2 não persistiu")
		}

		if err := svc.DesativarMaquina(ctx, tenantID, criada.Id); err != nil {
			t.Fatalf("erro ao desativar: %v", err)
		}

		if _, ok := buscarPorPatrimonio(t, "P2"); ok {
			t.Error("máquina desativada continua na listagem")
		}
		// A tela de edição precisa ler mesmo desativada.
		if _, err := svc.ObterMaquina(ctx, tenantID, criada.Id); err != nil {
			t.Errorf("obter desativada por id: %v", err)
		}
	})
}
