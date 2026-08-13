-- Exclusão é sempre soft delete (ativo = false) -- ver "Soft delete" em
-- docs/modelagem-banco-dados.md: perde-se o cadastro, não o histórico de OS
-- vinculado ao usuário (solicitante/técnico/gestor).

-- name: CriarUsuario :one
INSERT INTO usuario (tenant_id, perfil, area_tecnico_id, nome, email, senha_hash, telefone)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ObterUsuarioPorID :one
SELECT * FROM usuario
WHERE id = $1 AND tenant_id = $2;

-- name: ObterUsuarioPorEmail :one
-- Usado no login: email é citext (case-insensitive) e único por tenant.
SELECT * FROM usuario
WHERE tenant_id = $1 AND email = $2 AND ativo;

-- name: ListarUsuarios :many
-- Filtros combináveis: perfil e busca (nome/email) são opcionais -- passe
-- NULL para não filtrar por eles. Paginação por LIMIT/OFFSET, contagem total
-- em ContarUsuarios (RespostaPaginada exige os dois).
SELECT * FROM usuario
WHERE tenant_id = $1
  AND ativo
  AND (sqlc.narg(perfil)::perfil_usuario IS NULL OR perfil = sqlc.narg(perfil))
  AND (
    sqlc.narg(busca)::text IS NULL
    OR nome ILIKE '%' || sqlc.narg(busca) || '%'
    OR email ILIKE '%' || sqlc.narg(busca) || '%'
  )
ORDER BY nome
LIMIT $2 OFFSET $3;

-- name: ContarUsuarios :one
SELECT count(*) FROM usuario
WHERE tenant_id = $1
  AND ativo
  AND (sqlc.narg(perfil)::perfil_usuario IS NULL OR perfil = sqlc.narg(perfil))
  AND (
    sqlc.narg(busca)::text IS NULL
    OR nome ILIKE '%' || sqlc.narg(busca) || '%'
    OR email ILIKE '%' || sqlc.narg(busca) || '%'
  );

-- name: AtualizarUsuario :one
-- Sem senha_hash -- troca de senha tem query própria (AtualizarSenhaUsuario),
-- porque na edição ela é opcional (ver CadastrarUsuario: senha omitida mantém
-- o hash atual).
UPDATE usuario
SET perfil = $3,
    area_tecnico_id = $4,
    nome = $5,
    email = $6,
    telefone = $7
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: AtualizarSenhaUsuario :exec
UPDATE usuario
SET senha_hash = $3
WHERE id = $1 AND tenant_id = $2;

-- name: RegistrarUltimoAcesso :exec
UPDATE usuario
SET ultimo_acesso = now()
WHERE id = $1 AND tenant_id = $2;

-- name: DesativarUsuario :exec
UPDATE usuario
SET ativo = false
WHERE id = $1 AND tenant_id = $2;
