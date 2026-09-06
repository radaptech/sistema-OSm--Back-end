package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type PreventivaService struct {
	Pool *pgxpool.Pool
	// Notificador é opcional -- campo público setado depois da construção
	// (router.go), não parâmetro do construtor: mudar a assinatura de
	// NewRepoPreventiva quebraria todo teste que já chama NewRepoPreventiva(pool)
	// direto (preventivaIntegracao_test.go, preventivaJobIntegracao_test.go).
	// nil (o zero value, é o que todo teste existente continua recebendo)
	// significa "não notifica" -- ver o cheque em notificarPreventivaVencida.
	Notificador NotificadorInterface
}

func NewRepoPreventiva(pool *pgxpool.Pool) *PreventivaService {

	return &PreventivaService{
		Pool: pool,
	}
}

// validarTecnicoPreventiva confere que o técnico escolhido para a preventiva
// existe no tenant, ainda tem perfil técnico e ainda está ativo.
//
// Existe porque a FK (tenant_id, tecnico_id) -> usuario garante só a primeira
// das três: AtualizarUsuario promove um técnico a gestor sem tocar nas
// preventivas dele, e DesativarUsuario não as toca tampouco. Sem este cheque a
// preventiva ficaria apontando para alguém que não atende mais, e o erro só
// apareceria meses depois, dentro do cron -- longe de quem poderia consertar.
// É o mesmo cheque que AbrirOS faz no técnico que o Gestor escolhe.
//
// Recebe *repository.Queries e não o Pool, mesmo motivo de gravarPreventivas:
// precisa rodar dentro da transação de quem chama.
func validarTecnicoPreventiva(ctx context.Context, repo *repository.Queries, tenantID, tecnicoID int64) error {

	if tecnicoID <= 0 {
		return fmt.Errorf("%w: escolha o técnico responsável pela preventiva", helper.ErrValidacao)
	}

	tecnico, err := repo.ObterUsuarioPorID(ctx, repository.ObterUsuarioPorIDParams{ID: tecnicoID, TenantID: tenantID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: técnico %d não existe neste tenant", helper.ErrConflitoIntegridade, tecnicoID)
		}
		return helper.TraduzErroPostgres(err)
	}
	if tecnico.Perfil != repository.PerfilUsuarioTecnico || !tecnico.Ativo {
		return fmt.Errorf("%w: usuário %d não é um técnico ativo", helper.ErrConflitoIntegridade, tecnicoID)
	}

	return nil
}

// gravarPreventivas insere a lista de preventivas de uma máquina.
//
// Recebe *repository.Queries e não o Pool de propósito: é assim que
// CadastrarMaquina/AtualizarMaquina conseguem chamá-la de dentro da transação
// que já abriram -- máquina e preventivas gravam juntas ou não gravam. Um
// método de PreventivaService abriria transação própria e quebraria isso.
// Mesmo padrão de gravarEscopo em EscopoPerfilService.go.
//
// maquinaID vem de quem chama, não de p.MaquinaId: no cadastro a máquina acaba
// de ser inserida e o front mandou 0 no payload.
func gravarPreventivas(ctx context.Context, repo *repository.Queries, tenantID, maquinaID int64, preventivas []model.PreventivaPayload) error {

	// A regra "máquina exige ao menos uma preventiva" é do servidor, não só do
	// Zod: sem isto um POST direto (Postman) cria máquina sem preventiva
	// nenhuma e a regra de negócio passa a existir só no navegador.
	if len(preventivas) == 0 {
		return fmt.Errorf("%w: a máquina precisa de pelo menos uma manutenção preventiva", helper.ErrValidacao)
	}

	for _, p := range preventivas {
		descricao := strings.TrimSpace(p.Descricao)
		if descricao == "" {
			return fmt.Errorf("%w: a descrição da preventiva não pode ficar em branco", helper.ErrValidacao)
		}
		// ck_intervalo (intervalo_dias > 0) recusaria no banco, mas como 422; o
		// valor veio do formulário, então é erro de preenchimento (400).
		if p.IntervaloDias <= 0 {
			return fmt.Errorf("%w: o intervalo da preventiva deve ser de pelo menos 1 dia", helper.ErrValidacao)
		}
		if p.ProximaData == nil || p.ProximaData.IsZero() {
			return fmt.Errorf("%w: informe a próxima data da preventiva", helper.ErrValidacao)
		}
		if err := validarTecnicoPreventiva(ctx, repo, tenantID, p.TecnicoId); err != nil {
			return err
		}

		// tecnicoId é ponteiro na query (coluna nullable, migration 000008) mas
		// nunca nulo aqui: validarTecnicoPreventiva já recusou o zero acima.
		tecnicoID := p.TecnicoId
		_, err := repo.CriarPreventiva(ctx, repository.CriarPreventivaParams{
			TenantID:      tenantID,
			MaquinaID:     maquinaID,
			Descricao:     descricao,
			IntervaloDias: p.IntervaloDias,
			ProximaData:   pgtype.Date{Time: p.ProximaData.Time(), Valid: true},
			Ativa:         p.Ativa,
			TecnicoID:     &tecnicoID,
		})
		if err != nil {
			return helper.TraduzErroPostgres(err)
		}
	}

	return nil
}

// CadastrarPreventiva é POST /preventivas -- a preventiva avulsa, criada fora
// do formulário de máquina. Roda em transação porque gravarPreventivas é
// compartilhada com o caminho da máquina, que precisa de uma.
func (s *PreventivaService) CadastrarPreventiva(ctx context.Context, tenantID int64, payload model.PreventivaPayload) (model.Preventiva, error) {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.Preventiva{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	// A FK composta garante o tenant, não que a máquina exista: sem este cheque
	// um maquinaId inexistente vira 23503 e sobe como 422 sem dizer o quê.
	if _, err := repo.ObterMaquinaPorID(ctx, repository.ObterMaquinaPorIDParams{
		ID:       payload.MaquinaId,
		TenantID: tenantID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Preventiva{}, fmt.Errorf("%w: máquina %d não existe neste tenant", helper.ErrConflitoIntegridade, payload.MaquinaId)
		}
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}

	if err := gravarPreventivas(ctx, repo, tenantID, payload.MaquinaId, []model.PreventivaPayload{payload}); err != nil {
		return model.Preventiva{}, err
	}

	// gravarPreventivas não devolve id (serve uma lista), então a resposta sai
	// da listagem da máquina -- a recém-criada é a última pela ordenação.
	// EscopoUsuarioID nil: esta releitura é interna, para achar o id da linha
	// recém-criada -- não é listagem de ninguém, não há escopo a aplicar.
	criadas, err := repo.ListarPreventivas(ctx, repository.ListarPreventivasParams{
		TenantID:  tenantID,
		MaquinaID: &payload.MaquinaId,
	})
	if err != nil {
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}
	if len(criadas) == 0 {
		return model.Preventiva{}, helper.ErrNaoEncontrado
	}

	var nova repository.ListarPreventivasRow
	for _, p := range criadas {
		if p.ID > nova.ID {
			nova = p
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Preventiva{}, fmt.Errorf("erro ao commitar transação: %w", err)
	}

	return model.MontarPreventiva(nova), nil
}

// ListarPreventivas é GET /preventivas (?maquinaId=). Só as ativas -- ver
// preventiva.sql -- e só as que o escopo de quem chama alcança: a aba
// "Manutenção Prev." do gestor tem que trazer as lojas dele, não o tenant.
func (s *PreventivaService) ListarPreventivas(ctx context.Context, tenantID, usuarioID int64, perfil string, maquinaID *int64) ([]model.Preventiva, error) {

	repo := repository.New(s.Pool)

	preventivas, err := repo.ListarPreventivas(ctx, repository.ListarPreventivasParams{
		TenantID:        tenantID,
		MaquinaID:       maquinaID,
		EscopoUsuarioID: escopoDe(usuarioID, perfil),
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	// Não-nil: o front tipa PreventivaListada[] e nil viraria `null` no JSON.
	dto := make([]model.Preventiva, 0, len(preventivas))
	for _, p := range preventivas {
		dto = append(dto, model.MontarPreventiva(p))
	}

	return dto, nil
}

// ObterPreventiva é GET /preventivas/:id.
func (s *PreventivaService) ObterPreventiva(ctx context.Context, tenantID, id int64) (model.Preventiva, error) {

	repo := repository.New(s.Pool)

	preventiva, err := repo.ObterPreventivaPorID(ctx, repository.ObterPreventivaPorIDParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Preventiva{}, helper.ErrNaoEncontrado
		}
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}

	return model.MontarPreventiva(repository.ListarPreventivasRow(preventiva)), nil
}

// AtualizarPreventiva é PUT /preventivas/:id. Sem maquina_id: mover a
// preventiva de máquina deixaria as solicitações que ela já gerou apontando
// para outra máquina (ver preventiva.sql). O front manda o campo; o service
// ignora.
func (s *PreventivaService) AtualizarPreventiva(ctx context.Context, tenantID, id int64, payload model.PreventivaPayload) (model.Preventiva, error) {

	descricao := strings.TrimSpace(payload.Descricao)
	if descricao == "" {
		return model.Preventiva{}, fmt.Errorf("%w: a descrição da preventiva não pode ficar em branco", helper.ErrValidacao)
	}
	if payload.IntervaloDias <= 0 {
		return model.Preventiva{}, fmt.Errorf("%w: o intervalo da preventiva deve ser de pelo menos 1 dia", helper.ErrValidacao)
	}
	if payload.ProximaData == nil || payload.ProximaData.IsZero() {
		return model.Preventiva{}, fmt.Errorf("%w: informe a próxima data da preventiva", helper.ErrValidacao)
	}

	repo := repository.New(s.Pool)

	if err := validarTecnicoPreventiva(ctx, repo, tenantID, payload.TecnicoId); err != nil {
		return model.Preventiva{}, err
	}

	tecnicoID := payload.TecnicoId
	if _, err := repo.AtualizarPreventiva(ctx, repository.AtualizarPreventivaParams{
		ID:            id,
		TenantID:      tenantID,
		Descricao:     descricao,
		IntervaloDias: payload.IntervaloDias,
		ProximaData:   pgtype.Date{Time: payload.ProximaData.Time(), Valid: true},
		Ativa:         payload.Ativa,
		TecnicoID:     &tecnicoID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Preventiva{}, helper.ErrNaoEncontrado
		}
		return model.Preventiva{}, helper.TraduzErroPostgres(err)
	}

	// Relê para devolver os nomes denormalizados: RETURNING não enxerga JOIN.
	return s.ObterPreventiva(ctx, tenantID, id)
}

// DesativarPreventiva é DELETE /preventivas/:id -- soft delete (ativa = false).
//
// Soft não é só convenção da casa aqui: fk_solicitacao_preventiva não tem
// ON DELETE, então preventiva que já gerou solicitação automática recusaria o
// DELETE com 23503 (ver preventiva.sql).
func (s *PreventivaService) DesativarPreventiva(ctx context.Context, tenantID, id int64) error {

	repo := repository.New(s.Pool)

	linhas, err := repo.DesativarPreventiva(ctx, repository.DesativarPreventivaParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}

	if linhas == 0 {
		return helper.ErrNaoEncontrado
	}

	return nil
}

// errPreventivaJaProcessada é o "outra execução pegou esta linha primeiro":
// ObterPreventivaVencidaParaAbertura devolveu zero linhas porque o commit da
// outra réplica já avançou a proxima_data (ou porque o Administrador desativou
// a preventiva/máquina entre a varredura e agora).
//
// Sentinela local e não helper.ErrDadoDuplicado, que era o que o índice
// uq_preventiva_pendente produzia antes da migration 000008: ali havia mesmo
// uma violação de unicidade; aqui não há duplicata nenhuma, há uma linha que
// deixou de casar o WHERE. Chamar isso de "dado duplicado" mandaria o cron
// investigar o erro errado.
var errPreventivaJaProcessada = errors.New("preventiva já processada por outra execução")

// AbrirSolicitacoesDePreventivasVencidas percorre as preventivas cuja
// proxima_data já passou e abre a Solicitação e a Ordem de Serviço de cada uma.
// É o miolo do subcomando de CLI `preventivas-vencidas`, chamado pelo Railway
// Cron -- ver "Abertura automática de solicitação por preventiva" no CLAUDE.md.
//
// ⚠️ A solicitação NÃO passa mais pela fila do Gestor: ela nasce 'Convertida'
// junto com a OS, atribuída ao técnico que a própria preventiva carrega
// (migration 000008). O trabalho já foi aprovado quando a máquina foi
// cadastrada -- procedimento, intervalo e data saíram de lá --, e pedir uma
// segunda aprovação a cada ciclo não decidia nada, só atrasava. O Gestor
// continua vendo tudo pelas abas de OS e de Manutenção Preventiva.
//
// Sem tenantID no parâmetro, diferente de todo o resto do pacote: não há
// request nem token: o job varre todos os tenants e o tenant_id vem na linha da
// preventiva (ver ListarPreventivasVencidas).
//
// Devolve quantas solicitações abriu e os erros das que falharam, juntados com
// errors.Join -- falha em uma preventiva não pode impedir as outras de rodarem,
// então nenhuma delas aborta o laço. Erro não-nil aqui é resultado parcial, não
// fracasso: quem chama loga e segue.
func (s *PreventivaService) AbrirSolicitacoesDePreventivasVencidas(ctx context.Context) (int, error) {

	// Leitura fora de transação: cada preventiva ganha a sua logo abaixo.
	vencidas, err := repository.New(s.Pool).ListarPreventivasVencidas(ctx)
	if err != nil {
		return 0, helper.TraduzErroPostgres(err)
	}

	var criadas int
	var falhas []error

	for _, p := range vencidas {
		err := s.abrirSolicitacaoDaPreventiva(ctx, p)

		switch {
		case err == nil:
			criadas++

		// Outra execução do job pegou esta preventiva entre a varredura e a
		// transação desta -- duas réplicas, ou dois disparos do cron colados. É
		// exatamente o serviço que o FOR UPDATE existe para prestar: não é
		// falha, e refazer não tem sentido (a OS que interessa já existe).
		case errors.Is(err, errPreventivaJaProcessada):

		default:
			falhas = append(falhas, fmt.Errorf("preventiva %d (tenant %d): %w", p.ID, p.TenantID, err))
		}
	}

	return criadas, errors.Join(falhas...)
}

// abrirSolicitacaoDaPreventiva grava a solicitação, a ordem de serviço e o
// avanço do ciclo de uma preventiva vencida -- as três na mesma transação.
//
// Transação própria por preventiva, e não uma para o lote todo, pelos dois
// lados: uma linha ruim não pode derrubar as outras 200, e as escritas aqui
// dentro têm que ser atômicas entre si. Separadas, avançar a data com o INSERT
// falhando pularia o ciclo em silêncio, e abrir a OS sem avançar a data faria
// a preventiva disparar de novo na execução seguinte.
//
// A releitura sob lock no topo é o que substitui uq_preventiva_pendente
// (migration 000008) como proteção contra duas réplicas do cron: quem chega
// depois fica bloqueado no SELECT, lê a data já avançada e sai por
// errPreventivaJaProcessada sem escrever nada.
func (s *PreventivaService) abrirSolicitacaoDaPreventiva(ctx context.Context, p repository.ListarPreventivasVencidasRow) error {

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	prev, err := repo.ObterPreventivaVencidaParaAbertura(ctx, repository.ObterPreventivaVencidaParaAberturaParams{
		ID:       p.ID,
		TenantID: p.TenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errPreventivaJaProcessada
		}
		return helper.TraduzErroPostgres(err)
	}

	// Sem técnico não há para quem abrir OS (ordem_servico.tecnico_id é NOT
	// NULL). Erro visível e não `continue` calado: a coluna é nullable só para
	// a migration não falhar com dado dentro, então preventiva sem técnico é
	// linha antiga esperando conserto -- pulá-la em silêncio faria a máquina
	// parar de ser mantida sem ninguém notar. O erro sobe no errors.Join do
	// laço e o cron sai com código != 0.
	if prev.TecnicoID == nil {
		return fmt.Errorf("%w: preventiva %d não tem técnico responsável -- edite a máquina e escolha um", helper.ErrValidacao, prev.ID)
	}

	// A FK garante que é um usuário do tenant, não que ainda é técnico e que
	// ainda está ativo: AtualizarUsuario deixa promover um técnico a gestor sem
	// tocar nas preventivas dele. Mesmo cheque de AbrirOS, pelo mesmo motivo.
	tecnico, err := repo.ObterUsuarioPorID(ctx, repository.ObterUsuarioPorIDParams{ID: *prev.TecnicoID, TenantID: prev.TenantID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: técnico %d da preventiva %d não existe neste tenant", helper.ErrConflitoIntegridade, *prev.TecnicoID, prev.ID)
		}
		return helper.TraduzErroPostgres(err)
	}
	if tecnico.Perfil != repository.PerfilUsuarioTecnico || !tecnico.Ativo {
		return fmt.Errorf("%w: técnico da preventiva %d não é mais um técnico ativo -- edite a máquina e escolha outro", helper.ErrConflitoIntegridade, prev.ID)
	}

	solicitacaoId, err := repo.CriarSolicitacaoPreventiva(ctx, repository.CriarSolicitacaoPreventivaParams{
		TenantID:     prev.TenantID,
		MaquinaID:    prev.MaquinaID,
		SetorID:      prev.SetorID,
		PreventivaID: prev.ID,
		Descricao:    "Manutenção preventiva: " + prev.Descricao,
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}

	// Tipo, urgência, aberta_por_id e afeta_producao são literais na query --
	// ver CriarOrdemServicoDePreventiva para o porquê de cada um.
	if _, err := repo.CriarOrdemServicoDePreventiva(ctx, repository.CriarOrdemServicoDePreventivaParams{
		TenantID:      prev.TenantID,
		SolicitacaoID: solicitacaoId,
		TecnicoID:     *prev.TecnicoID,
	}); err != nil {
		return helper.TraduzErroPostgres(err)
	}

	// A query soma o intervalo a partir da proxima_data vencida, não de hoje --
	// senão um ciclo processado com atraso arrastaria todos os seguintes.
	if _, err := repo.AvancarProximaData(ctx, repository.AvancarProximaDataParams{
		ID:       prev.ID,
		TenantID: prev.TenantID,
	}); err != nil {
		return helper.TraduzErroPostgres(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("erro ao commitar transação: %w", err)
	}

	s.notificarPreventivaVencida(prev, tecnico.ID)

	return nil
}

// notificarPreventivaVencida avisa o técnico designado por WhatsApp depois
// que a OS já commitou -- fora da transação e em goroutine própria, mesmo
// motivo de SolicitacaoService.notificar: falha de rede não pode atrasar nem
// derrubar o job (que ainda tem outras N preventivas pra processar no mesmo
// laço).
//
// ⚠️ Avisa o TÉCNICO, não os gestores do setor como antes. A mensagem existe
// pra quem tem uma ação pendente, e desde que a preventiva deixou de passar
// pela fila de aprovação (migration 000008) o Gestor não tem nenhuma: a OS já
// nasceu atribuída. Mandar pra ele seria ruído diário sobre trabalho que ele
// não vai executar -- e ruído diário é o caminho mais curto pra ninguém mais
// ler a notificação de solicitação de verdade.
//
// A linha da preventiva não carrega os nomes denormalizados (o comentário da
// query já explica: "aqui ninguém monta resposta de contrato") -- por isso
// relê a máquina via ObterMaquinaPorID, fora da transação já fechada, só pra
// montar o texto da mensagem.
func (s *PreventivaService) notificarPreventivaVencida(p repository.ObterPreventivaVencidaParaAberturaRow, tecnicoId int64) {

	if s.Notificador == nil {
		return
	}

	go func() {
		fundo, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		maquina, err := repository.New(s.Pool).ObterMaquinaPorID(fundo, repository.ObterMaquinaPorIDParams{
			ID: p.MaquinaID, TenantID: p.TenantID,
		})
		if err != nil {
			log.Printf("notificar preventiva %d: obter máquina %d: %v", p.ID, p.MaquinaID, err)
			return
		}

		dados := DadosNotificacao{
			Alvo:      maquina.Nome + " · " + maquina.NumeroPatrimonio,
			Descricao: p.Descricao,
			LojaNome:  maquina.LojaNome,
			SetorNome: maquina.SetorNome,
			// SolicitanteNome fica nil de propósito: preventiva não tem
			// solicitante (ck_origem proíbe), e é o que o template usa pra
			// diferenciar as origens.
		}

		if err := s.Notificador.NotificarOSPreventiva(fundo, p.TenantID, tecnicoId, dados); err != nil {
			log.Printf("notificar preventiva %d: %v", p.ID, err)
		}
	}()
}
