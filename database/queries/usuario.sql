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
-- Filtros combináveis: perfil, busca (nome/email) e loja_id são opcionais --
-- passe NULL para não filtrar por eles. Paginação por LIMIT/OFFSET, contagem
-- total em ContarUsuarios (RespostaPaginada exige os dois).
--
-- loja_id filtra pelo escopo de acesso (usuario_escopo), com EXISTS e não
-- JOIN: usuário com N escopos apareceria N vezes e estouraria o LIMIT com
-- repetição. Administrador não tem escopo nenhum (a ausência É o acesso
-- total, 3.8), então some de qualquer listagem filtrada por loja -- que é o
-- certo: ele não pertence a loja alguma. ContarUsuarios repete o mesmo WHERE
-- de propósito; divergir entre as duas dá total que não bate com a página.
SELECT * FROM usuario
WHERE tenant_id = $1
  AND ativo
  AND (sqlc.narg(perfil)::perfil_usuario IS NULL OR perfil = sqlc.narg(perfil))
  AND (
    sqlc.narg(busca)::text IS NULL
    OR nome ILIKE '%' || sqlc.narg(busca) || '%'
    OR email ILIKE '%' || sqlc.narg(busca) || '%'
  )
  AND (
    sqlc.narg(loja_id)::bigint IS NULL
    OR EXISTS (
      SELECT 1 FROM usuario_escopo ue
      WHERE ue.usuario_id = usuario.id AND ue.loja_id = sqlc.narg(loja_id)
    )
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
  )
  AND (
    sqlc.narg(loja_id)::bigint IS NULL
    OR EXISTS (
      SELECT 1 FROM usuario_escopo ue
      WHERE ue.usuario_id = usuario.id AND ue.loja_id = sqlc.narg(loja_id)
    )
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

-- name: DesativarUsuario :execrows
-- :execrows e não :exec -- sem a contagem de linhas, desativar um id que não
-- existe (ou que é de outro tenant) responderia 200 igualzinho a desativar um
-- de verdade. Já desativado conta 1 mesmo assim: o UPDATE casa a linha.
UPDATE usuario
SET ativo = false
WHERE id = $1 AND tenant_id = $2;

-- name: ListarTecnicos :many
-- GET /tecnicos -- projeção somente-leitura sobre `usuario`, não tabela
-- própria: técnico é usuário com perfil 'tecnico'. Existe separada de
-- ListarUsuarios por três motivos, e o primeiro é o que obriga:
--
--  1. RBAC. /usuarios inteiro é do administrador, e quem precisa desta lista é
--     o GESTOR, no select de "Técnico Responsável" do ModalAbrirOrdemServico --
--     ele levaria 403 na outra rota.
--  2. Forma. O tipo Tecnico do front pede `area` como NOME ("Refrigeração") e
--     `lojasIds`; ListarUsuarios não junta area_tecnico e é paginada, enquanto
--     o select quer array simples.
--  3. Escrita continua em /usuarios (mesma tabela) -- duas superfícies de
--     escrita deixariam o mesmo e-mail entrar duas vezes.
--
-- O JOIN com area_tecnico é INNER porque ck_usuario_area_tecnico garante
-- area_tecnico_id NOT NULL exatamente para o perfil 'tecnico'.
--
-- lojas_ids sai de array_agg com FILTER + COALESCE: técnico sem escopo nenhum
-- (não deve existir -- o cadastro exige loja) devolveria {NULL} sem o filtro, e
-- NULL sem o COALESCE. O cast final é o que faz o sqlc gerar []int64 em vez de
-- interface{} -- mesma armadilha do `vencida` em preventiva.sql.
--
-- Os dois filtros são EXISTS e não JOIN, mesmo motivo de ListarUsuarios: com
-- JOIN o técnico apareceria uma vez por escopo que casa.
--   loja_id           -- a loja da solicitação, mandada pelo modal do gestor.
--   escopo_usuario_id -- quem chama (NULL = administrador, não filtra). Sem
--                        isto um gestor lista nome e e-mail dos técnicos de
--                        lojas que ele não enxerga.
--
-- ⚠️ Os dois filtros vivem no MESMO EXISTS, sobre a mesma linha de
-- usuario_escopo, e isso é o ponto: separados, eles pediriam "atende a loja X"
-- E "divide alguma loja comigo" -- que não é a mesma coisa. Um gestor da Loja A
-- pedindo ?lojaId=B receberia o técnico que atende A e B, porque cada condição
-- passava por uma loja diferente. Tem que ser a mesma loja ligando os dois.
-- (Foi assim que a primeira versão saiu; o teste de integração pegou.)
SELECT
    u.id,
    u.nome,
    u.email,
    u.telefone,
    a.nome AS area,
    COALESCE(
        array_agg(ue.loja_id) FILTER (WHERE ue.loja_id IS NOT NULL),
        '{}'
    )::bigint[] AS lojas_ids
FROM usuario u
JOIN area_tecnico a
  ON a.tenant_id = u.tenant_id AND a.id = u.area_tecnico_id
LEFT JOIN usuario_escopo ue
  ON ue.usuario_id = u.id
WHERE u.tenant_id = $1
  AND u.perfil = 'tecnico'
  AND u.ativo
  AND (
    (sqlc.narg(loja_id)::bigint IS NULL AND sqlc.narg(escopo_usuario_id)::bigint IS NULL)
    OR EXISTS (
      SELECT 1
      FROM usuario_escopo t
      WHERE t.usuario_id = u.id
        AND (sqlc.narg(loja_id)::bigint IS NULL OR t.loja_id = sqlc.narg(loja_id))
        AND (
          sqlc.narg(escopo_usuario_id)::bigint IS NULL
          OR EXISTS (
            SELECT 1 FROM usuario_escopo c
            WHERE c.usuario_id = sqlc.narg(escopo_usuario_id)
              AND c.loja_id = t.loja_id
          )
        )
    )
  )
GROUP BY u.id, u.nome, u.email, u.telefone, a.nome
ORDER BY u.nome;

-- name: ObterGestoresDoSetor :many
-- Quem recebe a notificação de WhatsApp quando uma Solicitação nasce naquele
-- setor (ver CLAUDE.md, "Notificação de solicitação por WhatsApp"): todo
-- gestor cujo escopo alcança o setor, mesmo critério de "alcança" do EXISTS
-- em ListarSolicitacoes/ListarMaquinas -- a loja bate E, dentro dela, acesso
-- total OU o setor específico marcado.
--
-- Administrador não entra: não tem linha em usuario_escopo
-- (trg_usuario_escopo_nao_admin recusa) -- a ausência de escopo É o acesso
-- total ao tenant, não dá pra "alcançar um setor" a partir do vazio. A
-- notificação de solicitação é só pro Gestor por ora (ver CLAUDE.md);
-- Técnico entra quando a fase 2 existir, com sua própria query.
--
-- Sem telefone cadastrado, o gestor não aparece -- não é erro, é degrade
-- silencioso: quem chama (NotificacaoService) não teria pra onde mandar de
-- qualquer forma. Ativo também filtra: gestor desativado não deve saber de
-- solicitação nova.
--
-- EXISTS e não JOIN, mesmo motivo de ListarUsuarios/ListarTecnicos: com JOIN
-- o gestor apareceria uma vez por linha de usuario_escopo_setor que casasse
-- (e com acesso_total_setores, a linha de usuario_escopo não tem setor
-- nenhum atrelado -- LEFT JOIN duplicaria por engano se fosse feito fora do
-- EXISTS).
SELECT u.id, u.nome, u.telefone
FROM usuario u
JOIN setor s ON s.tenant_id = u.tenant_id AND s.id = sqlc.arg(setor_id)
WHERE u.tenant_id = sqlc.arg(tenant_id)
  AND u.perfil = 'gestor'
  AND u.ativo
  AND u.telefone IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM usuario_escopo ue
    LEFT JOIN usuario_escopo_setor ues ON ues.escopo_id = ue.id
    WHERE ue.usuario_id = u.id
      AND ue.loja_id = s.loja_id
      AND (ue.acesso_total_setores OR ues.setor_id = sqlc.arg(setor_id))
  )
ORDER BY u.nome;
