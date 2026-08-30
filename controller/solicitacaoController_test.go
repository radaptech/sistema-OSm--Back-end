package controller

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

// solicitacaoFake grava o que recebeu, não só o erro que devolve -- mesmo
// critério de maquinaFake: payload/ator vindo do lugar errado não muda status
// nenhum, então o teste precisa olhar o que o service recebeu de verdade.
type solicitacaoFake struct {
	err error

	maquinario *model.NovaSolicitacaoMaquinarioPayload
	reparo     *model.NovaSolicitacaoReparoPayload
	abrirOS    *model.AberturaOrdemServicoPayload
	motivo     *string
	// ator recebido, como "usuarioId/perfil".
	ator *string

	minhaPagina *int32
	filtroLoja  *int64

	chaveAnexo string // devolvida em Anexos[0].Url, como o service faz.
}

func (s solicitacaoFake) resposta() model.SolicitacaoOS {
	return model.SolicitacaoOS{
		Id: 1, Tipo: "maquinario", Status: "Pendente", Descricao: "Barulho estranho",
		SetorId: 9, SetorNome: "Padaria", LojaId: 2, LojaNome: "Loja Sul",
		Origem: "solicitante", Impactos: []string{}, Anexos: []model.AnexoSolicitacao{{Id: 1, Tipo: "foto", Url: &s.chaveAnexo}},
	}
}

func (s solicitacaoFake) CadastrarSolicitacaoMaquinario(_ context.Context, _, _ int64, p model.NovaSolicitacaoMaquinarioPayload) (model.SolicitacaoOS, error) {
	if s.maquinario != nil {
		*s.maquinario = p
	}
	if s.err != nil {
		return model.SolicitacaoOS{}, s.err
	}
	return s.resposta(), nil
}

func (s solicitacaoFake) CadastrarSolicitacaoReparo(_ context.Context, _, _ int64, p model.NovaSolicitacaoReparoPayload) (model.SolicitacaoOS, error) {
	if s.reparo != nil {
		*s.reparo = p
	}
	if s.err != nil {
		return model.SolicitacaoOS{}, s.err
	}
	return s.resposta(), nil
}

func (s solicitacaoFake) ListarMinhasSolicitacoes(_ context.Context, _, usuarioId int64, pagina int32, _, _ *string) (model.RespostaPaginada[model.SolicitacaoOS], error) {
	if s.ator != nil {
		*s.ator = fmt.Sprintf("%d", usuarioId)
	}
	if s.minhaPagina != nil {
		*s.minhaPagina = pagina
	}
	if s.err != nil {
		return model.RespostaPaginada[model.SolicitacaoOS]{}, s.err
	}
	return model.RespostaPaginada[model.SolicitacaoOS]{Dados: []model.SolicitacaoOS{s.resposta()}, Pagina: pagina, TotalPaginas: 1, Total: 1}, nil
}

func (s solicitacaoFake) ListarSolicitacoes(_ context.Context, _, usuarioId int64, perfil string, _, _, _ *string, lojaId *int64) ([]model.SolicitacaoOS, error) {
	if s.ator != nil {
		*s.ator = fmt.Sprintf("%d/%s", usuarioId, perfil)
	}
	if s.filtroLoja != nil {
		*s.filtroLoja = derefOrZero(lojaId)
	}
	if s.err != nil {
		return nil, s.err
	}
	return []model.SolicitacaoOS{s.resposta()}, nil
}

func derefOrZero(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (s solicitacaoFake) ObterSolicitacao(_ context.Context, _, usuarioId int64, perfil string, _ int64) (model.SolicitacaoOS, error) {
	if s.ator != nil {
		*s.ator = fmt.Sprintf("%d/%s", usuarioId, perfil)
	}
	if s.err != nil {
		return model.SolicitacaoOS{}, s.err
	}
	return s.resposta(), nil
}

func (s solicitacaoFake) ObterResumo(_ context.Context, _, usuarioId int64) (model.ResumoSolicitacoes, error) {
	if s.ator != nil {
		*s.ator = fmt.Sprintf("%d", usuarioId)
	}
	if s.err != nil {
		return model.ResumoSolicitacoes{}, s.err
	}
	return model.ResumoSolicitacoes{Abertas: 2}, nil
}

func (s solicitacaoFake) AbrirOS(_ context.Context, _, atorId int64, perfil string, _ int64, p model.AberturaOrdemServicoPayload) (model.OrdemServico, error) {
	if s.abrirOS != nil {
		*s.abrirOS = p
	}
	if s.ator != nil {
		*s.ator = fmt.Sprintf("%d/%s", atorId, perfil)
	}
	if s.err != nil {
		return model.OrdemServico{}, s.err
	}
	return model.OrdemServico{Id: 1, SolicitacaoId: 1, StatusExecucao: "Aberta"}, nil
}

func (s solicitacaoFake) Rejeitar(_ context.Context, _, atorId int64, perfil string, _ int64, motivo string) (model.SolicitacaoOS, error) {
	if s.motivo != nil {
		*s.motivo = motivo
	}
	if s.ator != nil {
		*s.ator = fmt.Sprintf("%d/%s", atorId, perfil)
	}
	if s.err != nil {
		return model.SolicitacaoOS{}, s.err
	}
	r := s.resposta()
	r.Status = "Rejeitada"
	return r, nil
}

const bucketAnexos = "bucket-teste"

func novoController(fake solicitacaoFake) *SolicitacaoController {
	return NewSolicitacaoController(fake, bucketAnexos, bucketAnexos, bucketAnexos)
}

type arquivoMultipart struct {
	campo, nome, contentType string
	conteudo                 []byte
}

// requisicaoSolicitacao monta o multipart que o front manda (montarMultipart):
// `dados` + as partes de arquivo, cada uma com Content-Type explícito --
// CreateFormFile sozinho sempre grava application/octet-stream, e é
// exatamente o Content-Type que chaveDoUpload confere.
func requisicaoSolicitacao(metodo, path, dados string, arquivos ...arquivoMultipart) (*httptest.ResponseRecorder, *gin.Context) {

	var corpo bytes.Buffer
	form := multipart.NewWriter(&corpo)

	if dados != "" {
		_ = form.WriteField("dados", dados)
	}
	for _, a := range arquivos {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, a.campo, a.nome))
		h.Set("Content-Type", a.contentType)
		parte, _ := form.CreatePart(h)
		_, _ = parte.Write(a.conteudo)
	}
	_ = form.Close()

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, path, &corpo)
	ctx.Request.Header.Set("Content-Type", form.FormDataContentType())
	ctx.Set(middleware.UserTenantId, int64(7))
	ctx.Set(middleware.UserId, int64(1))
	ctx.Set(middleware.UserPerfil, "solicitante")
	return w, ctx
}

// requisicaoJSON é para as rotas sem arquivo (abrir-os, rejeitar) e as de
// leitura (minhas, listar, obter, resumo).
func requisicaoJSON(metodo, path, id, corpo string) (*httptest.ResponseRecorder, *gin.Context) {

	var body *strings.Reader = strings.NewReader(corpo)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, path, body)
	if corpo != "" {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	ctx.Set(middleware.UserTenantId, int64(7))
	ctx.Set(middleware.UserId, int64(1))
	ctx.Set(middleware.UserPerfil, "gestor")
	return w, ctx
}

const dadosMaquinario = `{"maquinaId":9,"descricao":"Barulho estranho","impactos":["Afeta Produção"]}`
const dadosReparo = `{"item":"Lâmpada queimada","descricao":"Corredor principal"}`

func fotoValida() arquivoMultipart {
	return arquivoMultipart{campo: "foto", nome: "foto.jpg", contentType: "image/jpeg", conteudo: []byte("bytes-de-mentira")}
}

func TestSolicitacaoMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	foraDoEscopo := helper.ErrNaoEncontrado
	jaConvertida := fmt.Errorf("%w: solicitação já está Convertida", helper.ErrConflitoIntegridade)

	casos := []struct {
		nome   string
		acao   string
		err    error
		status int
	}{
		{"obter sucesso", "obter", nil, http.StatusOK},
		{"obter fora do escopo é 404", "obter", foraDoEscopo, http.StatusNotFound},
		{"obter interno", "obter", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},

		{"abrir-os sucesso", "abrir-os", nil, http.StatusCreated},
		{"abrir-os inexistente", "abrir-os", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"abrir-os já convertida", "abrir-os", jaConvertida, http.StatusUnprocessableEntity},
		{"abrir-os interno", "abrir-os", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},

		{"rejeitar sucesso", "rejeitar", nil, http.StatusOK},
		{"rejeitar motivo vazio", "rejeitar", fmt.Errorf("%w: motivo é obrigatório", helper.ErrValidacao), http.StatusBadRequest},
		{"rejeitar inexistente", "rejeitar", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"rejeitar já rejeitada", "rejeitar", jaConvertida, http.StatusUnprocessableEntity},

		{"listar interno", "listar", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},
		{"minhas interno", "minhas", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},
		{"resumo interno", "resumo", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			ctrl := novoController(solicitacaoFake{err: c.err})

			var w *httptest.ResponseRecorder
			var ctx *gin.Context

			switch c.acao {
			case "obter":
				w, ctx = requisicaoJSON(http.MethodGet, "/solicitacoes/1", "1", "")
				ctrl.Obter()(ctx)
			case "abrir-os":
				w, ctx = requisicaoJSON(http.MethodPost, "/solicitacoes/1/abrir-os", "1", `{"urgencia":"Alta","tecnicoId":5}`)
				ctrl.AbrirOS()(ctx)
			case "rejeitar":
				w, ctx = requisicaoJSON(http.MethodPost, "/solicitacoes/1/rejeitar", "1", `{"motivo":"Já resolvido"}`)
				ctrl.Rejeitar()(ctx)
			case "listar":
				w, ctx = requisicaoJSON(http.MethodGet, "/solicitacoes", "", "")
				ctrl.Listar()(ctx)
			case "minhas":
				w, ctx = requisicaoJSON(http.MethodGet, "/solicitacoes/minhas", "", "")
				ctrl.Minhas()(ctx)
			case "resumo":
				w, ctx = requisicaoJSON(http.MethodGet, "/solicitacoes/resumo", "", "")
				ctrl.Resumo()(ctx)
			}

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
		})
	}
}

// A foto é obrigatória nas duas criações (é a evidência que o Gestor avalia
// antes de aprovar) -- diferente de POST /maquinas, onde ela é opcional.
// Ausente é 400 sem tocar no service nem no R2 (o cheque de FormFile vem
// antes do UploadFoto).
func TestSolicitacaoFotoObrigatoria(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("maquinário sem foto", func(t *testing.T) {
		var recebido model.NovaSolicitacaoMaquinarioPayload
		w, ctx := requisicaoSolicitacao(http.MethodPost, "/solicitacoes/maquinario", dadosMaquinario)
		novoController(solicitacaoFake{maquinario: &recebido}).CriarMaquinario()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
		}
		if recebido.Descricao != "" {
			t.Error("service foi chamado sem foto")
		}
	})

	t.Run("reparo sem foto", func(t *testing.T) {
		var recebido model.NovaSolicitacaoReparoPayload
		w, ctx := requisicaoSolicitacao(http.MethodPost, "/solicitacoes/reparo", dadosReparo)
		novoController(solicitacaoFake{reparo: &recebido}).CriarReparo()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
		}
		if recebido.Descricao != "" {
			t.Error("service foi chamado sem foto")
		}
	})
}

// Content-type errado é 400 antes de qualquer chamada ao R2 -- o front já
// filtra por accept, mas isso é o navegador, não uma fronteira de confiança.
func TestSolicitacaoContentTypeDoArquivo(t *testing.T) {

	gin.SetMode(gin.TestMode)

	fotoTextoPuro := arquivoMultipart{campo: "foto", nome: "foto.txt", contentType: "text/plain", conteudo: []byte("nao e imagem")}

	var recebido model.NovaSolicitacaoMaquinarioPayload
	w, ctx := requisicaoSolicitacao(http.MethodPost, "/solicitacoes/maquinario", dadosMaquinario, fotoTextoPuro)
	novoController(solicitacaoFake{maquinario: &recebido}).CriarMaquinario()(ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
	}
	if recebido.Descricao != "" {
		t.Error("service foi chamado com content-type errado")
	}
}

// Sem InitR2_cloudflare (é o caso destes testes) o upload falha -- mesmo
// comportamento de TestMaquinaFotoSemR2Configurado. A criação não pode ter
// chegado ao service.
func TestSolicitacaoUploadSemR2Configurado(t *testing.T) {

	gin.SetMode(gin.TestMode)

	var recebido model.NovaSolicitacaoMaquinarioPayload
	w, ctx := requisicaoSolicitacao(http.MethodPost, "/solicitacoes/maquinario", dadosMaquinario, fotoValida())
	novoController(solicitacaoFake{maquinario: &recebido}).CriarMaquinario()(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 (corpo: %s)", w.Code, w.Body)
	}
	if recebido.Descricao != "" {
		t.Error("solicitação foi criada mesmo com o upload falhando")
	}
}

// Sem o ValidateStruct do corpoMultipart, campo obrigatório em branco não
// seria barrado -- json.Unmarshal ignora a tag `binding`.
func TestSolicitacaoValidacaoDoPayload(t *testing.T) {

	gin.SetMode(gin.TestMode)

	casos := map[string]string{
		"sem descrição":  `{"maquinaId":9,"impactos":[]}`,
		"sem maquinaId":  `{"descricao":"d","impactos":[]}`,
		"maquinaId zero": `{"maquinaId":0,"descricao":"d","impactos":[]}`,
	}

	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			var recebido model.NovaSolicitacaoMaquinarioPayload
			w, ctx := requisicaoSolicitacao(http.MethodPost, "/solicitacoes/maquinario", corpo, fotoValida())
			novoController(solicitacaoFake{maquinario: &recebido}).CriarMaquinario()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
			}
			if recebido.Descricao != "" {
				t.Errorf("payload inválido chegou no service: %+v", recebido)
			}
		})
	}
}

// abrir-os e rejeitar chegam por corpoJSON -- ShouldBindJSON roda o binding
// de verdade (diferente do multipart), então oneof/required valem aqui.
func TestSolicitacaoValidacaoJSON(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("abrir-os com urgência fora da lista", func(t *testing.T) {
		var recebido model.AberturaOrdemServicoPayload
		w, ctx := requisicaoJSON(http.MethodPost, "/solicitacoes/1/abrir-os", "1", `{"urgencia":"Urgentíssima","tecnicoId":5}`)
		novoController(solicitacaoFake{abrirOS: &recebido}).AbrirOS()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
		}
		if recebido.TecnicoId != 0 {
			t.Error("service foi chamado com payload inválido")
		}
	})

	t.Run("abrir-os sem técnico", func(t *testing.T) {
		w, ctx := requisicaoJSON(http.MethodPost, "/solicitacoes/1/abrir-os", "1", `{"urgencia":"Alta"}`)
		novoController(solicitacaoFake{}).AbrirOS()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
		}
	})

	t.Run("rejeitar sem motivo", func(t *testing.T) {
		w, ctx := requisicaoJSON(http.MethodPost, "/solicitacoes/1/rejeitar", "1", `{}`)
		novoController(solicitacaoFake{}).Rejeitar()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
		}
	})
}

// O escopo é resolvido no WHERE a partir de quem chama, então usuario.id e
// perfil precisam sair do TOKEN -- mesmo critério de
// TestMaquinaListarUsaAtorDoToken. Vindo de outro lugar, o status continua
// 200 e a falha é silenciosa.
func TestSolicitacaoAtorVemDoToken(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("obter", func(t *testing.T) {
		var recebido string
		w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes/1", "1", "")
		ctx.Set(middleware.UserId, int64(42))
		ctx.Set(middleware.UserPerfil, "solicitante")

		novoController(solicitacaoFake{ator: &recebido}).Obter()(ctx)

		if w.Code != http.StatusOK || recebido != "42/solicitante" {
			t.Fatalf("status=%d ator=%q", w.Code, recebido)
		}
	})

	t.Run("listar", func(t *testing.T) {
		var recebido string
		w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes", "", "")
		ctx.Set(middleware.UserId, int64(42))
		ctx.Set(middleware.UserPerfil, "gestor")

		novoController(solicitacaoFake{ator: &recebido}).Listar()(ctx)

		if w.Code != http.StatusOK || recebido != "42/gestor" {
			t.Fatalf("status=%d ator=%q", w.Code, recebido)
		}
	})

	t.Run("minhas", func(t *testing.T) {
		var recebido string
		w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes/minhas", "", "")
		ctx.Set(middleware.UserId, int64(42))

		novoController(solicitacaoFake{ator: &recebido}).Minhas()(ctx)

		if w.Code != http.StatusOK || recebido != "42" {
			t.Fatalf("status=%d ator=%q", w.Code, recebido)
		}
	})

	t.Run("abrir-os", func(t *testing.T) {
		var recebido string
		w, ctx := requisicaoJSON(http.MethodPost, "/solicitacoes/1/abrir-os", "1", `{"urgencia":"Alta","tecnicoId":5}`)
		ctx.Set(middleware.UserId, int64(42))
		ctx.Set(middleware.UserPerfil, "gestor")

		novoController(solicitacaoFake{ator: &recebido}).AbrirOS()(ctx)

		if w.Code != http.StatusCreated || recebido != "42/gestor" {
			t.Fatalf("status=%d ator=%q (corpo: %s)", w.Code, recebido, w.Body)
		}
	})

	t.Run("sem as claims no contexto é 500", func(t *testing.T) {
		var recebido string
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/solicitacoes", nil)
		ctx.Set(middleware.UserTenantId, int64(7))

		novoController(solicitacaoFake{ator: &recebido}).Listar()(ctx)

		if w.Code != http.StatusInternalServerError || recebido != "" {
			t.Fatalf("status=%d ator=%q", w.Code, recebido)
		}
	})
}

// ?lojaId= é o único filtro numérico de GET /solicitacoes -- malformado é 400
// sem tocar o service (mesmo critério de TestMaquinaListarFiltros).
func TestSolicitacaoFiltroLojaId(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("válido chega no service", func(t *testing.T) {
		var loja int64
		w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes?lojaId=3", "", "")
		novoController(solicitacaoFake{filtroLoja: &loja}).Listar()(ctx)

		if w.Code != http.StatusOK || loja != 3 {
			t.Fatalf("status=%d loja=%d", w.Code, loja)
		}
	})

	t.Run("inválido é 400", func(t *testing.T) {
		w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes?lojaId=abc", "", "")
		novoController(solicitacaoFake{}).Listar()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400", w.Code)
		}
	})

	t.Run("status fora da lista é 400", func(t *testing.T) {
		w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes?status=Cancelada", "", "")
		novoController(solicitacaoFake{}).Listar()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400", w.Code)
		}
	})

	t.Run("tipo fora da lista é 400", func(t *testing.T) {
		w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes?tipo=terceiros", "", "")
		novoController(solicitacaoFake{}).Listar()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400", w.Code)
		}
	})
}

// A chave crua do R2 não pode vazar na resposta: sem InitR2_cloudflare a
// assinatura falha, e resolverSolicitacao precisa engolir o erro trocando o
// campo por vazio -- nunca deixar a chave passar adiante. Mesmo critério de
// TestMaquinaFotoSemR2Configurado/"listagem com foto também não vaza a chave".
func TestSolicitacaoChaveNaoVazaNaResposta(t *testing.T) {

	gin.SetMode(gin.TestMode)

	chave := "tenant/7/anexo-secreto.jpg"

	w, ctx := requisicaoJSON(http.MethodGet, "/solicitacoes/1", "1", "")
	novoController(solicitacaoFake{chaveAnexo: chave}).Obter()(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", w.Code)
	}
	if strings.Contains(w.Body.String(), chave) {
		t.Errorf("chave do R2 vazou na resposta: %s", w.Body)
	}
}
