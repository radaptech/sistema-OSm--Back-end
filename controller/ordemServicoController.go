package controller

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/internal/service"
)

type OrdemServicoServiceInterface interface {
	ListarOrdensServico(ctx context.Context, tenantId, usuarioId int64, perfil string, filtros service.FiltrosOrdemServico) ([]model.OrdemServico, error)
	ListarOrdensServicoPaginada(ctx context.Context, tenantId, usuarioId int64, perfil string, filtros service.FiltrosOrdemServico, pagina int32) (model.RespostaPaginada[model.OrdemServico], error)
	ObterIndicadoresDaMaquina(ctx context.Context, tenantId, maquinaId, usuarioId int64, perfil string) (model.IndicadoresMaquina, error)
	ObterIndicadoresDaLoja(ctx context.Context, tenantId, lojaId, usuarioId int64, perfil string) (model.IndicadoresLoja, error)
	// Ciclo de vida da OS (fase 2) -- todo método aqui recebe atorId como o
	// TÉCNICO do token, nunca do corpo (mesmo motivo do resto do sistema): é
	// o service que confere que atorId é o dono da OS, devolvendo
	// ErrNaoEncontrado quando não é (ver a nota em ObterOrdemServicoPorID).
	Iniciar(ctx context.Context, tenantId, atorId, ordemServicoId int64) (model.OrdemServico, error)
	Pausar(ctx context.Context, tenantId, atorId, ordemServicoId int64, motivo string) (model.OrdemServico, error)
	Retomar(ctx context.Context, tenantId, atorId, ordemServicoId int64) (model.OrdemServico, error)
	AcionarTerceiro(ctx context.Context, tenantId, atorId, ordemServicoId, empresaTerceirizadaId int64) (model.OrdemServico, error)
	Encerrar(ctx context.Context, tenantId, atorId, ordemServicoId int64, payload model.EncerramentoOrdemServicoPayload) (model.OrdemServico, error)
	// atorId aqui é o ADMINISTRADOR do token, não o técnico -- é ele quem
	// corrige, não quem executou. Ver a nota em service.CorrigirCusto.
	CorrigirCusto(ctx context.Context, tenantId, atorId, ordemServicoId int64, payload model.LancamentoCustoManutencaoPayload) (model.OrdemServico, error)
	// Linha do tempo da OS (migration 000021). Leitura: o escopo é conferido no
	// service pelo perfil do token.
	ListarHistorico(ctx context.Context, tenantId, usuarioId int64, perfil string, ordemServicoId int64) ([]model.EventoOrdemServico, error)
	// Resumo por loja/setor (cards da navegação com lista paginada no servidor) e
	// uma OS por id (link de notificação).
	ResumoOrdensServicoPorSetor(ctx context.Context, tenantId, usuarioId int64, perfil string, filtros service.FiltrosOrdemServico) ([]model.ResumoOrdensServicoSetor, error)
	ObterOrdemServico(ctx context.Context, tenantId, usuarioId int64, perfil string, id int64) (model.OrdemServico, error)
	// A lista por trás de um card de custo do painel da loja (maquinário ou pequenos reparos).
	ListarOrdensDoPainelDaLoja(ctx context.Context, tenantId, lojaId, usuarioId int64, perfil, grupo string, filtros service.FiltrosOrdensDoPainel, pagina int32) (model.RespostaPaginada[model.OrdemDoPainel], error)
}

// OrdemServicoController guarda só o bucket das fotos de empresa terceirizada
// (OrdemServico.empresaTerceirizadaFotoUrl). A foto do defeito é da
// SOLICITAÇÃO, e é lá que o modal de detalhes vai buscá-la.
type OrdemServicoController struct {
	service             OrdemServicoServiceInterface
	bucketTerceirizadas string
}

func NewOrdemServicoController(service OrdemServicoServiceInterface, bucketTerceirizadas string) *OrdemServicoController {

	return &OrdemServicoController{
		service:             service,
		bucketTerceirizadas: bucketTerceirizadas,
	}
}

// resolverFoto troca, no lugar, a chave do R2 da foto da terceirizada pela URL
// assinada (assinarFoto, controller/foto.go).
func (o *OrdemServicoController) resolverFoto(ctx context.Context, ordem *model.OrdemServico) {
	ordem.EmpresaTerceirizadaFotoUrl = assinarFoto(ctx, o.bucketTerceirizadas, ordem.EmpresaTerceirizadaFotoUrl, "ordem_servico", ordem.Id)
}

// statusOsValidos e tiposOsValidos são os ENUMs status_os/tipo_os do banco.
// Mesmo motivo de statusSolicitacaoValidos em solicitacaoController.go: o
// filtro entra num cast ::status_os/::tipo_os dentro da query, então valor
// fora da lista viraria erro 22P02 do Postgres -- 500 para o que é erro do
// cliente. Barra aqui e responde 400.
var statusOsValidos = []string{"Aberta", "Em Andamento", "Pausada", "Concluída"}
var tiposOsValidos = []string{"maquinario", "terceiros", "reparo"}

// statusDeQuery lê ?status= da fila de OS.
//
// ⚠️ Separado por VÍRGULA, não repetido: montarQuery no front faz
// `busca.set(chave, valor.join(','))` para todo array (servicos/montarQuery.ts),
// então ?status=Aberta,Em Andamento é uma chave só. ctx.QueryArray devolveria
// um item só, com a vírgula dentro, e o cast no Postgres estouraria em 22P02.
//
// Devolve nil (não filtra) quando ausente ou vazio, mesmo critério de
// idDeQuery -- o front já descarta filtro vazio, e "todas" é o modo normal do
// Painel do Gestor.
func statusDeQuery(ctx *gin.Context) ([]string, bool) {

	bruto := ctx.Query("status")
	if bruto == "" {
		return nil, true
	}

	status := strings.Split(bruto, ",")
	for i, s := range status {
		// Um espaço depois da vírgula é o erro humano óbvio em teste manual
		// (curl/Postman), e o encode do front já preserva o espaço DENTRO do
		// valor ("Em Andamento") -- aparar a borda não estraga nenhum rótulo.
		status[i] = strings.TrimSpace(s)
		if !slices.Contains(statusOsValidos, status[i]) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "status inválido: " + status[i]})
			return nil, false
		}
	}

	return status, true
}

// filtrosDeQuery lê os filtros de GET /ordens-servico e de /ordens-servico/resumo
// -- os dois têm que aceitar exatamente os mesmos, senão o card diz uma
// quantidade e a lista mostra outra. Já responde 400 e devolve ok=false quando
// algum é inválido.
func filtrosDeQuery(ctx *gin.Context) (service.FiltrosOrdemServico, bool) {

	status, ok := statusDeQuery(ctx)
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	var tipo *string
	if bruto := ctx.Query("tipo"); bruto != "" {
		if !slices.Contains(tiposOsValidos, bruto) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "tipo inválido"})
			return service.FiltrosOrdemServico{}, false
		}
		tipo = &bruto
	}

	// Diferente de status/tipo, "finalizada" tem só dois valores válidos e
	// quem os define é o ParseBool: qualquer outra coisa é erro de
	// cliente, não "não filtrar" -- ?finalizada=sim silenciosamente
	// ignorado devolveria a lista inteira para a tela de OS Finalizadas.
	finalizada, ok := boolDeQuery(ctx, "finalizada")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	lojaId, ok := idDeQuery(ctx, "lojaId")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	setorId, ok := idDeQuery(ctx, "setorId")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	// Nível de máquina: a máquina da solicitação ou, com ?reparo=true, os
	// pequenos reparos (sem máquina). Os dois juntos nunca casam nada.
	maquinaId, ok := idDeQuery(ctx, "maquinaId")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	reparo, ok := boolDeQuery(ctx, "reparo")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	tecnicoId, ok := idDeQuery(ctx, "tecnicoId")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	var busca *string
	if bruto := ctx.Query("busca"); bruto != "" {
		busca = &bruto
	}

	revisado, ok := boolDeQuery(ctx, "revisado")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	// Filtros avançados do Painel do Gestor. Data é YYYY-MM-DD (o que o
	// <input type="date"> manda) e valor é número: lixo é 400, não "sem filtro" --
	// ignorar calado devolveria a lista inteira para quem pediu um recorte.
	var maquina *string
	if bruto := strings.TrimSpace(ctx.Query("maquina")); bruto != "" {
		maquina = &bruto
	}

	dataInicio, ok := dataDeQuery(ctx, "dataInicio")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}
	dataFim, ok := dataDeQuery(ctx, "dataFim")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}
	valorMinimo, ok := valorDeQuery(ctx, "valorMinimo")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}
	valorMaximo, ok := valorDeQuery(ctx, "valorMaximo")
	if !ok {
		return service.FiltrosOrdemServico{}, false
	}

	return service.FiltrosOrdemServico{
		Status:      status,
		Tipo:        tipo,
		Finalizada:  finalizada,
		Revisado:    revisado,
		LojaId:      lojaId,
		SetorId:     setorId,
		MaquinaId:   maquinaId,
		Reparo:      reparo,
		TecnicoId:   tecnicoId,
		Busca:       busca,
		Maquina:     maquina,
		DataInicio:  dataInicio,
		DataFim:     dataFim,
		ValorMinimo: valorMinimo,
		ValorMaximo: valorMaximo,
	}, true
}

func dataDeQuery(ctx *gin.Context, nome string) (*time.Time, bool) {

	bruto := ctx.Query(nome)
	if bruto == "" {
		return nil, true
	}

	data, err := time.Parse("2006-01-02", bruto)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": nome + " inválida (use AAAA-MM-DD)"})
		return nil, false
	}

	return &data, true
}

func valorDeQuery(ctx *gin.Context, nome string) (*float64, bool) {

	bruto := ctx.Query(nome)
	if bruto == "" {
		return nil, true
	}

	valor, err := strconv.ParseFloat(bruto, 64)
	if err != nil || valor < 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": nome + " inválido"})
		return nil, false
	}

	return &valor, true
}

// Listar é GET /ordens-servico -- um endpoint para os três painéis, recortado
// pelo escopo de quem chama (o WHERE da query, nunca o filtro do cliente):
//
//	Gestor  (PainelGestor)                  -> sem filtro
//	Técnico (PainelTecnico)                 -> ?tecnicoId=
//	Admin   (CustosPendentes/OSFinalizadas) -> ?status=Concluída / ?finalizada=true
//
// Sem `?pagina=` a resposta é array simples com tudo -- os painéis do Gestor e
// do Técnico montam a navegação Loja -> Setor a partir da lista inteira. Com
// `?pagina=N` vira RespostaPaginada (12 por página, ListarOrdensServicoPaginada):
// é o que as telas de histórico do Administrador pedem, porque OS finalizada só
// acumula. `?revisado=` separa as abas de Custos Pendentes.
//
// Só lê -- o único erro possível é do banco, e ele vai 500 com o erro cru no
// log (nome de constraint/coluna não pode ir no corpo da resposta).
func (o *OrdemServicoController) Listar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		// Do TOKEN, nunca da query: aceitar do cliente deixaria um Técnico
		// listar a loja inteira mandando outro id.
		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		filtros, ok := filtrosDeQuery(ctx)
		if !ok {
			return
		}

		if bruto := ctx.Query("pagina"); bruto != "" {
			pagina, err := strconv.Atoi(bruto)
			if err != nil || pagina < 1 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "pagina inválida"})
				return
			}

			resposta, err := o.service.ListarOrdensServicoPaginada(ctx.Request.Context(), tenantId, usuarioId, perfil, filtros, int32(pagina))
			if err != nil {
				slog.ErrorContext(ctx.Request.Context(), "listar ordens de serviço (paginada)", "tenant", tenantId, "err", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar ordens de serviço"})
				return
			}

			for i := range resposta.Dados {
				o.resolverFoto(ctx.Request.Context(), &resposta.Dados[i])
			}

			ctx.JSON(http.StatusOK, resposta)
			return
		}

		ordens, err := o.service.ListarOrdensServico(ctx.Request.Context(), tenantId, usuarioId, perfil, filtros)
		if err != nil {
			slog.ErrorContext(ctx.Request.Context(), "listar ordens de serviço", "tenant", tenantId, "err", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar ordens de serviço"})
			return
		}

		for i := range ordens {
			o.resolverFoto(ctx.Request.Context(), &ordens[i])
		}

		ctx.JSON(http.StatusOK, ordens)
	}
}

// Indicadores é GET /indicadores/maquinas/:id -- o Painel de Indicadores do
// Gestor (DashboardGestor). O `:id` é de MÁQUINA, não de OS: a rota vive fora
// de /ordens-servico porque é assim que o front a chama (servicoIndicadores),
// mas o handler mora aqui pelo mesmo motivo do service -- tudo que ela lê é
// histórico de OS.
//
// ErrNaoEncontrado é 404 e cobre os dois casos de uma vez: máquina que não
// existe e máquina fora do escopo de quem chama. Distingui-los responderia
// "esta existe, você é que não pode ver" -- que é justamente o que a
// enumeração de ids procura. Não é 403: 403 é perfil errado (o RBAC da rota),
// e aqui o perfil está certo.
func (o *OrdemServicoController) Indicadores() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		maquinaId, ok := idDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		indicadores, err := o.service.ObterIndicadoresDaMaquina(ctx.Request.Context(), tenantId, maquinaId, usuarioId, perfil)
		if err != nil {
			if errors.Is(err, helper.ErrNaoEncontrado) {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "máquina não encontrada"})
				return
			}
			slog.ErrorContext(ctx.Request.Context(), "indicadores da máquina", "maquina", maquinaId, "tenant", tenantId, "err", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter indicadores da máquina"})
			return
		}

		ctx.JSON(http.StatusOK, indicadores)
	}
}

// Resumo é GET /ordens-servico/resumo -- quantas OS em cada loja/setor para os
// mesmos filtros de Listar. Sem ele os cards de loja e setor do Painel do Gestor
// não teriam de onde contar quando a lista é paginada no servidor.
func (o *OrdemServicoController) Resumo() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		filtros, ok := filtrosDeQuery(ctx)
		if !ok {
			return
		}

		resumo, err := o.service.ResumoOrdensServicoPorSetor(ctx.Request.Context(), tenantId, usuarioId, perfil, filtros)
		if err != nil {
			slog.ErrorContext(ctx.Request.Context(), "resumo de ordens de serviço", "tenant", tenantId, "err", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao resumir ordens de serviço"})
			return
		}

		ctx.JSON(http.StatusOK, resumo)
	}
}

// Obter é GET /ordens-servico/:id -- uma OS completa no escopo de quem pede
// (404 fora dele, igual a inexistente).
func (o *OrdemServicoController) Obter() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		ordem, err := o.service.ObterOrdemServico(ctx.Request.Context(), tenantId, usuarioId, perfil, id)
		if err != nil {
			if errors.Is(err, helper.ErrNaoEncontrado) {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "ordem de serviço não encontrada"})
				return
			}
			slog.ErrorContext(ctx.Request.Context(), "obter ordem de serviço", "os", id, "tenant", tenantId, "err", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter ordem de serviço"})
			return
		}

		o.resolverFoto(ctx.Request.Context(), &ordem)

		ctx.JSON(http.StatusOK, ordem)
	}
}

// Historico é GET /ordens-servico/:id/historico -- a linha do tempo da OS: quem
// fez cada passo e quando, com o estado do custo a cada lançamento. OS fora do
// alcance de quem pede é 404, igual a OS que não existe.
func (o *OrdemServicoController) Historico() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		eventos, err := o.service.ListarHistorico(ctx.Request.Context(), tenantId, usuarioId, perfil, id)
		if err != nil {
			if errors.Is(err, helper.ErrNaoEncontrado) {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "ordem de serviço não encontrada"})
				return
			}
			slog.ErrorContext(ctx.Request.Context(), "histórico da ordem de serviço", "os", id, "tenant", tenantId, "err", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter histórico da ordem de serviço"})
			return
		}

		ctx.JSON(http.StatusOK, eventos)
	}
}

// IndicadoresLoja é GET /indicadores/lojas/:id -- a tela da loja no Painel de
// Indicadores. Mesmo desenho de Indicadores: o `:id` é de LOJA, e loja que não
// existe ou fora do escopo são o mesmo 404.
func (o *OrdemServicoController) IndicadoresLoja() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		lojaId, ok := idDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		indicadores, err := o.service.ObterIndicadoresDaLoja(ctx.Request.Context(), tenantId, lojaId, usuarioId, perfil)
		if err != nil {
			if errors.Is(err, helper.ErrNaoEncontrado) {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "loja não encontrada"})
				return
			}
			slog.ErrorContext(ctx.Request.Context(), "indicadores da loja", "loja", lojaId, "tenant", tenantId, "err", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao obter indicadores da loja"})
			return
		}

		ctx.JSON(http.StatusOK, indicadores)
	}
}

// OrdensDoPainelDaLoja é GET /indicadores/lojas/:id/ordens -- a lista por trás de um card de custo
// do painel da loja, paginada (12). `?grupo=maquinario|reparos|preventivas` escolhe o card
// (obrigatório): maquinário é o "Custo Maquinário"; reparos e preventivas são as faixas "Pequenos
// Reparos" e "Manutenção Preventiva", que ficam separadas dele.
// Aceita os recortes que a tela mostra: `?setorId=`, `?maquinaId=` (só maquinário: reparo não
// tem máquina) e `?mes=MM/AAAA` (o rótulo do gráfico mensal, convertido aqui para o YYYY-MM
// que a query compara). Loja fora do escopo é 404, como o painel.
func (o *OrdemServicoController) OrdensDoPainelDaLoja() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		lojaId, ok := idDaRota(ctx)
		if !ok {
			return
		}

		usuarioId, perfil, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		// Sem grupo não há o que listar: erro de cliente, não "a loja inteira".
		grupo := ctx.Query("grupo")
		if grupo != model.GrupoMaquinario && grupo != model.GrupoReparos && grupo != model.GrupoPreventivas {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "grupo inválido (use maquinario, reparos ou preventivas)"})
			return
		}

		setorId, ok := idDeQuery(ctx, "setorId")
		if !ok {
			return
		}

		maquinaId, ok := idDeQuery(ctx, "maquinaId")
		if !ok {
			return
		}
		if maquinaId != nil && grupo == model.GrupoReparos {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "maquinaId não vale para reparos (não têm máquina)"})
			return
		}

		// Lixo é erro de cliente: ignorar calado listaria a loja inteira para quem pediu um mês.
		var mes *string
		if bruto := ctx.Query("mes"); bruto != "" {
			if !mesValido.MatchString(bruto) {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "mes inválido (use MM/AAAA)"})
				return
			}
			convertido := bruto[3:7] + "-" + bruto[0:2]
			mes = &convertido
		}

		pagina := 1
		if bruto := ctx.Query("pagina"); bruto != "" {
			n, err := strconv.Atoi(bruto)
			if err != nil || n < 1 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "pagina inválida"})
				return
			}
			pagina = n
		}

		resposta, err := o.service.ListarOrdensDoPainelDaLoja(ctx.Request.Context(), tenantId, lojaId, usuarioId, perfil, grupo,
			service.FiltrosOrdensDoPainel{SetorId: setorId, MaquinaId: maquinaId, Mes: mes}, int32(pagina))
		if err != nil {
			if errors.Is(err, helper.ErrNaoEncontrado) {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "loja não encontrada"})
				return
			}
			slog.ErrorContext(ctx.Request.Context(), "ordens do painel da loja", "loja", lojaId, "grupo", grupo, "tenant", tenantId, "err", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar as ordens da loja"})
			return
		}

		ctx.JSON(http.StatusOK, resposta)
	}
}

// mesValido é o rótulo MM/AAAA do gráfico mensal.
var mesValido = regexp.MustCompile(`^(0[1-9]|1[0-2])/[0-9]{4}$`)

// Iniciar é POST /ordens-servico/:id/iniciar -- Aberta -> Em Andamento.
// Sem corpo. 200, não 201: atualiza uma OS que já existe (quem cria é
// SolicitacaoController.AbrirOS), mesmo critério de Rejeitar.
func (o *OrdemServicoController) Iniciar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		os, err := o.service.Iniciar(ctx.Request.Context(), tenantId, atorId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				slog.ErrorContext(ctx.Request.Context(), "iniciar ordem de serviço", "os", id, "tenant", tenantId, "err", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar ordem de serviço"})
			}
			return
		}

		o.resolverFoto(ctx.Request.Context(), &os)

		ctx.JSON(http.StatusOK, os)
	}
}

// Pausar é POST /ordens-servico/:id/pausar -- (Aberta ou Em Andamento) ->
// Pausada, com `{motivo}` no corpo.
func (o *OrdemServicoController) Pausar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.PausaOrdemServicoPayload](ctx)
		if !ok {
			return
		}

		os, err := o.service.Pausar(ctx.Request.Context(), tenantId, atorId, id, input.Motivo)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				slog.ErrorContext(ctx.Request.Context(), "pausar ordem de serviço", "os", id, "tenant", tenantId, "err", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao pausar ordem de serviço"})
			}
			return
		}

		o.resolverFoto(ctx.Request.Context(), &os)

		ctx.JSON(http.StatusOK, os)
	}
}

// Retomar é POST /ordens-servico/:id/retomar -- Pausada -> o status que ela
// tinha antes de pausar. Sem corpo.
func (o *OrdemServicoController) Retomar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		os, err := o.service.Retomar(ctx.Request.Context(), tenantId, atorId, id)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				slog.ErrorContext(ctx.Request.Context(), "retomar ordem de serviço", "os", id, "tenant", tenantId, "err", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao retomar ordem de serviço"})
			}
			return
		}

		o.resolverFoto(ctx.Request.Context(), &os)

		ctx.JSON(http.StatusOK, os)
	}
}

// AcionarTerceiro é POST /ordens-servico/:id/acionar-terceiro -- promove tipo
// pra 'terceiros', com `{empresaTerceirizadaId}` no corpo. Não muda status de
// execução (ver a nota no service).
func (o *OrdemServicoController) AcionarTerceiro() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.AcionamentoTerceiroPayload](ctx)
		if !ok {
			return
		}

		os, err := o.service.AcionarTerceiro(ctx.Request.Context(), tenantId, atorId, id, input.EmpresaTerceirizadaId)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				slog.ErrorContext(ctx.Request.Context(), "acionar terceiro ordem de serviço", "os", id, "tenant", tenantId, "err", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao acionar terceiro"})
			}
			return
		}

		o.resolverFoto(ctx.Request.Context(), &os)

		ctx.JSON(http.StatusOK, os)
	}
}

// Encerrar é POST /ordens-servico/:id/encerrar -- Em Andamento -> Concluída,
// gravando o que o Técnico apurou e o custo na mesma escrita (ver a nota no
// service).
func (o *OrdemServicoController) Encerrar() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.EncerramentoOrdemServicoPayload](ctx)
		if !ok {
			return
		}

		os, err := o.service.Encerrar(ctx.Request.Context(), tenantId, atorId, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				slog.ErrorContext(ctx.Request.Context(), "encerrar ordem de serviço", "os", id, "tenant", tenantId, "err", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao encerrar ordem de serviço"})
			}
			return
		}

		o.resolverFoto(ctx.Request.Context(), &os)

		ctx.JSON(http.StatusOK, os)
	}
}

// Custo é POST /ordens-servico/:id/custo -- o Administrador corrigindo o
// custo que o Técnico já lançou no encerramento (ver a nota em
// service.CorrigirCusto), tipicamente conferindo contra a nota fiscal.
func (o *OrdemServicoController) Custo() gin.HandlerFunc {

	return func(ctx *gin.Context) {

		id, ok := idDaRota(ctx)
		if !ok {
			return
		}

		tenantId, ok := tenantDaRota(ctx)
		if !ok {
			return
		}

		atorId, _, ok := atorDaRota(ctx)
		if !ok {
			return
		}

		input, ok := corpoJSON[model.LancamentoCustoManutencaoPayload](ctx)
		if !ok {
			return
		}

		os, err := o.service.CorrigirCusto(ctx.Request.Context(), tenantId, atorId, id, input)
		if err != nil {

			switch {
			case errors.Is(err, helper.ErrValidacao):
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrNaoEncontrado):
				ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			// uq_nota_fiscal_os (migration 000012): o Administrador digitou a
			// mesma nota duas vezes na lista. Só passou a ser alcançável quando
			// a nota virou tabela -- até a 000011 esta rota era um UPDATE 1:1 em
			// os_custo, que não tem como colidir, e por isso o caso não existia
			// aqui. Sem ele o erro cai no default e vira 500 numa tela que só
			// queria avisar "essa nota você já lançou".
			case errors.Is(err, helper.ErrDadoDuplicado):
				ctx.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case errors.Is(err, helper.ErrConflitoIntegridade):
				ctx.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				slog.ErrorContext(ctx.Request.Context(), "corrigir custo ordem de serviço", "os", id, "tenant", tenantId, "err", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao corrigir custo da ordem de serviço"})
			}
			return
		}

		o.resolverFoto(ctx.Request.Context(), &os)

		ctx.JSON(http.StatusOK, os)
	}
}

// boolDeQuery lê um filtro booleano opcional (?finalizada=, ?revisado=). Só o
// ParseBool decide o que vale: qualquer outra coisa é 400, nunca "não filtrar"
// -- ?finalizada=sim silenciosamente ignorado devolveria a lista inteira para
// a tela de OS Finalizadas.
func boolDeQuery(ctx *gin.Context, nome string) (*bool, bool) {

	bruto := ctx.Query(nome)
	if bruto == "" {
		return nil, true
	}

	v, err := strconv.ParseBool(bruto)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": nome + " inválido"})
		return nil, false
	}

	return &v, true
}
