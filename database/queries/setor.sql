-- name: ObterSetorPorID :one
-- Usado no login/sessão do solicitante: SessaoUsuario.setorNome vem daqui
-- (usuario_escopo guarda só o setor_id).
SELECT id, nome, loja_id FROM setor
WHERE id = $1 AND tenant_id = $2;
