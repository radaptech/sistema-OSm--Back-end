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

// TestSolicitacaoCrud cobre o fluxo humano de Solicitação: as duas criações
// (maquinário e reparo), as listagens (minha/fila do gestor, as duas
// recortadas por escopo), a leitura por id (idem) e as duas transições
// (abrir-os/rejeitar). O job de preventiva vencida já tem o próprio teste
// (preventivaJobIntegracao_test.go) -- este cobre só o caminho `origem =
// 'solicitante'`.
func TestSolicitacaoCrud(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svcUsuario := NewRepoUsuario(pool)
	svcMaquina := NewRepoMaquinario(pool)
	svc := NewRepoSolicitacao(pool)

	var tenantID, lojaID, setorA, setorB int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('solicitacoes', 'Empresa Solicitacoes') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja A') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	for _, s := range []struct {
		nome string
		dest *int64
	}{{"Padaria", &setorA}, {"Açougue", &setorB}} {
		if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, $3) RETURNING id`, tenantID, lojaID, s.nome).Scan(s.dest); err != nil {
			t.Fatalf("erro ao criar setor: %v", err)
		}
	}

	maquinaAtiva, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
		SetorID: setorA, Criticidade: "Alta", NumeroPatrimonio: "PAT-1", Nome: "Forno",
		Preventivas: []model.PreventivaPayload{{
			Descricao: "Revisão", IntervaloDias: 30,
			ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
		}},
	})
	if err != nil {
		t.Fatalf("erro ao criar máquina ativa: %v", err)
	}

	maquinaDesativada, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
		SetorID: setorA, Criticidade: "Baixa", NumeroPatrimonio: "PAT-2", Nome: "Câmara fria",
		Preventivas: []model.PreventivaPayload{{
			Descricao: "Revisão", IntervaloDias: 30,
			ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
		}},
	})
	if err != nil {
		t.Fatalf("erro ao criar máquina desativada: %v", err)
	}
	if err := svcMaquina.DesativarMaquina(ctx, tenantID, maquinaDesativada.Id); err != nil {
		t.Fatalf("erro ao desativar máquina: %v", err)
	}

	// Máquina de outro setor -- usada só pra provar que o solicitante não
	// consegue abrir solicitação nela.
	maquinaOutroSetor, err := svcMaquina.CadastrarMaquina(ctx, tenantID, model.MaquinarioInsert{
		SetorID: setorB, Criticidade: "Alta", NumeroPatrimonio: "PAT-3", Nome: "Serra",
		Preventivas: []model.PreventivaPayload{{
			Descricao: "Revisão", IntervaloDias: 30,
			ProximaData: config.NewDataBrPtr(time.Now().AddDate(0, 0, 7)), Ativa: true,
		}},
	})
	if err != nil {
		t.Fatalf("erro ao criar máquina de outro setor: %v", err)
	}

	area, senha := "Elétrica", "senha-forte-123"
	cadastrar := func(nome, email, perfil string, p model.NovoUsuarioPayload) model.Usuario {
		t.Helper()
		p.Nome, p.Email, p.Perfil, p.Senha = nome, email, perfil, senha
		u, err := svcUsuario.CadastrarUsuario(ctx, p, tenantID)
		if err != nil {
			t.Fatalf("erro ao cadastrar %s: %v", perfil, err)
		}
		return u
	}

	admin := cadastrar("Ana", "ana@solicitacoes.com", "administrador", model.NovoUsuarioPayload{})
	solicitante := cadastrar("Bruno", "bruno@solicitacoes.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaID}, SetoresIds: []int64{setorA},
	})
	solicitanteOutroSetor := cadastrar("Bia", "bia@solicitacoes.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaID}, SetoresIds: []int64{setorB},
	})
	gestorParcial := cadastrar("Carla", "carla@solicitacoes.com", "gestor", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaID}, SetoresIds: []int64{setorA},
	})
	tecnico := cadastrar("Eder", "eder@solicitacoes.com", "tecnico", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaID}, Area: &area,
	})
	// Técnico de mentira: perfil solicitante, só pra AbrirOS recusar.
	naoTecnico := cadastrar("Fabio", "fabio@solicitacoes.com", "solicitante", model.NovoUsuarioPayload{
		LojasIds: []int64{lojaID}, SetoresIds: []int64{setorA},
	})

	anexoFoto := func() model.NovaSolicitacaoMaquinarioPayload {
		return model.NovaSolicitacaoMaquinarioPayload{
			MaquinaId: maquinaAtiva.Id,
			Descricao: "Barulho estranho no motor",
			Impactos:  []string{"Afeta Produção"},
			FotoChave: "tenant/1/foto.jpg", FotoMime: "image/jpeg", FotoTamanho: 12345,
		}
	}

	var solicitacaoMaquinarioID, solicitacaoReparoID int64

	t.Run("cadastra solicitação de maquinário e persiste depois da transação", func(t *testing.T) {
		resposta, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, anexoFoto())
		if err != nil {
			t.Fatalf("erro ao cadastrar: %v", err)
		}
		solicitacaoMaquinarioID = resposta.Id

		if resposta.MaquinaNome == nil || *resposta.MaquinaNome != "Forno" {
			t.Errorf("maquinaNome = %v, esperado Forno", resposta.MaquinaNome)
		}
		if resposta.SetorNome != "Padaria" || resposta.LojaNome != "Loja A" {
			t.Errorf("setor/loja não vieram denormalizados: %+v", resposta)
		}
		if resposta.Status != "Pendente" || resposta.Origem != "solicitante" {
			t.Errorf("status/origem = %s/%s, esperado Pendente/solicitante", resposta.Status, resposta.Origem)
		}
		if len(resposta.Impactos) != 1 || resposta.Impactos[0] != "Afeta Produção" {
			t.Errorf("impactos = %v", resposta.Impactos)
		}
		if len(resposta.Anexos) != 1 || resposta.Anexos[0].Tipo != "foto" || resposta.Anexos[0].Url == nil || *resposta.Anexos[0].Url != "tenant/1/foto.jpg" {
			t.Errorf("anexos = %+v", resposta.Anexos)
		}

		// Localiza pela leitura, não pelo retorno -- confirma que a transação
		// commitou de verdade (mesmo critério de TestMaquinarioCrud).
		relida, err := svc.ObterSolicitacao(ctx, tenantID, admin.Id, "administrador", resposta.Id)
		if err != nil {
			t.Fatalf("solicitação não persistiu: %v", err)
		}
		if relida.Descricao != "Barulho estranho no motor" {
			t.Errorf("descrição não persistiu: %q", relida.Descricao)
		}
	})

	t.Run("cadastra solicitação de reparo, sem impactos e sem máquina", func(t *testing.T) {
		resposta, err := svc.CadastrarSolicitacaoReparo(ctx, tenantID, solicitante.Id, model.NovaSolicitacaoReparoPayload{
			Item: "Lâmpada queimada", Descricao: "Corredor principal",
			FotoChave: "tenant/1/reparo.jpg", FotoMime: "image/jpeg", FotoTamanho: 999,
		})
		if err != nil {
			t.Fatalf("erro ao cadastrar reparo: %v", err)
		}
		solicitacaoReparoID = resposta.Id

		if resposta.MaquinaId != nil || resposta.ItemDescricao == nil || *resposta.ItemDescricao != "Lâmpada queimada" {
			t.Errorf("reparo com forma errada: maquinaId=%v itemDescricao=%v", resposta.MaquinaId, resposta.ItemDescricao)
		}
		if resposta.SetorNome != "Padaria" {
			t.Errorf("setor do reparo devia vir do escopo do solicitante, veio %q", resposta.SetorNome)
		}
		if len(resposta.Impactos) != 0 {
			t.Errorf("reparo não tem impactos, veio %v", resposta.Impactos)
		}
	})

	t.Run("recusa máquina de outro setor", func(t *testing.T) {
		payload := anexoFoto()
		payload.MaquinaId = maquinaOutroSetor.Id
		_, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, payload)
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("erro = %v, esperado ErrValidacao", err)
		}
	})

	t.Run("recusa máquina desativada", func(t *testing.T) {
		payload := anexoFoto()
		payload.MaquinaId = maquinaDesativada.Id
		_, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, payload)
		if !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("erro = %v, esperado ErrConflitoIntegridade", err)
		}
	})

	t.Run("recusa máquina inexistente", func(t *testing.T) {
		payload := anexoFoto()
		payload.MaquinaId = 999999
		_, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, payload)
		if !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("erro = %v, esperado ErrConflitoIntegridade", err)
		}
	})

	t.Run("recusa marcador de impacto desconhecido", func(t *testing.T) {
		payload := anexoFoto()
		payload.Impactos = []string{"Urgente"}
		_, err := svc.CadastrarSolicitacaoMaquinario(ctx, tenantID, solicitante.Id, payload)
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("erro = %v, esperado ErrValidacao", err)
		}
	})

	t.Run("minhas solicitações são só do próprio solicitante", func(t *testing.T) {
		pagina, err := svc.ListarMinhasSolicitacoes(ctx, tenantID, solicitante.Id, 1, nil, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if pagina.Total != 2 {
			t.Errorf("total = %d, esperado 2 (maquinário + reparo)", pagina.Total)
		}

		vazia, err := svc.ListarMinhasSolicitacoes(ctx, tenantID, solicitanteOutroSetor.Id, 1, nil, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if vazia.Total != 0 {
			t.Errorf("solicitante de outro setor não devia ter solicitação nenhuma, total = %d", vazia.Total)
		}
	})

	t.Run("fila do gestor recortada por escopo", func(t *testing.T) {
		doGestor, err := svc.ListarSolicitacoes(ctx, tenantID, gestorParcial.Id, "gestor", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(doGestor) != 2 {
			t.Errorf("gestor da Padaria devia ver as 2 solicitações do setor A, viu %d", len(doGestor))
		}

		doAdmin, err := svc.ListarSolicitacoes(ctx, tenantID, admin.Id, "administrador", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("erro ao listar: %v", err)
		}
		if len(doAdmin) != 2 {
			t.Errorf("administrador devia ver o tenant inteiro, viu %d", len(doAdmin))
		}
	})

	t.Run("obter por id respeita o escopo -- fora dele é 404, não 403", func(t *testing.T) {
		_, err := svc.ObterSolicitacao(ctx, tenantID, solicitanteOutroSetor.Id, "solicitante", solicitacaoMaquinarioID)
		if !errors.Is(err, helper.ErrNaoEncontrado) {
			t.Errorf("erro = %v, esperado ErrNaoEncontrado", err)
		}

		_, err = svc.ObterSolicitacao(ctx, tenantID, gestorParcial.Id, "gestor", solicitacaoMaquinarioID)
		if err != nil {
			t.Errorf("gestor do mesmo setor devia enxergar, erro = %v", err)
		}
	})

	t.Run("resumo conta a pendente como aberta", func(t *testing.T) {
		resumo, err := svc.ObterResumo(ctx, tenantID, solicitante.Id)
		if err != nil {
			t.Fatalf("erro ao obter resumo: %v", err)
		}
		if resumo.Abertas != 2 || resumo.EmAndamento != 0 || resumo.Concluidas != 0 {
			t.Errorf("resumo = %+v, esperado {2 0 0}", resumo)
		}
	})

	t.Run("abrir-os recusa usuário que não é técnico", func(t *testing.T) {
		_, err := svc.AbrirOS(ctx, tenantID, gestorParcial.Id, "gestor", solicitacaoMaquinarioID, model.AberturaOrdemServicoPayload{
			Urgencia: "Alta", TecnicoId: naoTecnico.Id,
		})
		if !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("erro = %v, esperado ErrConflitoIntegridade", err)
		}
	})

	t.Run("abrir-os aprova e converte a solicitação", func(t *testing.T) {
		os, err := svc.AbrirOS(ctx, tenantID, gestorParcial.Id, "gestor", solicitacaoMaquinarioID, model.AberturaOrdemServicoPayload{
			Urgencia: "Alta", TecnicoId: tecnico.Id,
		})
		if err != nil {
			t.Fatalf("erro ao abrir OS: %v", err)
		}
		if os.SolicitacaoId != solicitacaoMaquinarioID || os.TecnicoId != tecnico.Id || os.Urgencia != "Alta" {
			t.Errorf("OS com forma errada: %+v", os)
		}
		if !os.AfetaProducao {
			t.Error("afetaProducao devia ser true -- a solicitação tem o marcador")
		}
		if os.StatusExecucao != "Aberta" || os.Finalizada {
			t.Errorf("statusExecucao/finalizada = %s/%v, esperado Aberta/false", os.StatusExecucao, os.Finalizada)
		}

		convertida, err := svc.ObterSolicitacao(ctx, tenantID, admin.Id, "administrador", solicitacaoMaquinarioID)
		if err != nil {
			t.Fatalf("erro ao reler solicitação: %v", err)
		}
		if convertida.Status != "Convertida" {
			t.Errorf("status = %s, esperado Convertida", convertida.Status)
		}

		resumo, err := svc.ObterResumo(ctx, tenantID, solicitante.Id)
		if err != nil {
			t.Fatalf("erro ao obter resumo: %v", err)
		}
		if resumo.Abertas != 2 {
			t.Errorf("abertas = %d, esperado 2 (a nova OS ainda está Aberta)", resumo.Abertas)
		}
	})

	t.Run("abrir-os de novo na mesma solicitação é recusado", func(t *testing.T) {
		_, err := svc.AbrirOS(ctx, tenantID, gestorParcial.Id, "gestor", solicitacaoMaquinarioID, model.AberturaOrdemServicoPayload{
			Urgencia: "Baixa", TecnicoId: tecnico.Id,
		})
		if !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("erro = %v, esperado ErrConflitoIntegridade", err)
		}
	})

	t.Run("rejeitar encerra a solicitação com motivo", func(t *testing.T) {
		rejeitada, err := svc.Rejeitar(ctx, tenantID, gestorParcial.Id, "gestor", solicitacaoReparoID, "Já foi resolvido por outra via")
		if err != nil {
			t.Fatalf("erro ao rejeitar: %v", err)
		}
		if rejeitada.Status != "Rejeitada" {
			t.Errorf("status = %s, esperado Rejeitada", rejeitada.Status)
		}
		if rejeitada.MotivoRejeicao == nil || *rejeitada.MotivoRejeicao != "Já foi resolvido por outra via" {
			t.Errorf("motivoRejeicao = %v", rejeitada.MotivoRejeicao)
		}
		if rejeitada.RejeitadoPorNome == nil || *rejeitada.RejeitadoPorNome != "Carla" {
			t.Errorf("rejeitadoPorNome = %v, esperado Carla", rejeitada.RejeitadoPorNome)
		}
	})

	t.Run("rejeitar de novo é recusado", func(t *testing.T) {
		_, err := svc.Rejeitar(ctx, tenantID, gestorParcial.Id, "gestor", solicitacaoReparoID, "motivo qualquer")
		if !errors.Is(err, helper.ErrConflitoIntegridade) {
			t.Errorf("erro = %v, esperado ErrConflitoIntegridade", err)
		}
	})

	t.Run("motivo em branco é recusado", func(t *testing.T) {
		// Reaproveita solicitacaoMaquinarioID mesmo já Convertida: a validação
		// de motivo vazio roda ANTES da checagem de status, então não precisa
		// de mais uma solicitação Pendente só pra este caso.
		_, err := svc.Rejeitar(ctx, tenantID, gestorParcial.Id, "gestor", solicitacaoMaquinarioID, "   ")
		if !errors.Is(err, helper.ErrValidacao) {
			t.Errorf("erro = %v, esperado ErrValidacao", err)
		}
	})
}
