-- Escopo de acesso (loja + setor) dos 4 perfis -- ver "3.8 Escopo de acesso
-- unificado" em docs/modelagem-banco-dados.md:
--   Solicitante: 1 escopo, acesso_total_setores = false, exatamente 1 setor.
--   Técnico:     N escopos, acesso_total_setores = true (sem linha de setor).
--   Gestor:      N escopos, cada um acesso_total OU uma lista de setores.
--   Administrador: nenhum escopo -- a ausência É o acesso total ao tenant.
--
-- Editar substitui o conjunto inteiro (mesmo padrão de preventivas em
-- CadastrarMaquina): o service apaga tudo do usuário e recria, numa
-- transação -- não há UPDATE de escopo individual aqui de propósito.

-- name: CriarEscopo :one
INSERT INTO usuario_escopo (usuario_id, loja_id, acesso_total_setores)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CriarEscopoSetor :exec
INSERT INTO usuario_escopo_setor (escopo_id, setor_id)
VALUES ($1, $2);

-- name: ObterEscoposPorUsuario :many
SELECT * FROM usuario_escopo
WHERE usuario_id = $1
ORDER BY loja_id;

-- name: ObterSetoresPorEscopos :many
SELECT * FROM usuario_escopo_setor
WHERE escopo_id = ANY(sqlc.arg(escopo_ids)::bigint[]);

-- name: ObterEscopoSessaoPorUsuario :many
-- Formato que o front consome direto em EscopoAcessoGestor[] (login/sessão):
-- um escopo por loja, com a lista de setor_id (vazia quando acesso_total_setores).
SELECT
    ue.loja_id,
    ue.acesso_total_setores,
    array_remove(array_agg(ues.setor_id), NULL)::bigint[] AS setores_ids
FROM usuario_escopo ue
LEFT JOIN usuario_escopo_setor ues ON ues.escopo_id = ue.id
WHERE ue.usuario_id = $1
GROUP BY ue.id, ue.loja_id, ue.acesso_total_setores
ORDER BY ue.loja_id;

-- name: DeletarSetoresDosEscoposPorUsuario :exec
-- Roda antes de DeletarEscoposPorUsuario -- não há ON DELETE CASCADE entre
-- as duas tabelas.
DELETE FROM usuario_escopo_setor
WHERE escopo_id IN (SELECT id FROM usuario_escopo WHERE usuario_id = $1);

-- name: DeletarEscoposPorUsuario :exec
DELETE FROM usuario_escopo
WHERE usuario_id = $1;

-- name: ObterEscoposSessaoPorUsuarios :many
-- Mesma forma de ObterEscopoSessaoPorUsuario, só que para vários usuários numa
-- ida só ao banco -- é o que ListarUsuarios usa para montar o escopo de uma
-- página inteira sem N+1.
SELECT
    ue.usuario_id,
    ue.loja_id,
    ue.acesso_total_setores,
    array_remove(array_agg(ues.setor_id), NULL)::bigint[] AS setores_ids
FROM usuario_escopo ue
LEFT JOIN usuario_escopo_setor ues ON ues.escopo_id = ue.id
WHERE ue.usuario_id = ANY(sqlc.arg(usuario_ids)::bigint[])
GROUP BY ue.id, ue.usuario_id, ue.loja_id, ue.acesso_total_setores
ORDER BY ue.usuario_id, ue.loja_id;
