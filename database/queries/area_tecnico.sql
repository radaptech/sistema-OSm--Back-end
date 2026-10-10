-- name: ObterAreaTecnicoPorNome :one
-- Usado no cadastro de técnico: o front manda o nome da área (AreaTecnico em
-- front-end/src/tipos/tecnico.ts), o banco guarda o id em usuario.area_tecnico_id.
SELECT id FROM area_tecnico
WHERE tenant_id = $1 AND nome = $2;

-- name: ListarAreasTecnico :many
-- O caminho inverso de ObterAreaTecnicoPorNome: id -> nome, para devolver
-- `area` em GET /usuarios(/:id) -- a tela de edição precisa dela para não
-- abrir o técnico com a área em branco. Lista inteira e não um :one por
-- usuário: são as poucas áreas do tenant, e a listagem paginada de usuários
-- faria uma ida ao banco por técnico.
SELECT id, nome FROM area_tecnico
WHERE tenant_id = $1;
