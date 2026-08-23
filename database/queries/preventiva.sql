-- Manutenção preventiva de uma máquina. Máquina exige pelo menos uma
-- (regra de negócio do front: esquemaCadastrarMaquina, preventivas min(1)), e
-- elas viajam na mesma requisição da máquina -- CriarPreventiva é chamada
-- tanto por POST /preventivas quanto por CadastrarMaquina, dentro da mesma
-- transação.
--
-- Exclusão é soft delete (ativa = false), e aqui isso não é só convenção da
-- casa: fk_solicitacao_preventiva não tem ON DELETE, então toda preventiva que
-- já disparou uma solicitação automática (origem = 'preventiva') recusaria o
-- DELETE com 23503. Soft delete é o único caminho que não quebra depois do
-- primeiro ciclo vencer.
--
-- ⚠️ `ativa` acumula dois sentidos: o alternador "Preventiva habilitada no
-- sistema" do ModalManutencaoPreventiva e o soft delete do DELETE
-- /preventivas/:id. Desabilitar pelo modal e excluir produzem o mesmo estado --
-- em ambos a preventiva para de vencer e some da listagem. Consistente com o
-- resto do sistema (não existe reativação pela API), mas é uma decisão, não um
-- acidente.
--
-- proxima_data é `date`, não timestamptz: preventiva vence no dia, não na hora
-- (docs/modelagem-banco-dados.md, seção 3).

-- name: CriarPreventiva :one
-- fk_preventiva_maquina é composta (tenant_id, maquina_id): o banco recusa
-- sozinho pendurar preventiva em máquina de outro tenant.
-- ck_intervalo (intervalo_dias > 0) volta 23514 e vira ErrConflitoIntegridade.
INSERT INTO preventiva (tenant_id, maquina_id, descricao, intervalo_dias, proxima_data, ativa)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ObterPreventivaPorID :one
-- Traz os denormalizados porque PreventivaListada (o retorno de POST e PUT
-- /preventivas, não só do GET) exige maquinaNome/setorId/setorNome/lojaId/
-- lojaNome. RETURNING não enxerga tabela juntada, então Criar/Atualizar releem
-- por aqui dentro da mesma transação para responder na forma do contrato.
--
-- Sem filtro de `ativa`: a tela de edição precisa carregar o registro.
SELECT
    p.*,
    m.nome AS maquina_nome,
    m.setor_id,
    s.nome AS setor_nome,
    s.loja_id,
    l.nome AS loja_nome,
    COALESCE(p.ativa AND p.proxima_data <= (now() AT TIME ZONE 'America/Sao_Paulo')::date, false)::boolean AS vencida
FROM preventiva p
JOIN maquina m ON m.tenant_id = p.tenant_id AND m.id = p.maquina_id
JOIN setor   s ON s.tenant_id = m.tenant_id AND s.id = m.setor_id
JOIN loja    l ON l.tenant_id = s.tenant_id AND l.id = s.loja_id
WHERE p.id = $1 AND p.tenant_id = $2;

-- name: ListarPreventivas :many
-- maquina_id é opcional (NULL não filtra): a aba "Manutenção Prev." do painel
-- do gestor lista tudo, e a tela de edição de máquina pede só as dela
-- (GET /preventivas?maquinaId=).
--
-- `vencida` é calculado, nunca coluna -- o front só reage ao flag
-- (front-end/CLAUDE.md, ModalManutencaoPreventiva). A data de corte usa o fuso
-- de São Paulo explícito e não CURRENT_DATE: o container roda em UTC, e com
-- CURRENT_DATE uma preventiva apareceria vencida até 3h antes da virada do dia
-- no Brasil.
--
-- Ordena por proxima_data: o que interessa na fila é o que vence antes, não a
-- ordem alfabética da máquina. Array simples, sem paginação -- o front pagina
-- no cliente.
SELECT
    p.*,
    m.nome AS maquina_nome,
    m.setor_id,
    s.nome AS setor_nome,
    s.loja_id,
    l.nome AS loja_nome,
    COALESCE(p.ativa AND p.proxima_data <= (now() AT TIME ZONE 'America/Sao_Paulo')::date, false)::boolean AS vencida
FROM preventiva p
JOIN maquina m ON m.tenant_id = p.tenant_id AND m.id = p.maquina_id
JOIN setor   s ON s.tenant_id = m.tenant_id AND s.id = m.setor_id
JOIN loja    l ON l.tenant_id = s.tenant_id AND l.id = s.loja_id
WHERE p.tenant_id = $1
  AND p.ativa
  AND (sqlc.narg(maquina_id)::bigint IS NULL OR p.maquina_id = sqlc.narg(maquina_id))
  -- Escopo de acesso no WHERE, nunca no cliente (back-end/CLAUDE.md, "Regras
  -- herdadas do contrato com o front"): NULL não filtra e é o caso do
  -- administrador, que não tem escopo nenhum -- a ausência dele É o acesso
  -- total ao tenant. Para os outros perfis o service manda o usuario.id do
  -- token e a linha só aparece se ele alcança a loja E o setor:
  --   solicitante -> um escopo, um setor;
  --   tecnico     -> escopos com acesso_total_setores (escopoDoPerfil);
  --   gestor      -> setores marcados, ou a loja inteira quando total.
  -- EXISTS e não JOIN, mesmo motivo de ListarUsuarios: com JOIN a máquina
  -- apareceria uma vez por escopo que a alcança.
  AND (
    sqlc.narg(escopo_usuario_id)::bigint IS NULL
    OR EXISTS (
      SELECT 1
      FROM usuario_escopo ue
      LEFT JOIN usuario_escopo_setor ues ON ues.escopo_id = ue.id
      WHERE ue.usuario_id = sqlc.narg(escopo_usuario_id)
        AND ue.loja_id = s.loja_id
        AND (ue.acesso_total_setores OR ues.setor_id = m.setor_id)
    )
  )
ORDER BY p.proxima_data, p.id;

-- name: AtualizarPreventiva :one
-- Sem maquina_id, mesmo motivo de AtualizarSetor não mexer em loja_id: mover a
-- preventiva de máquina deixaria as solicitações que ela já gerou apontando
-- para uma máquina que não é mais a dela. O front manda o campo no PUT; o
-- service ignora.
UPDATE preventiva
SET descricao = $3,
    intervalo_dias = $4,
    proxima_data = $5,
    ativa = $6
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: AvancarProximaData :one
-- Roda quando a preventiva vence e gera a solicitação automática: empurra a
-- próxima data em intervalo_dias a partir da data vencida (não a partir de
-- hoje) -- senão um ciclo processado com atraso arrastaria todos os seguintes.
UPDATE preventiva
SET proxima_data = proxima_data + (intervalo_dias * INTERVAL '1 day')
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: DesativarPreventiva :execrows
-- :execrows e não :exec, mesmo motivo de DesativarLoja/DesativarSetor: sem a
-- contagem de linhas, desativar um id inexistente (ou de outro tenant)
-- responderia sucesso igual a desativar um de verdade.
UPDATE preventiva
SET ativa = false
WHERE id = $1 AND tenant_id = $2;

-- name: DesativarPreventivasDaMaquina :exec
-- PUT /maquinas/:id substitui o conjunto inteiro de preventivas (não faz merge
-- incremental): o service desativa todas as da máquina e insere as novas, na
-- mesma transação -- mesmo padrão do escopo de acesso em AtualizarUsuario.
--
-- Desativa em vez de deletar justamente por causa de fk_solicitacao_preventiva:
-- um DELETE aqui quebraria a edição de qualquer máquina cuja preventiva já
-- tivesse vencido uma vez.
UPDATE preventiva
SET ativa = false
WHERE tenant_id = $1 AND maquina_id = $2;
