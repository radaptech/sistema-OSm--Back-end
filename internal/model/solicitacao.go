package model

import (
	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
)

// NovaSolicitacaoMaquinarioPayload é o corpo de POST /solicitacoes/maquinario --
// o JSON da parte `dados` do multipart, espelhando NovaSolicitacaoOSPayload
// (front-end/src/tipos/ordemServico.ts). Setor, loja, solicitante e data/hora
// não entram aqui: o servidor deriva da máquina (setor) e da sessão
// (solicitante), mesmo comentário do próprio tipo do front.
//
// Foto é obrigatória e vídeo é opcional; as duas chaves entram como `json:"-"`
// porque quem sobe pro R2 é o controller, ANTES de chamar o service -- mesmo
// padrão de MaquinarioInsert.FotoChave. mime/tamanho saem de graça do header
// do multipart (sem I/O extra) e alimentam solicitacao_anexo.mime_type/
// tamanho_bytes, NOT NULL no schema.
//
// Impactos não leva `binding:"oneof"` de propósito: marcador_impacto tem um
// valor só hoje ("Afeta Produção"), mas o rótulo tem espaço -- a sintaxe de
// `oneof` do validator separa alternativas por espaço, então
// `oneof=Afeta Produção` seria lido como dois valores ("Afeta" e "Produção").
// Quem valida cada item é o service (marcadorValido).
type NovaSolicitacaoMaquinarioPayload struct {
	MaquinaId    int64    `json:"maquinaId" binding:"required,gt=0"`
	Descricao    string   `json:"descricao" binding:"required"`
	Impactos     []string `json:"impactos"`
	FotoChave    string   `json:"-"`
	FotoMime     string   `json:"-"`
	FotoTamanho  int64    `json:"-"`
	VideoChave   *string  `json:"-"`
	VideoMime    *string  `json:"-"`
	VideoTamanho *int64   `json:"-"`
}

// NovaSolicitacaoReparoPayload é o corpo de POST /solicitacoes/reparo --
// espelha NovaSolicitacaoReparoPayload (front-end/src/tipos/reparo.ts). Sem
// Impactos: reparo (lâmpada, vidro, piso) não afeta produção de máquina
// nenhuma -- o front nem oferece o marcador nessa tela.
type NovaSolicitacaoReparoPayload struct {
	Item        string `json:"item" binding:"required"`
	Descricao   string `json:"descricao" binding:"required"`
	FotoChave   string `json:"-"`
	FotoMime    string `json:"-"`
	FotoTamanho int64  `json:"-"`
}

// AberturaOrdemServicoPayload é o corpo de POST /solicitacoes/:id/abrir-os --
// espelha AberturaOrdemServicoPayload do front, menos SolicitacaoId (vem do
// `:id` da rota, não do corpo -- o front já destrói ele pra fora antes de
// montar o POST). Urgencia usa oneof com segurança aqui: 'Baixa'/'Média'/
// 'Alta' não têm espaço interno, diferente do marcador de impacto.
type AberturaOrdemServicoPayload struct {
	Urgencia  string `json:"urgencia" binding:"required,oneof=Baixa Média Alta"`
	TecnicoId int64  `json:"tecnicoId" binding:"required,gt=0"`
}

// RejeicaoSolicitacaoPayload é o corpo de POST /solicitacoes/:id/rejeitar --
// espelha RejeicaoSolicitacaoPayload do front, menos SolicitacaoId (mesmo
// motivo do payload acima).
type RejeicaoSolicitacaoPayload struct {
	Motivo string `json:"motivo" binding:"required"`
}

// AnexoSolicitacao é um item de SolicitacaoOS.Anexos -- espelha
// AnexoSolicitacao do front. Url nasce com a CHAVE do R2 (o que o service
// devolve) e o controller substitui pelo endereço da URL assinada antes de
// responder, mesmo padrão de Maquinario.FotoUrl -- o service nunca resolve
// URL, só lê o banco.
//
// Ponteiro e não string: o front declara `url: string` sem `?` (sempre
// presente), mas se a assinatura falhar no controller não tem como inventar
// uma URL -- string vazia sairia como `""` na resposta, que parece uma URL
// válida até o `<img>`/`<video>` tentar carregar. `null` é honesto sobre o
// que aconteceu; string vazia não. Mesma folga que Maquinario.FotoUrl já usa,
// só que sem `omitempty` (aqui o campo é sempre emitido, `null` incluído --
// como os outros campos `T | null` da struct).
type AnexoSolicitacao struct {
	Id   int64   `json:"id"`
	Tipo string  `json:"tipo"`
	Url  *string `json:"url"`
}

// SolicitacaoOS é o corpo de resposta de /solicitacoes* -- espelha
// SolicitacaoOS do front (front-end/src/tipos/ordemServico.ts), então os
// ponteiros seguem o `T | null` do TypeScript à risca (sem omitempty: o front
// espera a chave presente com `null`, não ausente) e os campos com `?` no
// front levam `omitempty` aqui.
//
// MaquinaFotoUrl nasce com a foto_chave da MÁQUINA (bucket diferente do dos
// anexos da solicitação) -- o controller resolve as duas URLs com buckets
// diferentes antes de responder.
type SolicitacaoOS struct {
	Id               int64              `json:"id"`
	Tipo             string             `json:"tipo"`
	MaquinaId        *int64             `json:"maquinaId"`
	MaquinaNome      *string            `json:"maquinaNome"`
	MaquinaCodigo    *string            `json:"maquinaCodigo"`
	MaquinaFotoUrl   *string            `json:"maquinaFotoUrl,omitempty"`
	ItemDescricao    *string            `json:"itemDescricao"`
	Status           string             `json:"status"`
	Descricao        string             `json:"descricao"`
	SolicitanteId    *int64             `json:"solicitanteId"`
	SolicitanteNome  *string            `json:"solicitanteNome"`
	CriadoEm         *config.DataBr     `json:"criadoEm"`
	SetorId          int64              `json:"setorId"`
	SetorNome        string             `json:"setorNome"`
	LojaId           int64              `json:"lojaId"`
	LojaNome         string             `json:"lojaNome"`
	Impactos         []string           `json:"impactos"`
	Origem           string             `json:"origem"`
	PreventivaId     *int64             `json:"preventivaId,omitempty"`
	Anexos           []AnexoSolicitacao `json:"anexos"`
	MotivoRejeicao   *string            `json:"motivoRejeicao,omitempty"`
	RejeitadoPorNome *string            `json:"rejeitadoPorNome,omitempty"`
}

// MontarSolicitacao é a única tradução de linha de solicitação para resposta
// -- mesmo motivo de MontarPreventiva/MontarListaMaquinarios. ObterSolicitacaoPorID,
// ListarSolicitacoes e ListarSolicitacoesDoSolicitante têm exatamente a mesma
// forma de linha (mesmo SELECT), por isso o service converte por tipo Go para
// repository.ObterSolicitacaoPorIDRow em vez de cada uma ganhar sua própria
// função -- mesmo truque de ListarMaquinasRow em MontarListaMaquinarios.
//
// impactos/anexos entram como as rows CRUAS do repository (não já traduzidas):
// a conversão pra []string/[]AnexoSolicitacao mora aqui, não espalhada pelo
// service, então esta função continua sendo a ÚNICA tradutora. Ficam fora da
// row principal de propósito (ver comentário de ObterSolicitacaoPorID em
// solicitacao_os.sql) -- quem chama busca por query própria (singular pra uma
// solicitação, plural pra uma página inteira sem N+1) e passa aqui.
func MontarSolicitacao(s repository.ObterSolicitacaoPorIDRow, impactos []repository.MarcadorImpacto, anexos []repository.SolicitacaoAnexo) SolicitacaoOS {

	impactosDto := make([]string, 0, len(impactos))
	for _, m := range impactos {
		impactosDto = append(impactosDto, string(m))
	}

	anexosDto := make([]AnexoSolicitacao, 0, len(anexos))
	for _, a := range anexos {
		// Url nasce com a CHAVE (a.Chave), não a URL -- o controller resolve
		// antes de responder, mesmo padrão de Maquinario.FotoUrl.
		anexosDto = append(anexosDto, AnexoSolicitacao{Id: a.ID, Tipo: string(a.Tipo), Url: &a.Chave})
	}

	return SolicitacaoOS{
		Id:               s.ID,
		Tipo:             string(s.Tipo),
		MaquinaId:        s.MaquinaID,
		MaquinaNome:      s.MaquinaNome,
		MaquinaCodigo:    s.MaquinaCodigo,
		MaquinaFotoUrl:   s.MaquinaFotoChave,
		ItemDescricao:    s.ItemDescricao,
		Status:           string(s.Status),
		Descricao:        s.Descricao,
		SolicitanteId:    s.SolicitanteID,
		SolicitanteNome:  s.SolicitanteNome,
		CriadoEm:         config.NewDataBrPtr(s.CriadoEm.Time),
		SetorId:          s.SetorID,
		SetorNome:        s.SetorNome,
		LojaId:           s.LojaID,
		LojaNome:         s.LojaNome,
		Impactos:         impactosDto,
		Origem:           string(s.Origem),
		PreventivaId:     s.PreventivaID,
		Anexos:           anexosDto,
		MotivoRejeicao:   s.MotivoRejeicao,
		RejeitadoPorNome: s.RejeitadoPorNome,
	}
}

// ResumoSolicitacoes é o corpo de resposta de GET /solicitacoes/resumo --
// espelha ResumoSolicitacoes do front. Os três vêm sempre presentes (o
// count(*) FILTER da query nunca devolve NULL), sem omitempty.
type ResumoSolicitacoes struct {
	Abertas     int64 `json:"abertas"`
	EmAndamento int64 `json:"emAndamento"`
	Concluidas  int64 `json:"concluidas"`
}
