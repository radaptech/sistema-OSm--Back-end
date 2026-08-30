package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// NotificadorInterface é o que SolicitacaoService (e o job de preventiva
// vencida) dependem pra avisar o Gestor de uma solicitação nova -- nunca a
// struct concreta, pra poder trocar por um fake nos testes sem bater na
// Evolution API de verdade (mesmo motivo de todo XxxServiceInterface em
// controller/, só que um nível mais fundo: aqui é service dependendo de
// service).
type NotificadorInterface interface {
	NotificarNovaSolicitacao(ctx context.Context, tenantId, setorId int64, dados DadosNotificacao) error
}

// DadosNotificacao é o que os dois templates (solicitação aberta pelo
// Solicitante, preventiva vencida) precisam pra montar o texto -- reduzido
// ao necessário pra mensagem, não é model.SolicitacaoOS inteiro: o job de
// preventiva vencida não tem (nem devia fabricar) uma resposta de
// solicitação completa só pra notificar.
//
// SolicitanteNome nil é o que decide o template: preventiva vencida nasce
// sem solicitante (ninguém por trás, ver ck_origem), mesmo critério que o
// resto do sistema já usa pra diferenciar as duas origens.
type DadosNotificacao struct {
	// Alvo já vem formatado por quem chama -- "Forno · PAT-001" (maquinário,
	// mesmo padrão nome·patrimônio de CamposMaquina.tsx/AdministradorMaquinas)
	// ou o texto livre do item (reparo/preventiva).
	Alvo            string
	Descricao       string
	LojaNome        string
	SetorNome       string
	SolicitanteNome *string
}

// NotificacaoService é a implementação real: lê os gestores do setor
// (ObterGestoresDoSetor, database/queries/usuario.sql) e manda a mensagem
// pra cada um via Evolution API (ver CLAUDE.md, "Notificação de solicitação
// por WhatsApp").
type NotificacaoService struct {
	Pool   *pgxpool.Pool
	client *http.Client
	url    string
	apiKey string
	// instancia é o nome da instância do WhatsApp na Evolution API
	// (EVOLUTION_INSTANCE_NAME) -- entra na URL de todo envio.
	instancia string
}

// NewRepoNotificacao recebe url/apiKey/instancia como parâmetros, não lê
// os.Getenv aqui dentro -- mesmo padrão de NewMaquinaController recebendo
// bucketFotos: quem resolve variável de ambiente é o wiring (router.go),
// não o service. Mantém isto testável sem depender de env global.
func NewRepoNotificacao(pool *pgxpool.Pool, url, apiKey, instancia string) *NotificacaoService {

	return &NotificacaoService{
		Pool:      pool,
		client:    &http.Client{Timeout: 10 * time.Second},
		url:       url,
		apiKey:    apiKey,
		instancia: instancia,
	}
}

// NotificarNovaSolicitacao é o único método do notificador de verdade: busca
// os gestores do setor e manda a mensagem pra cada um.
//
// Zero gestor com telefone não é erro -- ObterGestoresDoSetor já filtra
// quieto quem não tem telefone/está desativado, e simplesmente não há pra
// quem mandar.
//
// Falha em UM gestor não impede os outros -- errors.Join acumula, mesmo
// espírito de AbrirSolicitacoesDePreventivasVencidas (erro não-nil aqui é
// resultado parcial, não fracasso total). Quem chama loga; este método não
// loga nada sozinho.
func (n *NotificacaoService) NotificarNovaSolicitacao(ctx context.Context, tenantId, setorId int64, dados DadosNotificacao) error {

	repo := repository.New(n.Pool)

	gestores, err := repo.ObterGestoresDoSetor(ctx, repository.ObterGestoresDoSetorParams{
		TenantID: tenantId,
		SetorID:  setorId,
	})
	if err != nil {
		return fmt.Errorf("buscar gestores do setor %d: %w", setorId, err)
	}

	texto := montarTexto(dados)

	var falhas error
	for _, gestor := range gestores {
		if gestor.Telefone == nil {
			// Defesa: a query já filtra IS NOT NULL. Nunca deveria disparar.
			continue
		}
		if err := n.enviarTexto(ctx, *gestor.Telefone, texto); err != nil {
			falhas = errors.Join(falhas, fmt.Errorf("gestor %d (%s): %w", gestor.ID, gestor.Nome, err))
		}
	}

	return falhas
}

// montarTexto escolhe o template pelos dados que têm: com solicitante, é a
// mensagem "alguém abriu um pedido"; sem, é "uma preventiva venceu sozinha".
func montarTexto(d DadosNotificacao) string {

	if d.SolicitanteNome != nil {
		return fmt.Sprintf(
			"🔧 Nova solicitação — %s / %s\n%s\nSolicitante: %s\n\"%s\"\n\nAcesse o painel para avaliar.",
			d.LojaNome, d.SetorNome, d.Alvo, *d.SolicitanteNome, d.Descricao,
		)
	}

	return fmt.Sprintf(
		"🛠️ Preventiva vencida — %s / %s\n%s\n\"%s\"\n\nAcesse o painel para avaliar.",
		d.LojaNome, d.SetorNome, d.Alvo, d.Descricao,
	)
}

// normalizarTelefone reduz o telefone cadastrado (livre, sem máscara --
// usuario.telefone não tem validação de formato em lugar nenhum, ver
// internal/model/usuarios.go) pro que a Evolution API espera: só dígitos,
// com o 55 (DDI do Brasil) na frente. Sem isto "(11) 99999-0001" nunca
// bateria com nenhum número de WhatsApp de verdade.
//
// Só cobre o Brasil de propósito -- é o escopo do projeto inteiro (fuso
// horário fixo em America/Sao_Paulo, valores em R$, sem i18n em lugar
// nenhum).
func normalizarTelefone(bruto string) string {

	var digitos strings.Builder
	for _, r := range bruto {
		if r >= '0' && r <= '9' {
			digitos.WriteRune(r)
		}
	}

	numero := digitos.String()
	if strings.HasPrefix(numero, "55") && len(numero) >= 12 {
		return numero
	}
	return "55" + numero
}

// enviarTexto é a única função que fala HTTP com a Evolution API --
// POST /message/sendText/{instancia}, testado contra a instância real
// (endpoint e formato do corpo confirmados na prática, não só pela doc: a
// doc pública descreve o path como /{instancia}/message/sendText, invertido
// -- 404 contra a instância de verdade. É /message/sendText/{instancia},
// mesmo padrão de /instance/connect/{instancia}).
func (n *NotificacaoService) enviarTexto(ctx context.Context, telefoneBruto, texto string) error {

	corpo, err := json.Marshal(map[string]string{
		"number": normalizarTelefone(telefoneBruto),
		"text":   texto,
	})
	if err != nil {
		return fmt.Errorf("montar corpo da mensagem: %w", err)
	}

	endpoint := fmt.Sprintf("%s/message/sendText/%s", n.url, n.instancia)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(corpo))
	if err != nil {
		return fmt.Errorf("montar requisição: %w", err)
	}
	req.Header.Set("apiKey", n.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("chamar evolution api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("evolution api respondeu %d", resp.StatusCode)
	}

	return nil
}
