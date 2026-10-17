-- name: CriarSessao :exec
-- Uma linha por login. token_hash é o SHA-256 do segredo que vai no cookie
-- (ver a migration 000022): o segredo em si nunca chega ao banco.
INSERT INTO sessao (tenant_id, usuario_id, token_hash)
VALUES (sqlc.arg(tenant_id), sqlc.arg(usuario_id), sqlc.arg(token_hash));

-- name: ValidarSessao :one
-- O que o middleware de autenticação roda a cada request. Devolve quem é o
-- dono só se a sessão ainda vale: usada nos últimos 14 dias, criada nos
-- últimos 60 (os dois limites vêm do service, já calculados), usuário ativo e
-- empresa ativa. O perfil sai da linha do usuário, então trocar o perfil de
-- alguém vale no próximo request, não no próximo login.
--
-- Sem filtro de tenant por parâmetro, e não é esquecimento: o tenant
-- autoritativo é o DA SESSÃO (é ele que o middleware põe no contexto), como
-- antes era o do JWT. O segredo de 256 bits é que identifica a linha.
SELECT s.id, s.tenant_id, s.usuario_id, u.perfil, s.ultimo_uso_em
FROM sessao s
JOIN usuario u ON u.tenant_id = s.tenant_id AND u.id = s.usuario_id
JOIN empresa e ON e.id = s.tenant_id
WHERE s.token_hash = sqlc.arg(token_hash)
  AND s.ultimo_uso_em > sqlc.arg(limite_inatividade)
  AND s.criada_em > sqlc.arg(limite_absoluto)
  AND u.ativo
  AND e.ativa;

-- name: TocarSessao :exec
-- Estica a sessão: é o ultimo_uso_em que conta os 14 dias. O service só chama
-- quando o último toque já tem mais de um minuto -- sem isso cada request
-- viraria um UPDATE.
UPDATE sessao SET ultimo_uso_em = now() WHERE id = sqlc.arg(id);

-- name: ApagarSessao :exec
-- Logout: derruba só a sessão deste aparelho.
DELETE FROM sessao WHERE token_hash = sqlc.arg(token_hash);

-- name: ApagarSessoesDoUsuario :exec
-- Todas as sessões de um usuário, em todos os aparelhos: desativação e troca
-- de senha (a de recuperação e a que o Administrador faz na edição).
DELETE FROM sessao WHERE tenant_id = sqlc.arg(tenant_id) AND usuario_id = sqlc.arg(usuario_id);

-- name: ApagarSessoesVencidas :exec
-- Limpeza das que passaram de um dos dois prazos, chamada no login (de
-- qualquer usuário: a tabela é pequena e sem isso ela só cresceria).
DELETE FROM sessao
WHERE ultimo_uso_em <= sqlc.arg(limite_inatividade)
   OR criada_em <= sqlc.arg(limite_absoluto);
