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

// contextoOS monta a requisição já autenticada, como o AutenticacaoJwt
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
			NewOrdemServicoController(ordemServicoFake{err: caso.err}).Listar()(ctx)

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
	NewOrdemServicoController(ordemServicoFake{filtros: &recebidos}).Listar()(ctx)

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
	NewOrdemServicoController(ordemServicoFake{filtros: &comEspaco}).Listar()(ctx2)
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
	NewOrdemServicoController(ordemServicoFake{filtros: &recebidos}).Listar()(ctx)

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
	NewOrdemServicoController(ordemServicoFake{filtros: &vazios}).Listar()(ctx2)
	if vazios.Status != nil || vazios.Tipo != nil || vazios.Finalizada != nil ||
		vazios.LojaId != nil || vazios.TecnicoId != nil || vazios.Busca != nil {
		t.Errorf("sem query, todos os filtros deviam ser nil: %+v", vazios)
	}

	// ?finalizada=false é um filtro de verdade (a fila de custo pendente), não
	// "não filtrar" -- o zero value do bool não pode engolir a diferença.
	var negativo service.FiltrosOrdemServico
	ctx3, _ := contextoOS(url.Values{"finalizada": {"false"}})
	NewOrdemServicoController(ordemServicoFake{filtros: &negativo}).Listar()(ctx3)
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
	NewOrdemServicoController(ordemServicoFake{ator: &ator, filtros: &filtros}).Listar()(ctx)

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

// Claim ausente é 500 (bug de wiring da rota -- o AutenticacaoJwt já devia ter
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

			NewOrdemServicoController(ordemServicoFake{}).Listar()(ctx)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, esperado 500", rec.Code)
			}
		})
	}
}

// O front tipa OrdemServico[] -- um corpo `null` quebraria o .map da tela.
func TestOrdemServicoRespondeArray(t *testing.T) {

	ctx, rec := contextoOS(nil)
	NewOrdemServicoController(ordemServicoFake{}).Listar()(ctx)

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
			NewOrdemServicoController(ordemServicoFake{err: caso.err}).Indicadores()(ctx)

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
	NewOrdemServicoController(ordemServicoFake{ator: &ator, maquinaId: &maquinaId}).Indicadores()(ctx)

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
	NewOrdemServicoController(ordemServicoFake{}).Indicadores()(ctx)

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
