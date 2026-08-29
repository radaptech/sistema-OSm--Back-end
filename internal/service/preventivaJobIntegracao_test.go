package service

import (
	"context"
	"testing"
	"time"
)

// TestAbrirSolicitacoesDePreventivasVencidas cobre o job que abre solicitação
// automática a partir de preventiva vencida. Integração e não unitário porque
// tudo que pode dar errado aqui é do banco: os CHECKs que definem a forma da
// solicitação automática (ck_origem, ck_solicitacao_alvo), o trigger DEFERRABLE
// que exigia foto até a migration 000005, e o índice parcial
// uq_preventiva_pendente, que é quem garante "uma preventiva não tem duas
// solicitações pendentes ao mesmo tempo".
//
// Os subtestes compartilham estado de propósito e rodam em ordem: "não duplica"
// só faz sentido depois de "abre", e "reabre no ciclo seguinte" só depois das
// duas.
func TestAbrirSolicitacoesDePreventivasVencidas(t *testing.T) {

	ctx := context.Background()
	pool := bancoDeTeste(t)
	svc := NewRepoPreventiva(pool)

	// Tenant com duas máquinas -- uma ativa e uma desativada -- para provar que
	// desativar máquina para o job. DesativarMaquina não desativa as preventivas
	// dela, então sem o m.ativa da query a máquina morta abriria solicitação a
	// cada ciclo, para sempre, sem jeito de parar pela API.
	var tenantID, lojaID, setorID, maquinaAtiva, maquinaInativa int64
	if err := pool.QueryRow(ctx, `INSERT INTO empresa (subdominio, nome) VALUES ('teste', 'Empresa Teste') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("erro ao criar empresa: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO loja (tenant_id, nome) VALUES ($1, 'Loja A') RETURNING id`, tenantID).Scan(&lojaID); err != nil {
		t.Fatalf("erro ao criar loja: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO setor (tenant_id, loja_id, nome) VALUES ($1, $2, 'Padaria') RETURNING id`, tenantID, lojaID).Scan(&setorID); err != nil {
		t.Fatalf("erro ao criar setor: %v", err)
	}
	for _, m := range []struct {
		patrimonio string
		ativa      bool
		dest       *int64
	}{{"P-001", true, &maquinaAtiva}, {"P-002", false, &maquinaInativa}} {
		if err := pool.QueryRow(ctx,
			`INSERT INTO maquina (tenant_id, setor_id, numero_patrimonio, nome, criticidade, ativa)
			 VALUES ($1, $2, $3, $3, 'Alta', $4) RETURNING id`,
			tenantID, setorID, m.patrimonio, m.ativa).Scan(m.dest); err != nil {
			t.Fatalf("erro ao criar máquina %s: %v", m.patrimonio, err)
		}
	}

	// Só a primeira deve gerar solicitação. As outras três são cada um dos
	// filtros da query, uma por motivo.
	const intervalo = 30
	const diasAtraso = 5
	var vencida int64
	for _, p := range []struct {
		descricao  string
		maquina    int64
		diasFrente int
		ativa      bool
		dest       *int64
	}{
		{"Troca de óleo", maquinaAtiva, -diasAtraso, true, &vencida},
		{"Ainda não venceu", maquinaAtiva, +1, true, nil},
		{"Vencida mas desabilitada", maquinaAtiva, -diasAtraso, false, nil},
		{"Vencida em máquina desativada", maquinaInativa, -diasAtraso, true, nil},
	} {
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO preventiva (tenant_id, maquina_id, descricao, intervalo_dias, proxima_data, ativa)
			 VALUES ($1, $2, $3, $4, CURRENT_DATE + $5::int, $6) RETURNING id`,
			tenantID, p.maquina, p.descricao, intervalo, p.diasFrente, p.ativa).Scan(&id); err != nil {
			t.Fatalf("erro ao criar preventiva %q: %v", p.descricao, err)
		}
		if p.dest != nil {
			*p.dest = id
		}
	}

	contarSolicitacoes := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM solicitacao_os`).Scan(&n); err != nil {
			t.Fatalf("erro ao contar solicitações: %v", err)
		}
		return n
	}

	t.Run("abre solicitação só para a preventiva vencida de máquina ativa", func(t *testing.T) {
		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err != nil {
			t.Fatalf("job devolveu erro: %v", err)
		}
		if criadas != 1 {
			t.Fatalf("criadas = %d, esperado 1 (futura, desabilitada e de máquina inativa não podem gerar)", criadas)
		}
		if n := contarSolicitacoes(); n != 1 {
			t.Fatalf("solicitações no banco = %d, esperado 1", n)
		}
	})

	t.Run("a solicitação nasce na forma que o Gestor espera", func(t *testing.T) {
		var (
			tipo, status, origem, descricao string
			solicitanteID, itemDescricao    *string
			maquinaID, setorLinha           int64
			preventivaID                    int64
			anexos                          int
		)
		if err := pool.QueryRow(ctx, `
			SELECT s.tipo, s.status, s.origem, s.descricao, s.solicitante_id::text, s.item_descricao,
			       s.maquina_id, s.setor_id, s.preventiva_id,
			       (SELECT count(*) FROM solicitacao_anexo a WHERE a.solicitacao_id = s.id)
			  FROM solicitacao_os s`).
			Scan(&tipo, &status, &origem, &descricao, &solicitanteID, &itemDescricao,
				&maquinaID, &setorLinha, &preventivaID, &anexos); err != nil {
			t.Fatalf("erro ao ler solicitação: %v", err)
		}

		// origem/solicitante_id e tipo/item_descricao não são preferência: são
		// ck_origem e ck_solicitacao_alvo. O banco recusaria qualquer outra
		// combinação -- o que este subteste tranca é o job não ter parado de
		// mandar a combinação certa.
		if tipo != "maquinario" || itemDescricao != nil {
			t.Errorf("tipo = %q, item_descricao = %v; esperado maquinario sem texto livre", tipo, itemDescricao)
		}
		if origem != "preventiva" || solicitanteID != nil {
			t.Errorf("origem = %q, solicitante_id = %v; esperado preventiva sem solicitante", origem, solicitanteID)
		}
		if status != "Pendente" {
			t.Errorf("status = %q, esperado Pendente -- a OS só nasce quando o Gestor aprova", status)
		}
		if maquinaID != maquinaAtiva || preventivaID != vencida {
			t.Errorf("apontou para máquina %d / preventiva %d, esperado %d / %d", maquinaID, preventivaID, maquinaAtiva, vencida)
		}
		// setor_id é NOT NULL e a solicitação não guarda loja -- ela sai via setor.
		if setorLinha != setorID {
			t.Errorf("setor_id = %d, esperado %d (o setor da máquina)", setorLinha, setorID)
		}
		if descricao != "Manutenção preventiva: Troca de óleo" {
			t.Errorf("descricao = %q", descricao)
		}
		// Migration 000005: sem ela o COMMIT falharia aqui, não o INSERT.
		if anexos != 0 {
			t.Errorf("anexos = %d, esperado 0 -- ninguém fotografou nada", anexos)
		}
	})

	t.Run("proxima_data avança a partir da data vencida, não de hoje", func(t *testing.T) {
		var proxima time.Time
		if err := pool.QueryRow(ctx, `SELECT proxima_data FROM preventiva WHERE id = $1`, vencida).Scan(&proxima); err != nil {
			t.Fatalf("erro ao ler proxima_data: %v", err)
		}

		// Vencida há 5 dias com intervalo de 30 vai para hoje+25, não hoje+30:
		// contar a partir de hoje faria um ciclo processado com atraso arrastar
		// todos os seguintes.
		var esperado time.Time
		if err := pool.QueryRow(ctx, `SELECT CURRENT_DATE + $1::int`, intervalo-diasAtraso).Scan(&esperado); err != nil {
			t.Fatalf("erro ao calcular data esperada: %v", err)
		}
		if !proxima.Equal(esperado) {
			t.Errorf("proxima_data = %s, esperado %s (hoje+%d, não hoje+%d)",
				proxima.Format("2006-01-02"), esperado.Format("2006-01-02"), intervalo-diasAtraso, intervalo)
		}
	})

	t.Run("segunda execução não duplica", func(t *testing.T) {
		// Rodar o cron duas vezes (duas réplicas, dois disparos colados) não
		// pode render duas solicitações. Aqui quem segura ainda é só a data já
		// avançada pela execução anterior -- o subteste seguinte tira essa
		// muleta.
		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err != nil {
			t.Fatalf("job devolveu erro na segunda execução: %v", err)
		}
		if criadas != 0 {
			t.Errorf("criadas = %d, esperado 0", criadas)
		}
		if n := contarSolicitacoes(); n != 1 {
			t.Errorf("solicitações no banco = %d, esperado continuar 1", n)
		}
	})

	t.Run("mesma preventiva vencida de novo, com a anterior pendente, é barrada", func(t *testing.T) {
		// Sem a muleta da data: a preventiva volta a vencer com a solicitação
		// anterior ainda Pendente. Duas defesas cobrem este caso e o teste passa
		// com qualquer uma das duas -- o NOT EXISTS da query a exclui, e se ele
		// não existisse o índice recusaria o INSERT com 23505, que o service
		// trata como benigno. Não é um trap de mutação para o NOT EXISTS
		// (ver o comentário dele em preventiva.sql); é a garantia de que a
		// combinação das duas nunca gera a segunda pendente.
		if _, err := pool.Exec(ctx, `UPDATE preventiva SET proxima_data = CURRENT_DATE - 1 WHERE id = $1`, vencida); err != nil {
			t.Fatalf("erro ao regredir proxima_data: %v", err)
		}
		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err != nil {
			t.Fatalf("job devolveu erro: %v", err)
		}
		if criadas != 0 || contarSolicitacoes() != 1 {
			t.Errorf("criadas = %d, solicitações = %d; esperado 0 e 1 -- uma preventiva não tem duas pendentes", criadas, contarSolicitacoes())
		}
	})

	t.Run("ciclo seguinte reabre depois que o Gestor resolve a pendente", func(t *testing.T) {
		// A trava é "duas Pendentes ao mesmo tempo", não "uma por preventiva
		// para sempre": resolvida a primeira, o próximo vencimento gera outra.
		if _, err := pool.Exec(ctx, `UPDATE solicitacao_os SET status = 'Convertida' WHERE preventiva_id = $1`, vencida); err != nil {
			t.Fatalf("erro ao converter solicitação: %v", err)
		}
		criadas, err := svc.AbrirSolicitacoesDePreventivasVencidas(ctx)
		if err != nil {
			t.Fatalf("job devolveu erro: %v", err)
		}
		if criadas != 1 {
			t.Errorf("criadas = %d, esperado 1 -- a preventiva gera uma solicitação por ciclo", criadas)
		}
		if n := contarSolicitacoes(); n != 2 {
			t.Errorf("solicitações no banco = %d, esperado 2", n)
		}
	})
}
