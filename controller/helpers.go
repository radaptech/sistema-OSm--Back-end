package controller

// Helpers de pacote: o que mais de um controller precisa fazer antes de chamar
// o service -- ler id da rota, tenant do token, filtro da query e corpo da
// requisição. Todos seguem o mesmo contrato: devolvem (valor, false) DEPOIS de
// já terem escrito a resposta de erro, então o handler só precisa de
//
//	x, ok := helper(ctx)
//	if !ok {
//		return
//	}
//
// e nunca escreve dois corpos na mesma resposta.
//
// O que NÃO mora aqui, de propósito: cookieSessao (loginController.go) carrega
// as regras do cookie de sessão e só faz sentido colado no Login/Logout; e
// resolverFoto/chaveDaFoto (maquinasController.go) são métodos, porque
// dependem do bucket que o controller guarda.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	bucketr2 "github.com/radaptech/sistema-OSm--Back-end/bucketR2"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

// idDaRota lê o :id da URL. Erro aqui é 400 e não 404 de propósito: "/abc" não
// é um id que não existe, é um id malformado -- quem não distingue os dois
// acaba respondendo 404 pra bug de cliente.
func idDaRota(ctx *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || id < 1 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": helper.ErrId.Error()})
		return 0, false
	}
	return id, true
}

// tenantDaRota é o tenant do token -- todas as rotas que chamam isto são
// autenticadas. Ler o header X-tenant-ID aqui deixaria um administrador do
// tenant A escrever no tenant B só trocando o header; o único endpoint que lê
// o header é o login, antes de existir token.
//
// Falta da claim é 500, não 401: o AutenticacaoJwt já devia tê-la garantido,
// então chegar sem ela é erro de wiring da rota, não sessão inválida.
func tenantDaRota(ctx *gin.Context) (int64, bool) {
	tenantId, ok := middleware.GetTenantIDToken(ctx)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de tenant"})
		return 0, false
	}
	return tenantId, true
}

// atorDaRota é quem está fazendo a requisição, lido do token: o usuario.id e o
// perfil. Serve as listagens que filtram por escopo de acesso -- o recorte por
// loja/setor é o WHERE da query do servidor, nunca um filtro do cliente.
//
// Vem do token e não da rota/corpo pelo mesmo motivo do tenantDaRota: aceitar
// do cliente deixaria um solicitante listar a loja inteira mandando outro id.
// Claim ausente é 500, não 401 -- o AutenticacaoJwt já devia tê-la garantido.
func atorDaRota(ctx *gin.Context) (int64, string, bool) {

	usuarioId, okId := middleware.GetUserID(ctx)
	perfil, okPerfil := middleware.GetUserPerfil(ctx)
	if !okId || !okPerfil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno de sessão"})
		return 0, "", false
	}

	return usuarioId, perfil, true
}

// idDeQuery lê um filtro numérico opcional da query string (?lojaId=,
// ?setorId=, ?maquinaId=).
//
// Ausente ou vazio é nil sem erro -- montarQuery no front já descarta vazio, e
// "não filtrar" é um modo legítimo de todas essas listagens. Não numérico ou
// < 1 é 400, mesmo critério do idDaRota.
func idDeQuery(ctx *gin.Context, nome string) (*int64, bool) {

	bruto := ctx.Query(nome)
	if bruto == "" {
		return nil, true
	}

	n, err := strconv.ParseInt(bruto, 10, 64)
	if err != nil || n < 1 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": nome + " inválido"})
		return nil, false
	}

	return &n, true
}

// corpoJSON lê e valida o corpo de POST/PUT nas rotas sem arquivo (loja,
// setor, preventiva). Extra no JSON é ignorado pelo binding -- inclusive o
// empresaId que o front mandava em /lojas e que não tem coluna onde cair (ver
// model.Loja).
//
// Genérica porque a função é a mesma para todo payload: o que muda é só o tipo
// de destino, e uma cópia por domínio (corpoLoja, corpoSetor, ...) só
// multiplicava a mesma mensagem de erro. As notas de cada payload vivem na
// struct dele, em internal/model.
func corpoJSON[T any](ctx *gin.Context) (T, bool) {

	var input T
	if err := ctx.ShouldBindJSON(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":    "dados invalidos",
			"detalhes": err.Error(),
		})
		return input, false
	}

	return input, true
}

// corpoMultipart lê e valida o corpo das rotas COM arquivo (POST/PUT /maquinas
// hoje, as três criações de solicitação depois): o JSON vem na parte `dados` e
// os arquivos nas partes `foto`/`video` -- é o formato que montarMultipart
// monta no front. Por isso não há ShouldBindJSON aqui.
//
// Os arquivos em si não são lidos aqui: quem sabe se são obrigatórios, pra
// qual bucket vão e o que fazer quando o resto falha é o handler do domínio
// (ver chaveDaFoto em maquinasController.go).
func corpoMultipart[T any](ctx *gin.Context) (T, bool) {

	var input T

	// MaxBytesReader corta a leitura do corpo; ParseMultipartForm sozinho só
	// limita o que fica em memória, e o resto iria pra disco do container.
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, bucketr2.TamanhoMaximoFoto)
	if err := ctx.Request.ParseMultipartForm(bucketr2.TamanhoMaximoFoto); err != nil {

		// Estourar o limite é 413 e não 400: o corpo não está malformado, é
		// grande demais -- e é a diferença entre o toast dizer "foto muito
		// grande" ou "dados invalidos" pra quem fotografou pelo celular.
		var excedeu *http.MaxBytesError
		if errors.As(err, &excedeu) {
			ctx.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "arquivo maior que o limite de 10MB",
			})
			return input, false
		}

		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":    "erro ao ler o formulario enviado",
			"detalhes": err.Error(),
		})
		return input, false
	}

	if err := json.Unmarshal([]byte(ctx.Request.FormValue("dados")), &input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":    "dados invalidos",
			"detalhes": err.Error(),
		})
		return input, false
	}

	// json.Unmarshal não roda as tags `binding` -- o validator só entra pelo
	// caminho de bind do Gin. Sem esta linha, `required`/`oneof`/`min=1`
	// (inclusive os aninhados, pelo `dive`) seriam decoração.
	if err := binding.Validator.ValidateStruct(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":    "dados invalidos",
			"detalhes": err.Error(),
		})
		return input, false
	}

	return input, true
}
