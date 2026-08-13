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
