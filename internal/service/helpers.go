package service

// Helpers de pacote: o que mais de um service usa e não pertence a nenhuma
// entidade em particular.
//
// O que NÃO mora aqui, de propósito:
//   - os montarX (montarLoja, montarSetor, montarEmpresaTerceirizada, ...) --
//     cada um é a tradução ÚNICA da sua entidade, e o lugar dela é ao lado do
//     service que a usa;
//   - o que é por perfil e não por endpoint (validarEscopo, escopoDoPerfil,
//     setoresPorLoja, gravarEscopo, resolverAreaTecnico, montarUsuario) --
//     EscopoPerfilService.go já é o arquivo compartilhado desse assunto;
//   - gravarPreventivas -- recebe *repository.Queries para gravar dentro da
//     transação que CadastrarMaquina/AtualizarMaquina já abriram, e o porquê
//     disso só se lê junto do resto de preventiva.

import (
	"fmt"
	"strings"

	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
)

// nomeValido apara espaços e recusa o que sobrar vazio. O banco não tem CHECK
// de nome não-vazio, e binding:"required" do Gin passa numa string de espaços
// -- sem isto entra loja com nome em branco, que ninguém consegue selecionar
// depois num select.
func nomeValido(nome string) (string, error) {
	limpo := strings.TrimSpace(nome)
	if limpo == "" {
		return "", fmt.Errorf("%w: nome é obrigatório", helper.ErrValidacao)
	}
	return limpo, nil
}

// campoObrigatorio é o nomeValido genérico: mesma checagem (apara e recusa o
// que sobrar vazio), mas com o nome do campo na mensagem -- nomeValido fala
// sempre "nome", e usar ele para `descricao`/`item` daria um toast errado
// ("nome é obrigatório" para um campo que não é nome nenhum). nomeValido
// continua existindo por si -- é o call site mais comum -- em vez de virar um
// wrapper de uma linha em cima deste.
func campoObrigatorio(campo, valor string) (string, error) {
	limpo := strings.TrimSpace(valor)
	if limpo == "" {
		return "", fmt.Errorf("%w: %s é obrigatório", helper.ErrValidacao, campo)
	}
	return limpo, nil
}

// textoOuNil é o irmão do nomeValido para campo OPCIONAL: apara e devolve nil
// quando não sobra nada, em vez de recusar.
//
// Existe porque o formulário do front nasce com defaultValues vazios e o React
// Hook Form manda a string vazia do input que ninguém tocou -- ou seja, o campo
// opcional chega como "" e não ausente. Sem isto o banco guarda "" numa coluna
// nullable, e a resposta volta com o campo presente e vazio em vez de omitido
// (`omitempty` não pega ponteiro para string vazia).
func textoOuNil(texto *string) *string {

	if texto == nil {
		return nil
	}

	limpo := strings.TrimSpace(*texto)
	if limpo == "" {
		return nil
	}

	return &limpo
}

// escopoDe traduz quem chama no filtro de escopo das listagens.
//
// nil = não filtra, e é sempre o administrador: ele não tem linha em
// usuario_escopo (trg_usuario_escopo_nao_admin recusa), então filtrar por
// escopo devolveria zero máquinas para quem enxerga tudo. Para os outros
// perfis vai o usuario.id do token e a query resolve o resto -- ver o EXISTS
// em ListarMaquinas/ListarPreventivas.
func escopoDe(usuarioId int64, perfil string) *int64 {

	if perfil == "administrador" {
		return nil
	}

	return &usuarioId
}
