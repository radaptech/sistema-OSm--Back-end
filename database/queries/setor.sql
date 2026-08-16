-- name: ObterSetorPorID :one
-- Usado no login/sessão do solicitante: SessaoUsuario.setorNome vem daqui
-- (usuario_escopo guarda só o setor_id).
SELECT id, nome, loja_id FROM setor
WHERE id = $1 AND tenant_id = $2;

-- name: ObterSetoresPorIDs :many
-- Usado no cadastro de usuário: NovoUsuarioPayload manda uma lista plana de
-- setores para N lojas, e setor pertence a uma loja só -- é daqui que sai a
-- distribuição de cada setor no escopo da loja certa.
SELECT id, loja_id FROM setor
WHERE id = ANY(sqlc.arg(ids)::bigint[]) AND tenant_id = sqlc.arg(tenant_id);

-- Setor pertence a uma loja. Exclusão é soft delete (ativo = false) -- ver
-- "Soft delete" em docs/modelagem-banco-dados.md: máquina, solicitação e o
-- escopo de acesso apontam pro setor.
--
-- Não existe lista fixa de setores: quem cadastra é o Administrador, com nome
-- livre e unicidade por loja (uq_setor_loja). Dois "Padaria" em lojas
-- diferentes são registros distintos de propósito -- por isso todo lugar
-- referencia setor_id, nunca o nome (front-end/CLAUDE.md item 6).

-- name: CriarSetor :one
-- tenant_id vai explícito mesmo já estando implícito na loja: a FK composta
-- fk_setor_loja (tenant_id, loja_id) -> loja (tenant_id, id) só fecha com os
-- dois, e é ela que torna impossível pendurar um setor numa loja de outro
-- tenant -- o banco recusa, não depende de disciplina no service.
INSERT INTO setor (tenant_id, loja_id, nome)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ObterSetorCompletoPorID :one
-- Diferente de ObterSetorPorID acima, que projeta só o que a sessão precisa:
-- esta devolve a linha inteira para a tela de edição.
SELECT * FROM setor
WHERE id = $1 AND tenant_id = $2;

-- name: ListarSetores :many
-- loja_id é opcional (NULL não filtra) porque o front usa os dois modos: o
-- select em cascata pede os setores de uma loja (GET /setores?lojaId=) e
-- agruparPorEscopoGestor pede todos de uma vez, para nomear o cabeçalho de um
-- subgrupo vazio -- onde não há item de onde tirar o setorNome.
--
-- Array simples, sem paginação: o front pagina no cliente (item 12).
SELECT * FROM setor
WHERE tenant_id = $1
  AND ativo
  AND (sqlc.narg(loja_id)::bigint IS NULL OR loja_id = sqlc.narg(loja_id))
ORDER BY nome;

-- name: AtualizarSetor :one
-- Sem loja_id: mudar o setor de loja moveria junto as máquinas, o histórico de
-- OS e o escopo de quem tem acesso a ele -- e ninguém pediu isso. Setor na
-- loja errada se resolve desativando e criando na certa.
UPDATE setor
SET nome = $3
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: DesativarSetor :execrows
-- :execrows pelo mesmo motivo de DesativarLoja: distinguir "desativei" de
-- "esse id não existe neste tenant".
UPDATE setor
SET ativo = false
WHERE id = $1 AND tenant_id = $2;

-- name: DesativarSetoresDaLoja :execrows
-- Cascata em cima do soft delete: desativar a loja sem isto deixa setor ativo
-- pendurado em loja inativa, e o escopo de acesso aponta pro setor.
UPDATE setor
SET ativo = false
WHERE tenant_id = $1 AND loja_id = $2;
