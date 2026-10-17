package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/internal/service"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

// ordemServicoFake grava o que recebeu, não só o erro que devolve -- mesmo
// critério de solicitacaoFake/maquinaFake: filtro ou ator vindo do lugar
// errado não muda o status da resposta, então o teste precisa olhar o que o
// service recebeu de verdade. Um ?tecnicoId= lido da query como ATOR daria 200
// com a lista de outra pessoa.
type ordemServicoFake struct {
	err error

	filtros *service.FiltrosOrdemServico
	// ator recebido, como "usuarioId/perfil".
	ator *string
	// maquinaId recebido por ObterIndicadoresDaMaquina.
	maquinaId *int64
	// ordemServicoId recebido pelos métodos do ciclo de vida (Iniciar e os
	// que vêm a seguir).
	ordemServicoId *int64
	// motivo recebido por Pausar.
	motivo *string
	// empresaTerceirizadaId recebido por AcionarTerceiro.
	empresaTerceirizadaId *int64
	// payload recebido por Encerrar.
	encerramento *model.EncerramentoOrdemServicoPayload
	// payload recebido por CorrigirCusto.
	custo *model.LancamentoCustoManutencaoPayload
	// pagina recebida pela listagem paginada; fica 0 quando a chamada foi a
	// listagem sem página -- é o que distingue os dois caminhos no teste.
	pagina *int32
	// o que ListarOrdensDoPainelDaLoja recebeu, como "grupo=,setor=,maquina=,mes=,pagina=".
	painelRecebido *string
}

func (o ordemServicoFake) ListarOrdensServicoPaginada(ctx context.Context, tenantId, usuarioId int64, perfil string, f service.FiltrosOrdemServico, pagina int32) (model.RespostaPaginada[model.OrdemServico], error) {
	if o.pagina != nil {
		*o.pagina = pagina
	}
	lista, err := o.ListarOrdensServico(ctx, tenantId, usuarioId, perfil, f)
	if err != nil {
		return model.RespostaPaginada[model.OrdemServico]{}, err
	}
	return model.RespostaPaginada[model.OrdemServico]{Dados: lista, Pagina: pagina, TotalPaginas: 1, Total: int64(len(lista))}, nil
}

func (o ordemServicoFake) ListarOrdensServico(_ context.Context, _, usuarioId int64, perfil string, f service.FiltrosOrdemServico) ([]model.OrdemServico, error) {
	if o.filtros != nil {
		*o.filtros = f
	}
	if o.ator != nil {
		*o.ator = perfil + "/" + strconv.FormatInt(usuarioId, 10)
	}
	if o.err != nil {
		return nil, o.err
	}
	return []model.OrdemServico{{
		Id: 1, SolicitacaoId: 3, Tipo: "maquinario", StatusExecucao: "Aberta",
		Descricao: "Forno não aquece", SetorId: 4, SetorNome: "Padaria",
		LojaId: 1, LojaNome: "Loja A",
	}}, nil
}

func (o ordemServicoFake) ObterIndicadoresDaMaquina(_ context.Context, _, maquinaId, usuarioId int64, perfil string) (model.IndicadoresMaquina, error) {
	if o.maquinaId != nil {
		*o.maquinaId = maquinaId
	}
	if o.ator != nil {
		*o.ator = perfil + "/" + strconv.FormatInt(usuarioId, 10)
	}
	if o.err != nil {
		return model.IndicadoresMaquina{}, o.err
	}
	return model.MontarIndicadoresMaquina(maquinaId, nil), nil
}

func (o ordemServicoFake) ObterIndicadoresDaLoja(_ context.Context, _, lojaId, _ int64, _ string) (model.IndicadoresLoja, error) {
	if o.err != nil {
		return model.IndicadoresLoja{}, o.err
	}
	return model.MontarIndicadoresLoja(lojaId, nil, nil, nil), nil
}

func (o ordemServicoFake) Iniciar(_ context.Context, _, atorId, ordemServicoId int64) (model.OrdemServico, error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = ordemServicoId
	}
	if o.ator != nil {
		*o.ator = "tecnico/" + strconv.FormatInt(atorId, 10)
	}
	if o.err != nil {
		return model.OrdemServico{}, o.err
	}
	return model.OrdemServico{
		Id: ordemServicoId, SolicitacaoId: 3, Tipo: "maquinario", StatusExecucao: "Em Andamento",
		Descricao: "Forno não aquece", SetorId: 4, SetorNome: "Padaria",
		LojaId: 1, LojaNome: "Loja A",
	}, nil
}

func (o ordemServicoFake) Pausar(_ context.Context, _, atorId, ordemServicoId int64, motivo string) (model.OrdemServico, error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = ordemServicoId
	}
	if o.motivo != nil {
		*o.motivo = motivo
	}
	if o.ator != nil {
		*o.ator = "tecnico/" + strconv.FormatInt(atorId, 10)
	}
	if o.err != nil {
		return model.OrdemServico{}, o.err
	}
	return model.OrdemServico{
		Id: ordemServicoId, SolicitacaoId: 3, Tipo: "maquinario", StatusExecucao: "Pausada",
		Descricao: "Forno não aquece", SetorId: 4, SetorNome: "Padaria",
		LojaId: 1, LojaNome: "Loja A",
	}, nil
}

func (o ordemServicoFake) Retomar(_ context.Context, _, atorId, ordemServicoId int64) (model.OrdemServico, error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = ordemServicoId
	}
	if o.ator != nil {
		*o.ator = "tecnico/" + strconv.FormatInt(atorId, 10)
	}
	if o.err != nil {
		return model.OrdemServico{}, o.err
	}
	return model.OrdemServico{
		Id: ordemServicoId, SolicitacaoId: 3, Tipo: "maquinario", StatusExecucao: "Em Andamento",
		Descricao: "Forno não aquece", SetorId: 4, SetorNome: "Padaria",
		LojaId: 1, LojaNome: "Loja A",
	}, nil
}

func (o ordemServicoFake) AcionarTerceiro(_ context.Context, _, atorId, ordemServicoId, empresaTerceirizadaId int64) (model.OrdemServico, error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = ordemServicoId
	}
	if o.empresaTerceirizadaId != nil {
		*o.empresaTerceirizadaId = empresaTerceirizadaId
	}
	if o.ator != nil {
		*o.ator = "tecnico/" + strconv.FormatInt(atorId, 10)
	}
	if o.err != nil {
		return model.OrdemServico{}, o.err
	}
	return model.OrdemServico{
		Id: ordemServicoId, SolicitacaoId: 3, Tipo: "terceiros", StatusExecucao: "Aberta",
		Descricao: "Forno não aquece", SetorId: 4, SetorNome: "Padaria",
		LojaId: 1, LojaNome: "Loja A",
	}, nil
}

func (o ordemServicoFake) Encerrar(_ context.Context, _, atorId, ordemServicoId int64, payload model.EncerramentoOrdemServicoPayload) (model.OrdemServico, error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = ordemServicoId
	}
	if o.encerramento != nil {
		*o.encerramento = payload
	}
	if o.ator != nil {
		*o.ator = "tecnico/" + strconv.FormatInt(atorId, 10)
	}
	if o.err != nil {
		return model.OrdemServico{}, o.err
	}
	return model.OrdemServico{
		Id: ordemServicoId, SolicitacaoId: 3, Tipo: "maquinario", StatusExecucao: "Concluída",
		Descricao: "Forno não aquece", SetorId: 4, SetorNome: "Padaria",
		LojaId: 1, LojaNome: "Loja A", Finalizada: true,
	}, nil
}

// atorId aqui é o Administrador, não o técnico -- prefixo "administrador/",
// diferente de todo o resto do ciclo de vida acima.
func (o ordemServicoFake) CorrigirCusto(_ context.Context, _, atorId, ordemServicoId int64, payload model.LancamentoCustoManutencaoPayload) (model.OrdemServico, error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = ordemServicoId
	}
	if o.custo != nil {
		*o.custo = payload
	}
	if o.ator != nil {
		*o.ator = "administrador/" + strconv.FormatInt(atorId, 10)
	}
	if o.err != nil {
		return model.OrdemServico{}, o.err
	}
	return model.OrdemServico{
		Id: ordemServicoId, SolicitacaoId: 3, Tipo: "maquinario", StatusExecucao: "Concluída",
		Descricao: "Forno não aquece", SetorId: 4, SetorNome: "Padaria",
		LojaId: 1, LojaNome: "Loja A", Finalizada: true,
	}, nil
}

// contextoOS monta a requisição já autenticada, como o middleware.Autenticacao
// deixaria: tenant, id e perfil no contexto do Gin.
//
// Recebe url.Values e não uma string crua de propósito: é o que o front manda
// de verdade (montarQuery usa URLSearchParams, que escapa o espaço de "Em
// Andamento" e a vírgula do separador). Com a string crua o teste nem chegaria
// no handler -- httptest.NewRequest recusa a URL malformada.
func contextoOS(filtros url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	alvo := "/ordens-servico"
	if q := filtros.Encode(); q != "" {
		alvo += "?" + q
	}
	ctx.Request = httptest.NewRequest(http.MethodGet, alvo, nil)
	ctx.Set(middleware.UserTenantId, int64(7))
	ctx.Set(middleware.UserId, int64(5))
	ctx.Set(middleware.UserPerfil, "gestor")
	return ctx, rec
}

func TestOrdemServicoListarStatus(t *testing.T) {

	casos := []struct {
		nome    string
		err     error
		query   url.Values
		esperar int
	}{
		{"sucesso", nil, nil, http.StatusOK},
		// Só leitura: o único erro possível é do banco, e ele nunca vai cru
		// no corpo -- nome de constraint/coluna fica só no log.
		{"erro do banco", errors.New("connection refused"), nil, http.StatusInternalServerError},
		{"status fora do ENUM", nil, url.Values{"status": {"Cancelada"}}, http.StatusBadRequest},
		{"status válido junto de inválido", nil, url.Values{"status": {"Aberta,Cancelada"}}, http.StatusBadRequest},
		{"tipo fora do ENUM", nil, url.Values{"tipo": {"predial"}}, http.StatusBadRequest},
		{"finalizada não booleano", nil, url.Values{"finalizada": {"sim"}}, http.StatusBadRequest},
		{"lojaId não numérico", nil, url.Values{"lojaId": {"abc"}}, http.StatusBadRequest},
		{"tecnicoId zero", nil, url.Values{"tecnicoId": {"0"}}, http.StatusBadRequest},
		// ?pagina= existe no objeto de parâmetros de algumas telas do front e
		// não muda nada aqui -- recusar quebraria por um campo inofensivo.
		{"pagina é ignorada", nil, url.Values{"pagina": {"2"}}, http.StatusOK},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			ctx, rec := contextoOS(caso.query)
			NewOrdemServicoController(ordemServicoFake{err: caso.err}, "").Listar()(ctx)

			if rec.Code != caso.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, caso.esperar, rec.Body.String())
			}
			if caso.err != nil && strings.Contains(rec.Body.String(), caso.err.Error()) {
				t.Errorf("o erro cru do banco vazou na resposta: %s", rec.Body.String())
			}
		})
	}
}

// ⚠️ montarQuery no front serializa TODO array como uma chave só, separada por
// vírgula (`busca.set(chave, valor.join(','))`) -- ?status=Aberta,Em Andamento,
// não ?status=Aberta&status=Em Andamento. Com ctx.QueryArray o filtro chegaria
// como um item só, com a vírgula dentro, e o cast ::status_os no Postgres
// estouraria em 22P02 -- 500 numa tela que só queria filtrar.
func TestOrdemServicoStatusSeparadoPorVirgula(t *testing.T) {

	var recebidos service.FiltrosOrdemServico
	ctx, rec := contextoOS(url.Values{"status": {"Aberta,Em Andamento,Pausada"}})
	NewOrdemServicoController(ordemServicoFake{filtros: &recebidos}, "").Listar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (corpo: %s)", rec.Code, rec.Body.String())
	}
	esperado := []string{"Aberta", "Em Andamento", "Pausada"}
	if len(recebidos.Status) != len(esperado) {
		t.Fatalf("status = %v, esperado %v", recebidos.Status, esperado)
	}
	for i := range esperado {
		if recebidos.Status[i] != esperado[i] {
			t.Errorf("status[%d] = %q, esperado %q", i, recebidos.Status[i], esperado[i])
		}
	}

	// Um espaço depois da vírgula é o erro humano de teste manual, e o rótulo
	// "Em Andamento" tem espaço DENTRO -- aparar a borda não pode comê-lo.
	var comEspaco service.FiltrosOrdemServico
	ctx2, rec2 := contextoOS(url.Values{"status": {"Aberta, Em Andamento"}})
	NewOrdemServicoController(ordemServicoFake{filtros: &comEspaco}, "").Listar()(ctx2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("espaço depois da vírgula devia passar, veio %d (%s)", rec2.Code, rec2.Body.String())
	}
	if len(comEspaco.Status) != 2 || comEspaco.Status[1] != "Em Andamento" {
		t.Errorf("status = %v, esperado [Aberta, Em Andamento]", comEspaco.Status)
	}
}

func TestOrdemServicoFiltrosChegamNoService(t *testing.T) {

	var recebidos service.FiltrosOrdemServico
	ctx, rec := contextoOS(url.Values{"tipo": {"terceiros"}, "finalizada": {"true"}, "lojaId": {"3"}, "tecnicoId": {"9"}, "busca": {"forno"}})
	NewOrdemServicoController(ordemServicoFake{filtros: &recebidos}, "").Listar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (corpo: %s)", rec.Code, rec.Body.String())
	}
	if recebidos.Tipo == nil || *recebidos.Tipo != "terceiros" {
		t.Errorf("tipo = %v", recebidos.Tipo)
	}
	if recebidos.Finalizada == nil || !*recebidos.Finalizada {
		t.Errorf("finalizada = %v", recebidos.Finalizada)
	}
	if recebidos.LojaId == nil || *recebidos.LojaId != 3 {
		t.Errorf("lojaId = %v", recebidos.LojaId)
	}
	if recebidos.TecnicoId == nil || *recebidos.TecnicoId != 9 {
		t.Errorf("tecnicoId = %v", recebidos.TecnicoId)
	}
	if recebidos.Busca == nil || *recebidos.Busca != "forno" {
		t.Errorf("busca = %v", recebidos.Busca)
	}

	// Sem query nenhuma, tudo nil: "não filtrar" é o modo normal do Painel do
	// Gestor, e um filtro que chega vazio em vez de nil viraria WHERE = ''.
	var vazios service.FiltrosOrdemServico
	ctx2, _ := contextoOS(nil)
	NewOrdemServicoController(ordemServicoFake{filtros: &vazios}, "").Listar()(ctx2)
	if vazios.Status != nil || vazios.Tipo != nil || vazios.Finalizada != nil ||
		vazios.LojaId != nil || vazios.TecnicoId != nil || vazios.Busca != nil {
		t.Errorf("sem query, todos os filtros deviam ser nil: %+v", vazios)
	}

	// ?finalizada=false é um filtro de verdade (a fila de custo pendente), não
	// "não filtrar" -- o zero value do bool não pode engolir a diferença.
	var negativo service.FiltrosOrdemServico
	ctx3, _ := contextoOS(url.Values{"finalizada": {"false"}})
	NewOrdemServicoController(ordemServicoFake{filtros: &negativo}, "").Listar()(ctx3)
	if negativo.Finalizada == nil || *negativo.Finalizada {
		t.Errorf("finalizada = %v, esperado ponteiro para false", negativo.Finalizada)
	}
}

// O ator vem do TOKEN, nunca da query. Se viesse da query, o escopo do
// servidor viraria decoração: um Técnico mandando ?usuarioId= de um gestor
// receberia 200 com a lista dele. O teste de status sozinho não pegaria isso.
func TestOrdemServicoAtorVemDoToken(t *testing.T) {

	var ator string
	var filtros service.FiltrosOrdemServico
	ctx, rec := contextoOS(url.Values{"usuarioId": {"99"}, "perfil": {"administrador"}, "tecnicoId": {"9"}})
	NewOrdemServicoController(ordemServicoFake{ator: &ator, filtros: &filtros}, "").Listar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ator != "gestor/5" {
		t.Errorf("ator = %q, esperado gestor/5 (o do token, não o da query)", ator)
	}
	// tecnicoId é filtro legítimo e continua chegando -- ele estreita a lista
	// dentro do escopo, não substitui o escopo.
	if filtros.TecnicoId == nil || *filtros.TecnicoId != 9 {
		t.Errorf("tecnicoId = %v, devia continuar sendo filtro", filtros.TecnicoId)
	}
}

// Claim ausente é 500 (bug de wiring da rota -- o middleware.Autenticacao já devia ter
// garantido), nunca 401: 401 fora de /login desloga o usuário no front.
func TestOrdemServicoSemClaimDaSessao(t *testing.T) {

	casos := []struct {
		nome    string
		remover string
	}{
		{"sem tenant", middleware.UserTenantId},
		{"sem usuário", middleware.UserId},
		{"sem perfil", middleware.UserPerfil},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/ordens-servico", nil)
			for _, chave := range []string{middleware.UserTenantId, middleware.UserId, middleware.UserPerfil} {
				if chave == caso.remover {
					continue
				}
				if chave == middleware.UserPerfil {
					ctx.Set(chave, "gestor")
				} else {
					ctx.Set(chave, int64(7))
				}
			}

			NewOrdemServicoController(ordemServicoFake{}, "").Listar()(ctx)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, esperado 500", rec.Code)
			}
		})
	}
}

// O front tipa OrdemServico[] -- um corpo `null` quebraria o .map da tela.
func TestOrdemServicoRespondeArray(t *testing.T) {

	ctx, rec := contextoOS(nil)
	NewOrdemServicoController(ordemServicoFake{}, "").Listar()(ctx)

	var corpo []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("corpo não é array: %v (%s)", err, rec.Body.String())
	}
	if len(corpo) != 1 {
		t.Fatalf("esperava 1 OS, veio %d", len(corpo))
	}
	if corpo[0]["statusExecucao"] != "Aberta" || corpo[0]["lojaNome"] != "Loja A" {
		t.Errorf("corpo = %+v", corpo[0])
	}
}

// contextoIndicadores monta GET /indicadores/maquinas/:id já autenticado, com o
// :id que o gin extrairia da rota.
func contextoIndicadores(id string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/indicadores/maquinas/"+id, nil)
	ctx.Params = gin.Params{{Key: "id", Value: id}}
	ctx.Set(middleware.UserTenantId, int64(7))
	ctx.Set(middleware.UserId, int64(5))
	ctx.Set(middleware.UserPerfil, "gestor")
	return ctx, rec
}

func TestIndicadoresStatus(t *testing.T) {

	casos := []struct {
		nome    string
		id      string
		err     error
		esperar int
	}{
		{"sucesso", "9", nil, http.StatusOK},
		// Máquina inexistente e máquina fora do escopo chegam aqui como o
		// mesmo sentinela, e viram o mesmo 404 -- de propósito.
		{"máquina fora do escopo ou inexistente", "9", helper.ErrNaoEncontrado, http.StatusNotFound},
		// :id malformado é erro de cliente, não "não existe" -- mesmo critério
		// de idDaRota no resto do projeto.
		{"id não numérico", "abc", nil, http.StatusBadRequest},
		{"id zero", "0", nil, http.StatusBadRequest},
		{"erro do banco", "9", errors.New("connection refused"), http.StatusInternalServerError},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			ctx, rec := contextoIndicadores(caso.id)
			NewOrdemServicoController(ordemServicoFake{err: caso.err}, "").Indicadores()(ctx)

			if rec.Code != caso.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, caso.esperar, rec.Body.String())
			}
			if caso.err != nil && strings.Contains(rec.Body.String(), caso.err.Error()) {
				t.Errorf("o erro cru vazou na resposta: %s", rec.Body.String())
			}
		})
	}
}

// O ator vem do TOKEN e a máquina da ROTA. Aceitar o usuário de qualquer outro
// lugar deixaria um Gestor ler os indicadores das lojas de outro só trocando um
// parâmetro -- o escopo é aplicado sobre esse id lá no service.
func TestIndicadoresAtorDoTokenEMaquinaDaRota(t *testing.T) {

	var ator string
	var maquinaId int64

	ctx, rec := contextoIndicadores("42")
	NewOrdemServicoController(ordemServicoFake{ator: &ator, maquinaId: &maquinaId}, "").Indicadores()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (corpo: %s)", rec.Code, rec.Body.String())
	}
	if ator != "gestor/5" {
		t.Errorf("ator = %q, esperado \"gestor/5\" (o do token)", ator)
	}
	if maquinaId != 42 {
		t.Errorf("maquinaId = %d, esperado 42 (o da rota)", maquinaId)
	}
}

// O corpo tem que sair com as duas listas montadas mesmo sem histórico: o front
// tipa IndicadoresMaquina sem opcionais e faz .map nas duas -- `null` quebraria
// o painel de uma máquina recém-cadastrada.
func TestIndicadoresCorpoSemHistorico(t *testing.T) {

	ctx, rec := contextoIndicadores("9")
	NewOrdemServicoController(ordemServicoFake{}, "").Indicadores()(ctx)

	var corpo struct {
		MaquinaId      int64 `json:"maquinaId"`
		PorTipoDefeito []struct {
			TipoDefeito string  `json:"tipoDefeito"`
			HorasParada float64 `json:"horasParada"`
		} `json:"porTipoDefeito"`
		PorMes []struct{} `json:"porMes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("corpo inválido: %v (%s)", err, rec.Body.String())
	}

	if corpo.MaquinaId != 9 {
		t.Errorf("maquinaId = %d, esperado 9", corpo.MaquinaId)
	}
	if len(corpo.PorTipoDefeito) != 2 {
		t.Fatalf("porTipoDefeito = %d itens, esperado 2 (a rosca tem legenda fixa)", len(corpo.PorTipoDefeito))
	}
	if corpo.PorMes == nil {
		t.Error("porMes veio null; o front faz .map e quebra")
	}
	if !strings.Contains(rec.Body.String(), `"porMes":[]`) {
		t.Errorf("porMes deveria sair como array vazio: %s", rec.Body.String())
	}
}

// contextoOSAcao monta a requisição de uma transição do ciclo de vida
// (POST /ordens-servico/:id/<ação>), como o middleware.Autenticacao deixaria: id na
// rota, tenant/ator/perfil no contexto. Perfil fixo "tecnico" -- é o único
// que o RBAC da rota libera pra estas ações.
func contextoOSAcao(id, corpo string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	var body *strings.Reader = strings.NewReader(corpo)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/ordens-servico/"+id+"/iniciar", body)
	if corpo != "" {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	ctx.Params = gin.Params{{Key: "id", Value: id}}
	ctx.Set(middleware.UserTenantId, int64(7))
	ctx.Set(middleware.UserId, int64(5))
	ctx.Set(middleware.UserPerfil, "tecnico")
	return ctx, rec
}

func TestOrdemServicoIniciarStatus(t *testing.T) {

	casos := []struct {
		nome    string
		err     error
		esperar int
	}{
		{"sucesso", nil, http.StatusOK},
		{"não encontrada", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"conflito de estado", helper.ErrConflitoIntegridade, http.StatusUnprocessableEntity},
		{"erro genérico", errors.New("erro de banco"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, rec := contextoOSAcao("42", "")
			ctrl := NewOrdemServicoController(ordemServicoFake{err: c.err}, "")

			ctrl.Iniciar()(ctx)

			if rec.Code != c.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, c.esperar, rec.Body.String())
			}
		})
	}
}

func TestOrdemServicoIniciarAtorEIdVemDaRota(t *testing.T) {

	var ator string
	var ordemServicoId int64
	ctx, rec := contextoOSAcao("42", "")

	NewOrdemServicoController(ordemServicoFake{ator: &ator, ordemServicoId: &ordemServicoId}, "").Iniciar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	if ator != "tecnico/5" {
		t.Errorf("ator = %q, esperado tecnico/5 (do token, não do corpo/query)", ator)
	}
	if ordemServicoId != 42 {
		t.Errorf("ordemServicoId = %d, esperado 42 (do :id da rota)", ordemServicoId)
	}
}

func TestOrdemServicoIniciarRespondeOrdemServico(t *testing.T) {

	ctx, rec := contextoOSAcao("42", "")

	NewOrdemServicoController(ordemServicoFake{}, "").Iniciar()(ctx)

	var corpo model.OrdemServico
	if err := json.Unmarshal(rec.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("corpo não decodifica como OrdemServico: %v (corpo: %s)", err, rec.Body.String())
	}
	if corpo.Id != 42 {
		t.Errorf("id = %d, esperado 42", corpo.Id)
	}
	if corpo.StatusExecucao != "Em Andamento" {
		t.Errorf("statusExecucao = %q, esperado Em Andamento", corpo.StatusExecucao)
	}
}

func TestOrdemServicoPausarStatus(t *testing.T) {

	casos := []struct {
		nome    string
		corpo   string
		err     error
		esperar int
	}{
		{"sucesso", `{"motivo":"Aguardando peça"}`, nil, http.StatusOK},
		{"corpo sem motivo", `{}`, nil, http.StatusBadRequest},
		{"corpo malformado", `{`, nil, http.StatusBadRequest},
		{"motivo em branco (validação do service)", `{"motivo":"Aguardando peça"}`, helper.ErrValidacao, http.StatusBadRequest},
		{"não encontrada", `{"motivo":"Aguardando peça"}`, helper.ErrNaoEncontrado, http.StatusNotFound},
		{"conflito de estado", `{"motivo":"Aguardando peça"}`, helper.ErrConflitoIntegridade, http.StatusUnprocessableEntity},
		{"erro genérico", `{"motivo":"Aguardando peça"}`, errors.New("erro de banco"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, rec := contextoOSAcao("42", c.corpo)
			ctrl := NewOrdemServicoController(ordemServicoFake{err: c.err}, "")

			ctrl.Pausar()(ctx)

			if rec.Code != c.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, c.esperar, rec.Body.String())
			}
		})
	}
}

func TestOrdemServicoPausarAtorIdEMotivo(t *testing.T) {

	var ator, motivo string
	var ordemServicoId int64
	ctx, rec := contextoOSAcao("42", `{"motivo":"Aguardando peça"}`)

	NewOrdemServicoController(ordemServicoFake{ator: &ator, motivo: &motivo, ordemServicoId: &ordemServicoId}, "").Pausar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	if ator != "tecnico/5" {
		t.Errorf("ator = %q, esperado tecnico/5 (do token, não do corpo)", ator)
	}
	if ordemServicoId != 42 {
		t.Errorf("ordemServicoId = %d, esperado 42 (do :id da rota)", ordemServicoId)
	}
	if motivo != "Aguardando peça" {
		t.Errorf("motivo = %q, esperado %q", motivo, "Aguardando peça")
	}
}

func TestOrdemServicoRetomarStatus(t *testing.T) {

	casos := []struct {
		nome    string
		err     error
		esperar int
	}{
		{"sucesso", nil, http.StatusOK},
		{"não encontrada", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"conflito de estado", helper.ErrConflitoIntegridade, http.StatusUnprocessableEntity},
		{"erro genérico", errors.New("erro de banco"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, rec := contextoOSAcao("42", "")
			ctrl := NewOrdemServicoController(ordemServicoFake{err: c.err}, "")

			ctrl.Retomar()(ctx)

			if rec.Code != c.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, c.esperar, rec.Body.String())
			}
		})
	}
}

func TestOrdemServicoRetomarAtorEIdVemDaRota(t *testing.T) {

	var ator string
	var ordemServicoId int64
	ctx, rec := contextoOSAcao("42", "")

	NewOrdemServicoController(ordemServicoFake{ator: &ator, ordemServicoId: &ordemServicoId}, "").Retomar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	if ator != "tecnico/5" {
		t.Errorf("ator = %q, esperado tecnico/5 (do token, não do corpo/query)", ator)
	}
	if ordemServicoId != 42 {
		t.Errorf("ordemServicoId = %d, esperado 42 (do :id da rota)", ordemServicoId)
	}
}

func TestOrdemServicoAcionarTerceiroStatus(t *testing.T) {

	casos := []struct {
		nome    string
		corpo   string
		err     error
		esperar int
	}{
		{"sucesso", `{"empresaTerceirizadaId":9}`, nil, http.StatusOK},
		{"corpo sem empresaTerceirizadaId", `{}`, nil, http.StatusBadRequest},
		{"empresaTerceirizadaId zero", `{"empresaTerceirizadaId":0}`, nil, http.StatusBadRequest},
		{"corpo malformado", `{`, nil, http.StatusBadRequest},
		{"não encontrada", `{"empresaTerceirizadaId":9}`, helper.ErrNaoEncontrado, http.StatusNotFound},
		{"conflito de estado", `{"empresaTerceirizadaId":9}`, helper.ErrConflitoIntegridade, http.StatusUnprocessableEntity},
		{"erro genérico", `{"empresaTerceirizadaId":9}`, errors.New("erro de banco"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, rec := contextoOSAcao("42", c.corpo)
			ctrl := NewOrdemServicoController(ordemServicoFake{err: c.err}, "")

			ctrl.AcionarTerceiro()(ctx)

			if rec.Code != c.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, c.esperar, rec.Body.String())
			}
		})
	}
}

func TestOrdemServicoAcionarTerceiroAtorIdEEmpresa(t *testing.T) {

	var ator string
	var ordemServicoId, empresaTerceirizadaId int64
	ctx, rec := contextoOSAcao("42", `{"empresaTerceirizadaId":9}`)

	NewOrdemServicoController(ordemServicoFake{
		ator: &ator, ordemServicoId: &ordemServicoId, empresaTerceirizadaId: &empresaTerceirizadaId,
	}, "").AcionarTerceiro()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	if ator != "tecnico/5" {
		t.Errorf("ator = %q, esperado tecnico/5 (do token, não do corpo)", ator)
	}
	if ordemServicoId != 42 {
		t.Errorf("ordemServicoId = %d, esperado 42 (do :id da rota)", ordemServicoId)
	}
	if empresaTerceirizadaId != 9 {
		t.Errorf("empresaTerceirizadaId = %d, esperado 9", empresaTerceirizadaId)
	}
}

const dadosEncerramentoValidos = `{"tipoDefeito":"Corretiva","defeitoConstatado":"Resistência queimada","causaRaiz":"Desgaste natural","solucao":"Troca da resistência","itens":[{"descricao":"Troca do rolamento","custoManutencao":120.5,"custoHoraTecnico":45}]}`

func TestOrdemServicoEncerrarStatus(t *testing.T) {

	casos := []struct {
		nome    string
		corpo   string
		err     error
		esperar int
	}{
		{"sucesso", dadosEncerramentoValidos, nil, http.StatusOK},
		{"corpo malformado", `{`, nil, http.StatusBadRequest},
		{"tipoDefeito fora do oneof", `{"tipoDefeito":"Mecânico","defeitoConstatado":"x","causaRaiz":"y","solucao":"z","custoManutencao":10}`, nil, http.StatusBadRequest},
		{"sem defeitoConstatado", `{"tipoDefeito":"Predial","causaRaiz":"y","solucao":"z","custoManutencao":10}`, nil, http.StatusBadRequest},
		{"custoManutencao negativo", `{"tipoDefeito":"Predial","defeitoConstatado":"x","causaRaiz":"y","solucao":"z","custoManutencao":-10}`, nil, http.StatusBadRequest},
		{"regra de tipo violada (validação do service)", dadosEncerramentoValidos, helper.ErrValidacao, http.StatusBadRequest},
		{"não encontrada", dadosEncerramentoValidos, helper.ErrNaoEncontrado, http.StatusNotFound},
		{"conflito de estado", dadosEncerramentoValidos, helper.ErrConflitoIntegridade, http.StatusUnprocessableEntity},
		{"erro genérico", dadosEncerramentoValidos, errors.New("erro de banco"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, rec := contextoOSAcao("42", c.corpo)
			ctrl := NewOrdemServicoController(ordemServicoFake{err: c.err}, "")

			ctrl.Encerrar()(ctx)

			if rec.Code != c.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, c.esperar, rec.Body.String())
			}
		})
	}
}

func TestOrdemServicoEncerrarAtorIdEPayload(t *testing.T) {

	var ator string
	var ordemServicoId int64
	var recebido model.EncerramentoOrdemServicoPayload
	ctx, rec := contextoOSAcao("42", dadosEncerramentoValidos)

	NewOrdemServicoController(ordemServicoFake{
		ator: &ator, ordemServicoId: &ordemServicoId, encerramento: &recebido,
	}, "").Encerrar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	if ator != "tecnico/5" {
		t.Errorf("ator = %q, esperado tecnico/5 (do token, não do corpo)", ator)
	}
	if ordemServicoId != 42 {
		t.Errorf("ordemServicoId = %d, esperado 42 (do :id da rota)", ordemServicoId)
	}
	if recebido.TipoDefeito != "Corretiva" || recebido.Solucao != "Troca da resistência" {
		t.Errorf("payload = %+v, não bateu com o corpo enviado", recebido)
	}
	// A lista chega inteira e na ordem enviada -- é ela que o service soma.
	if len(recebido.Itens) != 1 {
		t.Fatalf("itens = %+v, esperado 1 tarefa", recebido.Itens)
	}
	tarefa := recebido.Itens[0]
	if tarefa.Descricao != "Troca do rolamento" || tarefa.CustoManutencao != 120.5 {
		t.Errorf("tarefa = %+v, esperado Troca do rolamento com 120.5 de peça", tarefa)
	}
	// Ponteiro, e não valor: nulo distingue "não cobrou hora" de "cobrou zero".
	if tarefa.CustoHoraTecnico == nil || *tarefa.CustoHoraTecnico != 45 {
		t.Errorf("custoHoraTecnico = %v, esperado 45", tarefa.CustoHoraTecnico)
	}
}

const dadosCustoValidos = `{"itens":[{"descricao":"Troca do rolamento","custoManutencao":120.5,"custoHoraTecnico":45}]}`

func TestOrdemServicoCustoStatus(t *testing.T) {

	casos := []struct {
		nome    string
		corpo   string
		err     error
		esperar int
	}{
		{"sucesso", dadosCustoValidos, nil, http.StatusOK},
		{"corpo malformado", `{`, nil, http.StatusBadRequest},
		{"custo de manutenção negativo", `{"itens":[{"descricao":"Peça","custoManutencao":-10}]}`, nil, http.StatusBadRequest},
		{"custo hora do técnico negativo", `{"itens":[{"descricao":"Peça","custoManutencao":10,"custoHoraTecnico":-1}]}`, nil, http.StatusBadRequest},
		{"tarefa sem descrição", `{"itens":[{"custoManutencao":10}]}`, nil, http.StatusBadRequest},
		{"lista de tarefas vazia", `{"itens":[]}`, nil, http.StatusBadRequest},
		{"regra de tipo violada (validação do service)", dadosCustoValidos, helper.ErrValidacao, http.StatusBadRequest},
		{"não encontrada", dadosCustoValidos, helper.ErrNaoEncontrado, http.StatusNotFound},
		{"conflito de estado (OS ainda não encerrada)", dadosCustoValidos, helper.ErrConflitoIntegridade, http.StatusUnprocessableEntity},
		// uq_nota_fiscal_os: a mesma nota lançada duas vezes na lista. Só virou
		// alcançável na migration 000012 -- antes esta rota era um UPDATE 1:1 em
		// os_custo, sem como colidir --, e sem este caso no switch do controller
		// o Administrador levava 500 no lugar de "essa nota você já lançou".
		{"nota fiscal repetida", dadosCustoValidos, helper.ErrDadoDuplicado, http.StatusConflict},
		{"erro genérico", dadosCustoValidos, errors.New("erro de banco"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ctx, rec := contextoOSAcao("42", c.corpo)
			ctrl := NewOrdemServicoController(ordemServicoFake{err: c.err}, "")

			ctrl.Custo()(ctx)

			if rec.Code != c.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, c.esperar, rec.Body.String())
			}
		})
	}
}

// atorId aqui vem do mesmo claim que os outros (UserId do token, ver
// contextoOSAcao) -- quem garante que só Administrador chega até aqui é o
// RBAC da rota (Permitir("administrador")), não o controller.
func TestOrdemServicoCustoAtorIdEPayload(t *testing.T) {

	var ator string
	var ordemServicoId int64
	var recebido model.LancamentoCustoManutencaoPayload
	ctx, rec := contextoOSAcao("42", dadosCustoValidos)

	NewOrdemServicoController(ordemServicoFake{
		ator: &ator, ordemServicoId: &ordemServicoId, custo: &recebido,
	}, "").Custo()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", rec.Code, rec.Body.String())
	}
	if ator != "administrador/5" {
		t.Errorf("ator = %q, esperado administrador/5 (do token, não do corpo)", ator)
	}
	if ordemServicoId != 42 {
		t.Errorf("ordemServicoId = %d, esperado 42 (do :id da rota)", ordemServicoId)
	}
	if len(recebido.Itens) != 1 {
		t.Fatalf("itens = %+v, esperado 1 tarefa", recebido.Itens)
	}
	if recebido.Itens[0].CustoManutencao != 120.5 {
		t.Errorf("custoManutencao da tarefa = %v, esperado 120.5", recebido.Itens[0].CustoManutencao)
	}
}

// ?pagina= troca o formato da resposta (RespostaPaginada em vez de array) e
// leva os mesmos filtros; sem ela, a listagem continua array -- é o que o
// Gestor e o Técnico consomem.
func TestOrdemServicoPaginacaoOpcional(t *testing.T) {

	var pagina int32
	var filtros service.FiltrosOrdemServico
	ctx, rec := contextoOS(url.Values{"pagina": {"3"}, "finalizada": {"true"}, "revisado": {"false"}})
	NewOrdemServicoController(ordemServicoFake{pagina: &pagina, filtros: &filtros}, "").Listar()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (corpo: %s)", rec.Code, rec.Body.String())
	}
	if pagina != 3 {
		t.Errorf("pagina = %d, esperado 3", pagina)
	}
	if filtros.Finalizada == nil || !*filtros.Finalizada || filtros.Revisado == nil || *filtros.Revisado {
		t.Errorf("filtros não chegaram na paginada: %+v", filtros)
	}
	if !strings.Contains(rec.Body.String(), `"totalPaginas"`) {
		t.Errorf("com ?pagina= a resposta devia ser RespostaPaginada: %s", rec.Body.String())
	}

	var semPagina int32
	ctx2, rec2 := contextoOS(url.Values{})
	NewOrdemServicoController(ordemServicoFake{pagina: &semPagina}, "").Listar()(ctx2)
	if semPagina != 0 || !strings.HasPrefix(strings.TrimSpace(rec2.Body.String()), "[") {
		t.Errorf("sem ?pagina= devia ser array pela listagem inteira (pagina=%d, corpo=%s)", semPagina, rec2.Body.String())
	}

	for _, ruim := range []string{"0", "-1", "abc"} {
		ctx3, rec3 := contextoOS(url.Values{"pagina": {ruim}})
		NewOrdemServicoController(ordemServicoFake{}, "").Listar()(ctx3)
		if rec3.Code != http.StatusBadRequest {
			t.Errorf("pagina=%s: status = %d, esperado 400", ruim, rec3.Code)
		}
	}

	ctx4, rec4 := contextoOS(url.Values{"revisado": {"talvez"}})
	NewOrdemServicoController(ordemServicoFake{}, "").Listar()(ctx4)
	if rec4.Code != http.StatusBadRequest {
		t.Errorf("revisado=talvez: status = %d, esperado 400", rec4.Code)
	}
}

func (o ordemServicoFake) ListarHistorico(_ context.Context, _, usuarioId int64, perfil string, ordemServicoId int64) ([]model.EventoOrdemServico, error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = ordemServicoId
	}
	if o.ator != nil {
		*o.ator = perfil + "/" + strconv.FormatInt(usuarioId, 10)
	}
	if o.err != nil {
		return nil, o.err
	}
	return model.MontarEventosOrdemServico(nil), nil
}

func TestHistoricoStatus(t *testing.T) {

	casos := []struct {
		nome    string
		id      string
		err     error
		esperar int
	}{
		{"sucesso", "9", nil, http.StatusOK},
		// OS fora do alcance e OS inexistente são o mesmo 404 de propósito.
		{"fora do escopo ou inexistente", "9", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"id não numérico", "abc", nil, http.StatusBadRequest},
		{"erro do banco", "9", errors.New("connection refused"), http.StatusInternalServerError},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			ctx, rec := contextoIndicadores(caso.id)
			NewOrdemServicoController(ordemServicoFake{err: caso.err}, "").Historico()(ctx)

			if rec.Code != caso.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, caso.esperar, rec.Body.String())
			}
			if caso.err != nil && strings.Contains(rec.Body.String(), caso.err.Error()) {
				t.Errorf("o erro cru vazou na resposta: %s", rec.Body.String())
			}
		})
	}
}

// Ator do TOKEN, OS da ROTA, e `[]` (nunca `null`) sem eventos -- o front faz
// .map na lista.
func TestHistoricoAtorDoTokenEListaVazia(t *testing.T) {

	var ator string
	var osId int64

	ctx, rec := contextoIndicadores("42")
	NewOrdemServicoController(ordemServicoFake{ator: &ator, ordemServicoId: &osId}, "").Historico()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (corpo: %s)", rec.Code, rec.Body.String())
	}
	if ator != "gestor/5" || osId != 42 {
		t.Errorf("ator = %q, os = %d; esperado gestor/5 e 42", ator, osId)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("corpo = %s, esperado []", rec.Body.String())
	}
}

func (o ordemServicoFake) ResumoOrdensServicoPorSetor(_ context.Context, _, usuarioId int64, perfil string, f service.FiltrosOrdemServico) ([]model.ResumoOrdensServicoSetor, error) {
	if o.filtros != nil {
		*o.filtros = f
	}
	if o.ator != nil {
		*o.ator = perfil + "/" + strconv.FormatInt(usuarioId, 10)
	}
	if o.err != nil {
		return nil, o.err
	}
	return []model.ResumoOrdensServicoSetor{}, nil
}

func (o ordemServicoFake) ObterOrdemServico(_ context.Context, _, _ int64, _ string, id int64) (model.OrdemServico, error) {
	if o.err != nil {
		return model.OrdemServico{}, o.err
	}
	return model.OrdemServico{Id: id, SolicitacaoId: 3, Tipo: "maquinario", StatusExecucao: "Aberta", SetorId: 4, LojaId: 1}, nil
}

// Os filtros avançados do Painel do Gestor chegam no service pelas DUAS rotas
// (listagem e resumo): se uma aceitasse e a outra não, o card diria uma
// quantidade e a lista mostraria outra.
func TestOrdemServicoFiltrosAvancadosChegamNoService(t *testing.T) {

	consulta := url.Values{
		"maquina": {" PAT-9 "}, "dataInicio": {"2026-08-01"}, "dataFim": {"2026-08-31"},
		"valorMinimo": {"100"}, "valorMaximo": {"900.5"}, "finalizada": {"true"},
	}

	conferir := func(t *testing.T, f service.FiltrosOrdemServico) {
		t.Helper()
		if f.Maquina == nil || *f.Maquina != "PAT-9" {
			t.Errorf("maquina = %v, esperado PAT-9 (sem os espaços)", f.Maquina)
		}
		if f.DataInicio == nil || f.DataInicio.Format("2006-01-02") != "2026-08-01" || f.DataFim == nil || f.DataFim.Format("2006-01-02") != "2026-08-31" {
			t.Errorf("período = %v a %v", f.DataInicio, f.DataFim)
		}
		if f.ValorMinimo == nil || *f.ValorMinimo != 100 || f.ValorMaximo == nil || *f.ValorMaximo != 900.5 {
			t.Errorf("valores = %v a %v", f.ValorMinimo, f.ValorMaximo)
		}
	}

	t.Run("listagem", func(t *testing.T) {
		var f service.FiltrosOrdemServico
		ctx, rec := contextoOS(consulta)
		NewOrdemServicoController(ordemServicoFake{filtros: &f}, "").Listar()(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
		}
		conferir(t, f)
	})

	t.Run("resumo", func(t *testing.T) {
		var f service.FiltrosOrdemServico
		ctx, rec := contextoOS(consulta)
		NewOrdemServicoController(ordemServicoFake{filtros: &f}, "").Resumo()(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
		}
		conferir(t, f)
		if strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Errorf("corpo = %s, esperado [] (nunca null)", rec.Body.String())
		}
	})

	// Lixo é erro de cliente: ignorar calado devolveria a lista inteira para
	// quem pediu um recorte.
	for nome, valores := range map[string]url.Values{
		"data inválida":  {"dataInicio": {"31/08/2026"}},
		"valor inválido": {"valorMinimo": {"abc"}},
		"valor negativo": {"valorMaximo": {"-1"}},
	} {
		t.Run(nome, func(t *testing.T) {
			ctx, rec := contextoOS(valores)
			NewOrdemServicoController(ordemServicoFake{}, "").Resumo()(ctx)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, esperado 400", rec.Code)
			}
		})
	}
}

func TestOrdemServicoObterStatus(t *testing.T) {

	casos := []struct {
		nome    string
		id      string
		err     error
		esperar int
	}{
		{"sucesso", "9", nil, http.StatusOK},
		{"fora do escopo ou inexistente", "9", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"id não numérico", "abc", nil, http.StatusBadRequest},
		{"erro do banco", "9", errors.New("connection refused"), http.StatusInternalServerError},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			ctx, rec := contextoIndicadores(caso.id)
			NewOrdemServicoController(ordemServicoFake{err: caso.err}, "").Obter()(ctx)
			if rec.Code != caso.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, caso.esperar, rec.Body.String())
			}
		})
	}
}

func (o ordemServicoFake) ListarOrdensDoPainelDaLoja(_ context.Context, _, lojaId, _ int64, _, grupo string, f service.FiltrosOrdensDoPainel, pagina int32) (model.RespostaPaginada[model.OrdemDoPainel], error) {
	if o.ordemServicoId != nil {
		*o.ordemServicoId = lojaId
	}
	if o.painelRecebido != nil {
		partes := []string{"grupo=" + grupo, "setor=", "maquina=", "mes=", "pagina=" + strconv.Itoa(int(pagina))}
		if f.SetorId != nil {
			partes[1] += strconv.FormatInt(*f.SetorId, 10)
		}
		if f.MaquinaId != nil {
			partes[2] += strconv.FormatInt(*f.MaquinaId, 10)
		}
		if f.Mes != nil {
			partes[3] += *f.Mes
		}
		*o.painelRecebido = strings.Join(partes, ",")
	}
	if o.err != nil {
		return model.RespostaPaginada[model.OrdemDoPainel]{}, o.err
	}
	return model.RespostaPaginada[model.OrdemDoPainel]{Dados: model.MontarReparosEncerrados(nil), Pagina: pagina, TotalPaginas: 1}, nil
}

func contextoOrdensDoPainel(id string, consulta url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	ctx, rec := contextoIndicadores(id)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/indicadores/lojas/"+id+"/ordens?"+consulta.Encode(), nil)
	return ctx, rec
}

func TestOrdensDoPainelDaLojaStatus(t *testing.T) {

	casos := []struct {
		nome     string
		id       string
		consulta url.Values
		err      error
		esperar  int
	}{
		{"maquinário", "9", url.Values{"grupo": {"maquinario"}}, nil, http.StatusOK},
		{"reparos", "9", url.Values{"grupo": {"reparos"}}, nil, http.StatusOK},
		{"sem grupo", "9", nil, nil, http.StatusBadRequest},
		{"grupo desconhecido", "9", url.Values{"grupo": {"tudo"}}, nil, http.StatusBadRequest},
		{"loja fora do escopo ou inexistente", "9", url.Values{"grupo": {"reparos"}}, helper.ErrNaoEncontrado, http.StatusNotFound},
		{"id de loja inválido", "abc", url.Values{"grupo": {"reparos"}}, nil, http.StatusBadRequest},
		{"mês em formato errado", "9", url.Values{"grupo": {"reparos"}, "mes": {"2026-07"}}, nil, http.StatusBadRequest},
		{"mês impossível", "9", url.Values{"grupo": {"reparos"}, "mes": {"13/2026"}}, nil, http.StatusBadRequest},
		{"setor inválido", "9", url.Values{"grupo": {"reparos"}, "setorId": {"x"}}, nil, http.StatusBadRequest},
		{"página inválida", "9", url.Values{"grupo": {"reparos"}, "pagina": {"0"}}, nil, http.StatusBadRequest},
		// Reparo não tem máquina: filtrar por ela não faz sentido e é recusado.
		{"máquina em reparos", "9", url.Values{"grupo": {"reparos"}, "maquinaId": {"3"}}, nil, http.StatusBadRequest},
		{"máquina em maquinário", "9", url.Values{"grupo": {"maquinario"}, "maquinaId": {"3"}}, nil, http.StatusOK},
		{"erro do banco", "9", url.Values{"grupo": {"reparos"}}, errors.New("connection refused"), http.StatusInternalServerError},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			ctx, rec := contextoOrdensDoPainel(caso.id, caso.consulta)
			NewOrdemServicoController(ordemServicoFake{err: caso.err}, "").OrdensDoPainelDaLoja()(ctx)
			if rec.Code != caso.esperar {
				t.Errorf("status = %d, esperado %d (corpo: %s)", rec.Code, caso.esperar, rec.Body.String())
			}
		})
	}
}

// O mês chega como MM/AAAA (o rótulo do gráfico) e vai ao service como AAAA-MM, o que a query
// compara; grupo, setor, máquina e página chegam como vieram.
func TestOrdensDoPainelDaLojaFiltrosChegamNoService(t *testing.T) {

	var recebido string
	ctx, rec := contextoOrdensDoPainel("9", url.Values{"grupo": {"maquinario"}, "mes": {"07/2026"}, "setorId": {"4"}, "maquinaId": {"7"}, "pagina": {"2"}})
	NewOrdemServicoController(ordemServicoFake{painelRecebido: &recebido}, "").OrdensDoPainelDaLoja()(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if recebido != "grupo=maquinario,setor=4,maquina=7,mes=2026-07,pagina=2" {
		t.Errorf("service recebeu %q", recebido)
	}
	if !strings.Contains(rec.Body.String(), `"dados":[]`) {
		t.Errorf("corpo = %s, esperado dados [] (nunca null)", rec.Body.String())
	}
}
