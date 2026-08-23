package model

// RespostaPaginada espelha RespostaPaginada<T> do front
// (front-end/src/tipos/paginacao.ts) -- usada nos endpoints paginados no
// servidor (GET /usuarios, GET /solicitacoes/minhas). Os demais devolvem
// array simples e o front pagina no cliente.
type RespostaPaginada[T any] struct {
	Dados        []T   `json:"dados"`
	Pagina       int32 `json:"pagina"`
	TotalPaginas int32 `json:"totalPaginas"`
	Total        int64 `json:"total"`
}
