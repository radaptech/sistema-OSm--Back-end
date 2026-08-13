-- name: ObterAreaTecnicoPorNome :one
-- Usado no cadastro de técnico: o front manda o nome da área (AreaTecnico em
-- front-end/src/tipos/tecnico.ts), o banco guarda o id em usuario.area_tecnico_id.
SELECT id FROM area_tecnico
WHERE tenant_id = $1 AND nome = $2;
