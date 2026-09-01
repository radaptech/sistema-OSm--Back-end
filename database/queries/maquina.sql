-- Máquina pertence a um setor (maquina só referencia setor_id -- a loja vem
-- por join, docs/modelagem-banco-dados.md). Exclusão é soft delete
-- (ativa = false), mesmo motivo de loja/setor: preventiva e o histórico de OS
-- apontam pra máquina.
--
-- criticidade é o ENUM nivel_criticidade ('Baixa','Média','Alta', migration
-- 000004) e viaja com o mesmo texto que o front manda -- sem resolução
-- nome->id como em area_tecnico_id.
--
-- foto_chave é a KEY do objeto no R2, nunca uma URL: a leitura é assinada na
-- hora (bucketR2.URLLeitura, ver back-end/CLAUDE.md "R2 -- storage de
-- anexos"). Entra em CriarMaquina porque o POST /maquinas sobe a foto na
-- mesma requisição; NULL quando o cliente não mandou nenhuma.
--
-- Em AtualizarMaquina ela entra como COALESCE($11, foto_chave): sem foto nova
-- no multipart o valor chega NULL e a antiga fica. É o que a tela de edição
-- precisa -- o front só sabe *trocar* a foto, não remover (UploadFoto em
-- CadastrarMaquina.tsx), então NULL ali significa "não mexi", não "apague".
-- ⚠️ Trocar a foto deixa o objeto antigo órfão no bucket: ninguém apaga do R2.
-- É lixo barato e sem referência; se incomodar, o caminho é um DELETE do
-- objeto antigo depois do commit (nunca antes -- a transação pode voltar).
--
-- ⚠️ As duas leituras (ObterMaquinaPorID, ListarMaquinas) trazem setor_nome,
-- loja_id e loja_nome por JOIN: o tipo Maquina do front exige setorNome e
-- lojaId, e maquina não guarda loja_id -- a loja só existe via setor. É o
-- padrão "nome vem denormalizado do servidor" de front-end/CLAUDE.md item 6:
-- a tela não monta o nome procurando numa lista à parte.
--
-- Criar/Atualizar não conseguem fazer isso: RETURNING não enxerga tabela
-- juntada. Quem escrever o service resolve relendo por ObterMaquinaPorID
-- dentro da mesma transação -- é o único jeito de a resposta do POST/PUT ter
-- a mesma forma da resposta do GET.

-- name: CriarMaquina :one
-- fk_maquina_setor é composta (tenant_id, setor_id) -> setor (tenant_id, id):
-- o banco já recusa sozinho um setor de outro tenant, sem checagem extra
-- aqui.
INSERT INTO maquina (
    tenant_id, setor_id, criticidade, numero_patrimonio, numero_serie,
    nome, descricao, marca, modelo, foto_chave
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ObterMaquinaPorID :one
-- Sem filtro de `ativa`, igual a ObterLojaPorID/ObterUsuarioPorID: a tela de
-- edição precisa ler o registro para preencher o formulário mesmo desativado.
--
-- Os JOINs são INNER e não LEFT de propósito: setor_id e setor.loja_id são
-- NOT NULL com FK, então não existe máquina sem setor nem setor sem loja --
-- LEFT só faria o Go receber ponteiro em campo que nunca é nulo.
--
-- O escopo é OPCIONAL aqui, diferente de ListarMaquinas: NULL não filtra, e é
-- o que os chamadores de escrita mandam (CadastrarMaquina/AtualizarMaquina
-- releem a linha que acabaram de gravar, o job de preventiva não tem sessão) e
-- também o administrador, que não tem escopo. Quem passa o usuário é
-- GET /indicadores/maquinas/:id, aberto ao Gestor: sem o EXISTS ele leria
-- custo e indisponibilidade de máquina de outra loja enumerando id -- mesma
-- lacuna que ObterSolicitacaoPorID fechou na fase 1.
SELECT m.*, s.nome AS setor_nome, s.loja_id, l.nome AS loja_nome
FROM maquina m
JOIN setor s ON s.tenant_id = m.tenant_id AND s.id = m.setor_id
JOIN loja  l ON l.tenant_id = s.tenant_id AND l.id = s.loja_id
WHERE m.id = sqlc.arg(id) AND m.tenant_id = sqlc.arg(tenant_id)
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
  );

-- name: ListarMaquinas :many
-- setorId e lojaId são opcionais e combináveis (ParametrosListagemMaquinas no
-- front) -- NULL não filtra, mesmo padrão de ListarSetores. O JOIN com setor
-- é o que permite filtrar por loja, já que maquina não guarda loja_id.
--
-- Filtra só por m.ativa, não por s.ativo/l.ativa: máquina em setor desativado
-- continua listada. É o comportamento certo enquanto DesativarSetor não
-- recusar (nem cascatear) com máquina ativa pendurada -- hoje não faz nem um
-- nem outro, diferente de DesativarLoja, que recusa com setor ativo.
--
-- Array simples, sem paginação: o front pagina no cliente.
SELECT m.*, s.nome AS setor_nome, s.loja_id, l.nome AS loja_nome
FROM maquina m
JOIN setor s ON s.tenant_id = m.tenant_id AND s.id = m.setor_id
JOIN loja  l ON l.tenant_id = s.tenant_id AND l.id = s.loja_id
WHERE m.tenant_id = $1
  AND m.ativa
  AND (sqlc.narg(setor_id)::bigint IS NULL OR m.setor_id = sqlc.narg(setor_id))
  AND (sqlc.narg(loja_id)::bigint IS NULL OR s.loja_id = sqlc.narg(loja_id))
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
ORDER BY m.nome;

-- name: AtualizarMaquina :one
-- setor_id entra no SET (diferente de AtualizarSetor, que ignora loja_id):
-- mover uma máquina de setor não arrasta setor/loja de ninguém, só o próprio
-- histórico dela, então mudar é uma operação válida, não um bug.
UPDATE maquina
SET setor_id = $3,
    criticidade = $4,
    numero_patrimonio = $5,
    numero_serie = $6,
    nome = $7,
    descricao = $8,
    marca = $9,
    modelo = $10,
    foto_chave = COALESCE(sqlc.narg('foto_chave'), foto_chave)
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: DesativarMaquina :execrows
-- :execrows e não :exec, mesmo motivo de DesativarLoja/DesativarSetor: sem a
-- contagem de linhas, desativar um id que não existe (ou é de outro tenant)
-- responderia sucesso igual a desativar um de verdade.
UPDATE maquina
SET ativa = false
WHERE id = $1 AND tenant_id = $2;
