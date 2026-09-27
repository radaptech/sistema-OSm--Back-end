package controller

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

// maquinaFake grava o que recebeu, não só o erro que devolve: payload vindo do
// lugar errado não muda status nenhum. O corpo aqui chega por multipart +
// json.Unmarshal, sem o binding do Gin no caminho, então "o JSON virou struct
// direito" é justamente o que pode quebrar calado.
type maquinaFake struct {
	err     error
	insert  *model.MaquinarioInsert
	editado *model.AtualizarMaquina
	lojaId  **int64
	setorId **int64
	// ator recebido em ListarMaquinario: vindo do lugar errado, o status não muda
	ator *string
	// chave devolvida pelo service em FotoUrl, como MontarListaMaquinarios faz.
	chaveFoto *string
}

func (m maquinaFake) resposta() model.Maquinario {
	return model.Maquinario{
		Id: 1, Nome: "Forno", NumeroPatrimonio: "P-1", Criticidade: "Alta",
		SetorId: 9, SetorNome: "Padaria", LojaId: 2, LojaNome: "Loja Sul",
		FotoUrl: m.chaveFoto, Ativa: true,
	}
}

func (m maquinaFake) CadastrarMaquina(_ context.Context, _ int64, p model.MaquinarioInsert) (model.Maquinario, error) {
	if m.insert != nil {
		*m.insert = p
	}
	if m.err != nil {
		return model.Maquinario{}, m.err
	}
	return m.resposta(), nil
}

func (m maquinaFake) ListarMaquinario(_ context.Context, _, usuarioId int64, perfil string, lojaId, setorId *int64) ([]model.Maquinario, error) {
	if m.ator != nil {
		*m.ator = fmt.Sprintf("%d/%s", usuarioId, perfil)
	}
	if m.lojaId != nil {
		*m.lojaId = lojaId
	}
	if m.setorId != nil {
		*m.setorId = setorId
	}
	if m.err != nil {
		return nil, m.err
	}
	return []model.Maquinario{m.resposta()}, nil
}

func (m maquinaFake) ObterMaquina(_ context.Context, _, _ int64) (model.Maquinario, error) {
	if m.err != nil {
		return model.Maquinario{}, m.err
	}
	return m.resposta(), nil
}

func (m maquinaFake) AtualizarMaquina(_ context.Context, _, _ int64, p model.AtualizarMaquina) (model.Maquinario, error) {
	if m.editado != nil {
		*m.editado = p
	}
	if m.err != nil {
		return model.Maquinario{}, m.err
	}
	return m.resposta(), nil
}

func (m maquinaFake) DesativarMaquina(_ context.Context, _, _ int64) error {
	return m.err
}

const dadosMaquina = `{"nome":"Forno","numeroPatrimonio":"P-1","serie":"SN-9","criticidade":"Alta","setorId":9,` +
	`"preventivas":[{"descricao":"Limpeza","intervaloDias":30,"proximaData":"01/09/2026","ativa":true}]}`

// requisicaoMaquina monta o multipart que o front manda (montarMultipart): o
// JSON na parte `dados` e o arquivo, quando existe, na parte `foto`.
func requisicaoMaquina(metodo, id, query, dados string, comFoto bool) (*httptest.ResponseRecorder, *gin.Context) {

	var corpo bytes.Buffer
	form := multipart.NewWriter(&corpo)

	if dados != "" {
		_ = form.WriteField("dados", dados)
	}
	if comFoto {
		parte, _ := form.CreateFormFile("foto", "foto.png")
		_, _ = parte.Write([]byte("nao-e-png-de-verdade"))
	}
	_ = form.Close()

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(metodo, "/maquinas/"+id+query, &corpo)
	ctx.Request.Header.Set("Content-Type", form.FormDataContentType())
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	ctx.Set(middleware.UserTenantId, int64(7))
	// Ator autenticado por padrão: toda rota daqui roda atrás do AutenticacaoJwt.
	// Quem testa a ausência das claims monta o contexto na mão.
	ctx.Set(middleware.UserId, int64(1))
	ctx.Set(middleware.UserPerfil, "administrador")
	return w, ctx
}

func TestMaquinaMapeiaErroParaStatus(t *testing.T) {

	gin.SetMode(gin.TestMode)

	// O 422 real é "setor de outro tenant ou inexistente", barrado no service.
	setorDeFora := fmt.Errorf("%w: setor %d não existe neste tenant", helper.ErrConflitoIntegridade, 99)

	casos := []struct {
		nome   string
		acao   string
		id     string
		err    error
		status int
	}{
		{"cadastrar sucesso", "cadastrar", "", nil, http.StatusCreated},
		{"cadastrar sem preventiva", "cadastrar", "", fmt.Errorf("%w: a máquina precisa de pelo menos uma manutenção preventiva", helper.ErrValidacao), http.StatusBadRequest},
		{"cadastrar patrimônio duplicado", "cadastrar", "", helper.ErrDadoDuplicado, http.StatusConflict},
		{"cadastrar setor inexistente", "cadastrar", "", setorDeFora, http.StatusUnprocessableEntity},
		{"cadastrar interno", "cadastrar", "", fmt.Errorf("conexão recusada"), http.StatusInternalServerError},

		{"obter sucesso", "obter", "3", nil, http.StatusOK},
		{"obter id malformado", "obter", "abc", nil, http.StatusBadRequest},
		{"obter inexistente", "obter", "999", helper.ErrNaoEncontrado, http.StatusNotFound},

		{"atualizar sucesso", "atualizar", "3", nil, http.StatusOK},
		{"atualizar inexistente", "atualizar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"atualizar setor inexistente", "atualizar", "3", setorDeFora, http.StatusUnprocessableEntity},
		{"atualizar duplicado", "atualizar", "3", helper.ErrDadoDuplicado, http.StatusConflict},

		{"desativar sucesso", "desativar", "3", nil, http.StatusOK},
		{"desativar inexistente", "desativar", "999", helper.ErrNaoEncontrado, http.StatusNotFound},
		{"desativar id malformado", "desativar", "abc", nil, http.StatusBadRequest},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {

			ctrl := NewMaquinaController(maquinaFake{err: c.err}, "bucket-teste")

			var w *httptest.ResponseRecorder
			var ctx *gin.Context

			switch c.acao {
			case "cadastrar":
				w, ctx = requisicaoMaquina(http.MethodPost, "", "", dadosMaquina, false)
				ctrl.Cadastrar()(ctx)
			case "obter":
				w, ctx = requisicaoMaquina(http.MethodGet, c.id, "", "", false)
				ctrl.Obter()(ctx)
			case "atualizar":
				w, ctx = requisicaoMaquina(http.MethodPut, c.id, "", dadosMaquina, false)
				ctrl.Atualizar()(ctx)
			case "desativar":
				w, ctx = requisicaoMaquina(http.MethodDelete, c.id, "", "", false)
				ctrl.Desativar()(ctx)
			}

			if w.Code != c.status {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", w.Code, c.status, w.Body)
			}
		})
	}
}

// O corpo chega por json.Unmarshal, sem o binding do Gin: sem as tags certas os
// campos viram zero sem erro nenhum. `serie` é o caso perigoso -- o nome no
// front não bate com NumeroSerie, e o cadastro passaria sem número de série.
func TestMaquinaCorpoMultipartChegaNoService(t *testing.T) {

	gin.SetMode(gin.TestMode)

	var recebido model.MaquinarioInsert
	w, ctx := requisicaoMaquina(http.MethodPost, "", "", dadosMaquina, false)
	NewMaquinaController(maquinaFake{insert: &recebido}, "bucket-teste").Cadastrar()(ctx)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d (corpo: %s)", w.Code, w.Body)
	}
	if recebido.Nome != "Forno" || recebido.SetorID != 9 || recebido.Criticidade != "Alta" {
		t.Errorf("campos básicos: %+v", recebido)
	}
	if recebido.NumeroSerie == nil || *recebido.NumeroSerie != "SN-9" {
		t.Errorf(`serie não chegou (tag json:"serie" sumiu?): %v`, recebido.NumeroSerie)
	}
	if len(recebido.Preventivas) != 1 || recebido.Preventivas[0].IntervaloDias != 30 {
		t.Fatalf("preventivas: %+v", recebido.Preventivas)
	}
	if recebido.Preventivas[0].ProximaData == nil {
		t.Error("proximaData nil: o config.DataBr não desserializou dd/mm/yyyy")
	}
	// Derivados do servidor não podem vir do cliente (json:"-").
	if recebido.TenantID != 0 || recebido.ID != 0 || recebido.Ativa {
		t.Errorf("campo derivado veio do corpo: %+v", recebido)
	}
}

// json.Unmarshal ignora tag `binding`: sem o ValidateStruct do corpoMultipart
// nada disto é barrado e a regra passa a existir só no navegador.
func TestMaquinaValidacaoDoPayload(t *testing.T) {

	gin.SetMode(gin.TestMode)

	casos := map[string]string{
		"sem preventiva":        `{"nome":"F","numeroPatrimonio":"P","criticidade":"Alta","setorId":9,"preventivas":[]}`,
		"sem nome":              `{"numeroPatrimonio":"P","criticidade":"Alta","setorId":9,"preventivas":[{"descricao":"d","intervaloDias":30,"proximaData":"01/09/2026"}]}`,
		"sem setor":             `{"nome":"F","numeroPatrimonio":"P","criticidade":"Alta","preventivas":[{"descricao":"d","intervaloDias":30,"proximaData":"01/09/2026"}]}`,
		"criticidade fora":      `{"nome":"F","numeroPatrimonio":"P","criticidade":"Altíssima","setorId":9,"preventivas":[{"descricao":"d","intervaloDias":30,"proximaData":"01/09/2026"}]}`,
		"intervalo zero (dive)": `{"nome":"F","numeroPatrimonio":"P","criticidade":"Alta","setorId":9,"preventivas":[{"descricao":"d","intervaloDias":0,"proximaData":"01/09/2026"}]}`,
		"data em ISO":           `{"nome":"F","numeroPatrimonio":"P","criticidade":"Alta","setorId":9,"preventivas":[{"descricao":"d","intervaloDias":30,"proximaData":"2026-09-01"}]}`,
		"dados não é json":      `nao sou json`,
	}

	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {

			var recebido model.MaquinarioInsert
			w, ctx := requisicaoMaquina(http.MethodPost, "", "", corpo, false)
			NewMaquinaController(maquinaFake{insert: &recebido}, "bucket-teste").Cadastrar()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400 (corpo: %s)", w.Code, w.Body)
			}
			// Payload recusado não pode ter chegado ao banco.
			if recebido.Nome != "" || len(recebido.Preventivas) != 0 {
				t.Errorf("payload inválido chegou no service: %+v", recebido)
			}
		})
	}

	t.Run("corpo JSON puro em vez de multipart é 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/maquinas", strings.NewReader(dadosMaquina))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set(middleware.UserTenantId, int64(7))

		NewMaquinaController(maquinaFake{}, "bucket-teste").Cadastrar()(ctx)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, esperado 400", w.Code)
		}
	})
}

// Os dois filtros são combináveis e recortam a listagem do solicitante (as
// máquinas do próprio setor). Chegando nil no service, ele lista o tenant todo.
func TestMaquinaListarFiltros(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("sem filtro chega nil nos dois", func(t *testing.T) {
		var loja, setor *int64
		w, ctx := requisicaoMaquina(http.MethodGet, "", "", "", false)
		NewMaquinaController(maquinaFake{lojaId: &loja, setorId: &setor}, "bucket-teste").ListarMaquinas()(ctx)

		if w.Code != http.StatusOK || loja != nil || setor != nil {
			t.Fatalf("status %d, lojaId %v, setorId %v", w.Code, loja, setor)
		}
	})

	t.Run("os dois filtros chegam juntos", func(t *testing.T) {
		var loja, setor *int64
		w, ctx := requisicaoMaquina(http.MethodGet, "", "?lojaId=2&setorId=9", "", false)
		NewMaquinaController(maquinaFake{lojaId: &loja, setorId: &setor}, "bucket-teste").ListarMaquinas()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if loja == nil || *loja != 2 || setor == nil || *setor != 9 {
			t.Fatalf("lojaId %v, setorId %v (esperado 2 e 9)", loja, setor)
		}
	})

	t.Run("filtro inválido é 400 e não vai ao banco", func(t *testing.T) {
		for _, ruim := range []string{"?lojaId=abc", "?setorId=0", "?setorId=-1"} {
			var loja, setor *int64
			w, ctx := requisicaoMaquina(http.MethodGet, "", ruim, "", false)
			NewMaquinaController(maquinaFake{lojaId: &loja, setorId: &setor}, "bucket-teste").ListarMaquinas()(ctx)

			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, esperado 400", ruim, w.Code)
			}
			if loja != nil || setor != nil {
				t.Errorf("%s chegou no service", ruim)
			}
		}
	})

	t.Run("sem tenant no contexto é 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/maquinas", nil)

		NewMaquinaController(maquinaFake{}, "bucket-teste").ListarMaquinas()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
	})
}

// Sem InitR2_cloudflare (é o caso destes testes, e o de um boot sem as
// variáveis do R2) o upload falha e a assinatura da URL também. As duas pontas
// se comportam diferente de propósito, e é isso que este teste tranca.
func TestMaquinaFotoSemR2Configurado(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("upload que falha responde 500 e não cria máquina", func(t *testing.T) {
		var recebido model.MaquinarioInsert
		w, ctx := requisicaoMaquina(http.MethodPost, "", "", dadosMaquina, true)
		NewMaquinaController(maquinaFake{insert: &recebido}, "bucket-teste").Cadastrar()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500 (corpo: %s)", w.Code, w.Body)
		}
		if recebido.Nome != "" {
			t.Error("a máquina foi gravada mesmo com a foto falhando")
		}
	})

	// Aqui é o contrário: a máquina já está no banco, então engolir a foto é
	// melhor que devolver erro para um cadastro que existe.
	t.Run("assinatura que falha não derruba a resposta", func(t *testing.T) {
		chave := "tenant/7/123.png"
		w, ctx := requisicaoMaquina(http.MethodGet, "3", "", "", false)
		NewMaquinaController(maquinaFake{chaveFoto: &chave}, "bucket-teste").Obter()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, esperado 200", w.Code)
		}
		// A chave crua não pode vazar: ou sai URL assinada, ou não sai nada.
		if strings.Contains(w.Body.String(), chave) {
			t.Errorf("chave do R2 vazou na resposta: %s", w.Body)
		}
	})

	t.Run("listagem com foto também não vaza a chave", func(t *testing.T) {
		chave := "tenant/7/456.png"
		w, ctx := requisicaoMaquina(http.MethodGet, "", "", "", false)
		NewMaquinaController(maquinaFake{chaveFoto: &chave}, "bucket-teste").ListarMaquinas()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, esperado 200", w.Code)
		}
		if strings.Contains(w.Body.String(), chave) {
			t.Errorf("chave do R2 vazou na listagem: %s", w.Body)
		}
	})
}

// O escopo da listagem é resolvido no WHERE a partir de quem chama, então
// usuario.id e perfil precisam sair do TOKEN. Vindo da query ou do corpo, um
// solicitante lista a loja inteira mandando outro id -- e o status continua
// 200, que é o que faz esta falha ser silenciosa.
func TestMaquinaListarUsaAtorDoToken(t *testing.T) {

	gin.SetMode(gin.TestMode)

	t.Run("ator vem do token, não da query", func(t *testing.T) {
		var recebido string
		w, ctx := requisicaoMaquina(http.MethodGet, "", "?usuarioId=999&perfil=administrador", "", false)
		ctx.Set(middleware.UserId, int64(42))
		ctx.Set(middleware.UserPerfil, "solicitante")

		NewMaquinaController(maquinaFake{ator: &recebido}, "bucket-teste").ListarMaquinas()(ctx)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if recebido != "42/solicitante" {
			t.Fatalf("ator = %q, esperado 42/solicitante", recebido)
		}
	})

	t.Run("sem as claims no contexto é 500 e não lista nada", func(t *testing.T) {
		var recebido string
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/maquinas", nil)
		ctx.Set(middleware.UserTenantId, int64(7)) // tenant ok, ator faltando

		NewMaquinaController(maquinaFake{ator: &recebido}, "bucket-teste").ListarMaquinas()(ctx)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, esperado 500", w.Code)
		}
		if recebido != "" {
			t.Errorf("listou sem saber quem é o ator: %q", recebido)
		}
	})
}
