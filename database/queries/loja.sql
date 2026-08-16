-- Loja é a unidade/filial dentro do tenant. Exclusão é sempre soft delete
-- (ativa = false) -- ver "Soft delete" em docs/modelagem-banco-dados.md:
-- máquina, setor e todo o histórico de OS apontam pra loja, então apagar de
-- verdade levaria o histórico junto.
--
-- ⚠️ A coluna é `ativa` (feminino) na loja e `ativo` no setor/usuário.

-- name: CriarLoja :one
-- Nome é único por tenant (uq_loja_tenant_nome): duplicado volta 23505 e
-- vira ErrDadoDuplicado no helper, sem switch em pgErr.Code no service.
INSERT INTO loja (tenant_id, nome)
VALUES ($1, $2)
RETURNING *;

-- name: ObterLojaPorID :one
-- Sem filtro de `ativa`, igual a ObterUsuarioPorID: a tela de edição precisa
-- ler o registro para preencher o formulário, e quem decide o que fazer com
-- uma loja desativada é o service.
SELECT * FROM loja
WHERE id = $1 AND tenant_id = $2;

-- name: ListarLojas :many
-- Sem paginação nem filtro de busca de propósito: /lojas devolve array simples
-- e o front pagina/filtra no cliente (front-end/CLAUDE.md item 12). Só
-- /usuarios e /solicitacoes/minhas paginam no servidor.
SELECT * FROM loja
WHERE tenant_id = $1
  AND ativa
ORDER BY nome;

-- name: AtualizarLoja :one
UPDATE loja
SET nome = $3
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: DesativarLoja :execrows
-- :execrows e não :exec -- sem a contagem de linhas, desativar um id que não
-- existe (ou que é de outro tenant) responderia igual a desativar um de
-- verdade. Já desativada conta 1: o UPDATE casa a linha do mesmo jeito.
UPDATE loja
SET ativa = false
WHERE id = $1 AND tenant_id = $2;

-- name: ContarSetoresAtivosDaLoja :one
-- Loja não é desativada por baixo dos setores: o escopo de acesso aponta pro
-- setor (usuario_escopo_setor), então setor ativo pendurado em loja inativa
-- continua dando acesso a uma loja que sumiu das listagens. Quem chama decide
-- se recusa ou se desativa os setores junto, mas precisa saber que existem.
SELECT count(*) FROM setor
WHERE tenant_id = $1 AND loja_id = $2 AND ativo;

-- name: ObterLojaParaEscrita :one
-- Igual a ObterLojaPorID, com FOR SHARE: usada dentro da transação que cria
-- setor, para a loja não ser desativada entre o cheque de `ativa` e o INSERT.
-- FOR SHARE e não FOR UPDATE porque ninguém aqui altera a loja -- só precisa
-- impedir que ela mude enquanto o setor entra.
SELECT * FROM loja
WHERE id = $1 AND tenant_id = $2
FOR SHARE;
