package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// TamanhoPaginaSolicitacoes é o tamanho de página de GET /solicitacoes/minhas
// -- mesmo padrão de TamanhoPaginaUsuarios (loginService.go).
const TamanhoPaginaSolicitacoes = 10

type SolicitacaoService struct {
	Pool *pgxpool.Pool
	// Notificador é opcional -- campo público setado depois da construção
	// (router.go), não parâmetro do construtor: mudar a assinatura de
	// NewRepoSolicitacao quebraria todo teste que já chama
	// NewRepoSolicitacao(pool) direto (solicitacaoOsIntegracao_test.go). nil
	// (o zero value, o que todo teste existente continua recebendo) significa
	// "não notifica" -- ver o cheque em notificar.
	Notificador NotificadorInterface
}

func NewRepoSolicitacao(pool *pgxpool.Pool) *SolicitacaoService {

	return &SolicitacaoService{
		Pool: pool,
	}
}

// notificar avisa os gestores do setor por WhatsApp que uma solicitação nova
// chegou -- fora da transação (já commitada, resposta já montada) e em
// goroutine própria: falha de rede/Evolution API não pode atrasar nem
// derrubar a resposta HTTP pro Solicitante que acabou de criar o pedido.
func (s *SolicitacaoService) notificar(tenantId int64, sol model.SolicitacaoOS) {

	if s.Notificador == nil {
		return
	}

	dados := DadosNotificacao{
		Alvo:            alvoDaSolicitacao(sol),
		Descricao:       sol.Descricao,
		LojaNome:        sol.LojaNome,
		SetorNome:       sol.SetorNome,
		SolicitanteNome: sol.SolicitanteNome,
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := s.Notificador.NotificarNovaSolicitacao(ctx, tenantId, sol.SetorId, dados); err != nil {
			log.Printf("notificar solicitação %d: %v", sol.Id, err)
		}
	}()
}

// CadastrarSolicitacaoMaquinario é POST /solicitacoes/maquinario. Foto e
// vídeo já subiram pro R2 (payload.FotoChave/.VideoChave -- o controller faz
// isso ANTES de chamar o service, mesmo padrão de MaquinaController.chaveDaFoto):
// falhar o upload não deixa resíduo no banco.
func (s *SolicitacaoService) CadastrarSolicitacaoMaquinario(ctx context.Context, tenantId, solicitanteId int64, payload model.NovaSolicitacaoMaquinarioPayload) (model.SolicitacaoOS, error) {

	descricao, err := campoObrigatorio("descrição", payload.Descricao)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}

	marcadores := make([]repository.MarcadorImpacto, 0, len(payload.Impactos))
	for _, item := range payload.Impactos {
		marcador, err := marcadorValido(item)
		if err != nil {
			return model.SolicitacaoOS{}, err
		}
		marcadores = append(marcadores, marcador)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.SolicitacaoOS{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	setorSolicitante, err := resolverSetorSolicitante(ctx, repo, solicitanteId)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}

	maquina, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{ID: payload.MaquinaId, TenantID: tenantId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.SolicitacaoOS{}, fmt.Errorf("%w: máquina %d não existe neste tenant", helper.ErrConflitoIntegridade, payload.MaquinaId)
		}
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}
	if !maquina.Ativa {
		return model.SolicitacaoOS{}, fmt.Errorf("%w: máquina %d está desativada", helper.ErrConflitoIntegridade, payload.MaquinaId)
	}
	// O Solicitante só abre solicitação de máquina do próprio setor -- GET
	// /maquinas já filtra o dropdown assim (back-end/CLAUDE.md, "GET
	// /maquinas e GET /preventivas..."); isto aplica a mesma regra do lado da
	// escrita, contra um POST direto escolhendo id de outro setor.
	if maquina.SetorID != setorSolicitante {
		return model.SolicitacaoOS{}, fmt.Errorf("%w: máquina não pertence ao seu setor", helper.ErrValidacao)
	}

	criada, err := repo.CriarSolicitacaoMaquinario(ctx, repository.CriarSolicitacaoMaquinarioParams{
		TenantID:      tenantId,
		MaquinaID:     &payload.MaquinaId,
		SetorID:       maquina.SetorID,
		SolicitanteID: &solicitanteId,
		Descricao:     descricao,
	})
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	anexos := []anexoNovo{{Tipo: repository.TipoAnexoFoto, Chave: payload.FotoChave, MimeType: payload.FotoMime, TamanhoBytes: payload.FotoTamanho}}
	if payload.VideoChave != nil {
		anexos = append(anexos, anexoNovo{Tipo: repository.TipoAnexoVideo, Chave: *payload.VideoChave, MimeType: *payload.VideoMime, TamanhoBytes: *payload.VideoTamanho})
	}

	if err := gravarImpactosEAnexos(ctx, repo, criada.ID, marcadores, anexos); err != nil {
		return model.SolicitacaoOS{}, err
	}

	resposta, err := concluirSolicitacao(ctx, tx, repo, tenantId, criada.ID)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}
	s.notificar(tenantId, resposta)

	return resposta, nil
}

// CadastrarSolicitacaoReparo é POST /solicitacoes/reparo. Sem impactos (a
// tela de Pequeno Reparo nem oferece o marcador) e setor sempre o do próprio
// solicitante -- reparo não tem máquina de onde derivar.
func (s *SolicitacaoService) CadastrarSolicitacaoReparo(ctx context.Context, tenantId, solicitanteId int64, payload model.NovaSolicitacaoReparoPayload) (model.SolicitacaoOS, error) {

	item, err := campoObrigatorio("item", payload.Item)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}
	descricao, err := campoObrigatorio("descrição", payload.Descricao)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.SolicitacaoOS{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	setorSolicitante, err := resolverSetorSolicitante(ctx, repo, solicitanteId)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}

	criada, err := repo.CriarSolicitacaoReparo(ctx, repository.CriarSolicitacaoReparoParams{
		TenantID:      tenantId,
		ItemDescricao: &item,
		SetorID:       setorSolicitante,
		SolicitanteID: &solicitanteId,
		Descricao:     descricao,
	})
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	anexos := []anexoNovo{{Tipo: repository.TipoAnexoFoto, Chave: payload.FotoChave, MimeType: payload.FotoMime, TamanhoBytes: payload.FotoTamanho}}
	if err := gravarImpactosEAnexos(ctx, repo, criada.ID, nil, anexos); err != nil {
		return model.SolicitacaoOS{}, err
	}

	resposta, err := concluirSolicitacao(ctx, tx, repo, tenantId, criada.ID)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}
	s.notificar(tenantId, resposta)

	return resposta, nil
}

// ListarMinhasSolicitacoes é GET /solicitacoes/minhas -- restrita ao próprio
// solicitante (nunca escopo: é a lista pessoal, não a fila do gestor).
func (s *SolicitacaoService) ListarMinhasSolicitacoes(ctx context.Context, tenantId, solicitanteId int64, pagina int32, status, busca *string) (model.RespostaPaginada[model.SolicitacaoOS], error) {

	var vazio model.RespostaPaginada[model.SolicitacaoOS]

	if pagina < 1 {
		pagina = 1
	}

	repo := repository.New(s.Pool)

	total, err := repo.ContarSolicitacoesDoSolicitante(ctx, repository.ContarSolicitacoesDoSolicitanteParams{
		TenantID:      tenantId,
		SolicitanteID: &solicitanteId,
		Status:        (*repository.StatusSolicitacao)(status),
		Busca:         busca,
	})
	if err != nil {
		return vazio, helper.TraduzErroPostgres(err)
	}

	linhas, err := repo.ListarSolicitacoesDoSolicitante(ctx, repository.ListarSolicitacoesDoSolicitanteParams{
		TenantID:      tenantId,
		SolicitanteID: &solicitanteId,
		Status:        (*repository.StatusSolicitacao)(status),
		Busca:         busca,
		Limite:        TamanhoPaginaSolicitacoes,
		Deslocamento:  (pagina - 1) * TamanhoPaginaSolicitacoes,
	})
	if err != nil {
		return vazio, helper.TraduzErroPostgres(err)
	}

	convertidas := make([]repository.ObterSolicitacaoPorIDRow, len(linhas))
	for i, l := range linhas {
		convertidas[i] = repository.ObterSolicitacaoPorIDRow(l)
	}

	dados, err := montarSolicitacoesEmLote(ctx, repo, convertidas)
	if err != nil {
		return vazio, err
	}

	return model.RespostaPaginada[model.SolicitacaoOS]{
		Dados:        dados,
		Pagina:       pagina,
		TotalPaginas: int32((total + TamanhoPaginaSolicitacoes - 1) / TamanhoPaginaSolicitacoes),
		Total:        total,
	}, nil
}

// ListarSolicitacoes é GET /solicitacoes -- a fila do Gestor (e Administrador
// e Técnico), array simples sem paginação (o front pagina no cliente, mesmo
// padrão de ListarMaquinas/ListarPreventivas). Recortada pelo escopo de quem
// chama, igual às duas.
func (s *SolicitacaoService) ListarSolicitacoes(ctx context.Context, tenantId, usuarioId int64, perfil string, status, tipo, busca *string, lojaId *int64) ([]model.SolicitacaoOS, error) {

	repo := repository.New(s.Pool)

	linhas, err := repo.ListarSolicitacoes(ctx, repository.ListarSolicitacoesParams{
		TenantID:        tenantId,
		Status:          (*repository.StatusSolicitacao)(status),
		Tipo:            (*repository.TipoSolicitacao)(tipo),
		LojaID:          lojaId,
		Busca:           busca,
		EscopoUsuarioID: escopoDe(usuarioId, perfil),
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	convertidas := make([]repository.ObterSolicitacaoPorIDRow, len(linhas))
	for i, l := range linhas {
		convertidas[i] = repository.ObterSolicitacaoPorIDRow(l)
	}

	return montarSolicitacoesEmLote(ctx, repo, convertidas)
}

// ObterSolicitacao é GET /solicitacoes/:id -- aberta a qualquer perfil
// autenticado, recortada pelo escopo de quem chama: sem isso um Solicitante
// enumerando ids leria foto e descrição de outro setor. Fora do escopo cai em
// pgx.ErrNoRows, igual a id inexistente -- 404, nunca 403.
func (s *SolicitacaoService) ObterSolicitacao(ctx context.Context, tenantId, usuarioId int64, perfil string, id int64) (model.SolicitacaoOS, error) {

	repo := repository.New(s.Pool)

	obtida, err := repo.ObterSolicitacaoPorID(ctx, repository.ObterSolicitacaoPorIDParams{
		ID:              id,
		TenantID:        tenantId,
		EscopoUsuarioID: escopoDe(usuarioId, perfil),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.SolicitacaoOS{}, helper.ErrNaoEncontrado
		}
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	impactos, err := repo.ObterImpactosDaSolicitacao(ctx, id)
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	anexos, err := repo.ObterAnexosDaSolicitacao(ctx, id)
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}

	return model.MontarSolicitacao(obtida, impactos, anexos), nil
}

// ObterResumo é GET /solicitacoes/resumo -- os três contadores da Home do
// Solicitante, sempre do próprio (nunca escopo: mesmo motivo de
// ListarMinhasSolicitacoes).
func (s *SolicitacaoService) ObterResumo(ctx context.Context, tenantId, solicitanteId int64) (model.ResumoSolicitacoes, error) {

	repo := repository.New(s.Pool)

	resumo, err := repo.ObterResumoSolicitacoes(ctx, repository.ObterResumoSolicitacoesParams{
		TenantID:      tenantId,
		SolicitanteID: &solicitanteId,
	})
	if err != nil {
		return model.ResumoSolicitacoes{}, helper.TraduzErroPostgres(err)
	}

	return model.ResumoSolicitacoes{
		Abertas:     resumo.Abertas,
		EmAndamento: resumo.EmAndamento,
		Concluidas:  resumo.Concluidas,
	}, nil
}

// AbrirOS é POST /solicitacoes/:id/abrir-os -- a aprovação do Gestor: define
// técnico e urgência e nasce a OrdemServico, na mesma transação que fecha a
// solicitação como Convertida (as duas juntas ou nenhuma: separadas, uma OS
// sem a solicitação convertida deixaria a preventiva/o pedido reabrível por
// engano).
func (s *SolicitacaoService) AbrirOS(ctx context.Context, tenantId, atorId int64, perfil string, solicitacaoId int64, payload model.AberturaOrdemServicoPayload) (model.OrdemServico, error) {

	urgencia := repository.NivelUrgencia(payload.Urgencia) // 'Baixa'/'Média'/'Alta' já garantidos pelo binding oneof

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	atual, err := repo.ObterSolicitacaoPorID(ctx, repository.ObterSolicitacaoPorIDParams{
		ID:              solicitacaoId,
		TenantID:        tenantId,
		EscopoUsuarioID: escopoDe(atorId, perfil),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, helper.ErrNaoEncontrado
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	// O `AND status = 'Pendente'` na query de baixo é a rede contra corrida
	// (duas abas do mesmo Gestor); esta checagem aqui só existe pra dar um
	// erro que diz o que aconteceu, em vez de "0 linhas" genérico -- mesmo
	// papel do NOT EXISTS em ListarPreventivasVencidas.
	if atual.Status != repository.StatusSolicitacaoPendente {
		return model.OrdemServico{}, fmt.Errorf("%w: solicitação já está %s", helper.ErrConflitoIntegridade, atual.Status)
	}

	tecnico, err := repo.ObterUsuarioPorID(ctx, repository.ObterUsuarioPorIDParams{ID: payload.TecnicoId, TenantID: tenantId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrdemServico{}, fmt.Errorf("%w: técnico %d não existe neste tenant", helper.ErrConflitoIntegridade, payload.TecnicoId)
		}
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}
	if tecnico.Perfil != repository.PerfilUsuarioTecnico || !tecnico.Ativo {
		return model.OrdemServico{}, fmt.Errorf("%w: usuário %d não é um técnico ativo", helper.ErrConflitoIntegridade, payload.TecnicoId)
	}

	impactos, err := repo.ObterImpactosDaSolicitacao(ctx, solicitacaoId)
	if err != nil {
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}
	afetaProducao := slices.Contains(impactos, repository.MarcadorImpactoAfetaProduo)

	tipoOs, err := tipoOsDaSolicitacao(atual.Tipo)
	if err != nil {
		return model.OrdemServico{}, err
	}

	os, err := repo.CriarOrdemServicoDeSolicitacao(ctx, repository.CriarOrdemServicoDeSolicitacaoParams{
		TenantID:      tenantId,
		SolicitacaoID: solicitacaoId,
		Tipo:          tipoOs,
		TecnicoID:     payload.TecnicoId,
		Urgencia:      urgencia,
		AbertaPorID:   atorId,
		AfetaProducao: afetaProducao,
	})
	if err != nil {
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}

	linhas, err := repo.MarcarSolicitacaoConvertida(ctx, repository.MarcarSolicitacaoConvertidaParams{ID: solicitacaoId, TenantID: tenantId})
	if err != nil {
		return model.OrdemServico{}, helper.TraduzErroPostgres(err)
	}
	if linhas == 0 {
		return model.OrdemServico{}, fmt.Errorf("%w: solicitação já não está mais pendente", helper.ErrConflitoIntegridade)
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrdemServico{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return model.MontarOrdemServicoDaAbertura(os, atual, string(urgencia), payload.TecnicoId, afetaProducao), nil
}

// Rejeitar é POST /solicitacoes/:id/rejeitar -- encerra a solicitação sem
// abrir OS. Mesmo esqueleto de AbrirOS (lê, checa Pendente, escreve, relê),
// mas termina em concluirSolicitacao porque a resposta aqui é SolicitacaoOS,
// não OrdemServico -- não nasce OS nenhuma.
func (s *SolicitacaoService) Rejeitar(ctx context.Context, tenantId, atorId int64, perfil string, solicitacaoId int64, motivoBruto string) (model.SolicitacaoOS, error) {

	motivo, err := campoObrigatorio("motivo", motivoBruto)
	if err != nil {
		return model.SolicitacaoOS{}, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.SolicitacaoOS{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	atual, err := repo.ObterSolicitacaoPorID(ctx, repository.ObterSolicitacaoPorIDParams{
		ID:              solicitacaoId,
		TenantID:        tenantId,
		EscopoUsuarioID: escopoDe(atorId, perfil),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.SolicitacaoOS{}, helper.ErrNaoEncontrado
		}
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}
	if atual.Status != repository.StatusSolicitacaoPendente {
		return model.SolicitacaoOS{}, fmt.Errorf("%w: solicitação já está %s", helper.ErrConflitoIntegridade, atual.Status)
	}

	linhas, err := repo.RejeitarSolicitacao(ctx, repository.RejeitarSolicitacaoParams{
		ID:             solicitacaoId,
		TenantID:       tenantId,
		MotivoRejeicao: &motivo,
		RejeitadoPorID: &atorId,
	})
	if err != nil {
		return model.SolicitacaoOS{}, helper.TraduzErroPostgres(err)
	}
	if linhas == 0 {
		return model.SolicitacaoOS{}, fmt.Errorf("%w: solicitação já não está mais pendente", helper.ErrConflitoIntegridade)
	}

	return concluirSolicitacao(ctx, tx, repo, tenantId, solicitacaoId)
}
