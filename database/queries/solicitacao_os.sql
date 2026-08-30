-- Solicitação de OS -- a fila que o Gestor avalia antes de existir Ordem de
-- Serviço. Nasce de duas origens (ck_origem amarra as duas colunas):
--   'solicitante' -> alguém abriu pelo app, tem solicitante_id e foto;
--   'preventiva'  -> venceu uma data no calendário, tem preventiva_id e
--                    ninguém por trás (solicitante_id NULL, sem anexo).
--
-- Este arquivo começa pelo caminho automático porque é o que o job de
-- preventiva vencida precisa. As leituras da fase 1 (GET /solicitacoes,
-- /minhas, /:id, /resumo) e as duas criações humanas (POST
-- /solicitacoes/maquinario e /reparo) entram aqui depois.

-- name: CriarSolicitacaoPreventiva :one
-- Insere a solicitação automática de uma preventiva vencida. Chamada só pelo
-- job (subcomando de CLI `preventivas-vencidas`), na mesma transação do
-- AvancarProximaData da preventiva de origem -- separadas, avançar a data com
-- o INSERT falhando pularia o ciclo em silêncio.
--
-- `tipo` e `origem` são literais no SQL, não parâmetros, porque as constraints
-- não deixam variar:
--   ck_solicitacao_alvo -> 'maquinario' exige maquina_id e PROÍBE
--                          item_descricao (o oposto de 'reparo'). Preventiva é
--                          sempre de uma máquina cadastrada;
--   ck_origem           -> (origem = 'preventiva') = (preventiva_id IS NOT NULL)
--                          e (origem = 'solicitante') = (solicitante_id IS NOT
--                          NULL). Ou seja: solicitante_id não é "opcional" aqui,
--                          é proibido -- por isso nem aparece na lista de
--                          colunas.
--
-- `status` fica no DEFAULT 'Pendente': a OS só nasce quando o Gestor aprova com
-- técnico e urgência (POST /solicitacoes/:id/abrir-os). Criar OS direto pularia
-- a aprovação, que é o ponto inteiro do fluxo.
--
-- `setor_id` é NOT NULL e vem da máquina (a solicitação não guarda loja -- ela
-- sai via setor). Quem lê é ListarPreventivasVencidas, que já projeta
-- m.setor_id justamente para cá.
--
-- Sem foto e sem ON CONFLICT, os dois de propósito:
--   - trg_solicitacao_tem_foto exige anexo só para origem = 'solicitante'
--     desde a migration 000005 -- antes dela este INSERT falhava no COMMIT;
--   - uq_preventiva_pendente (índice único parcial em preventiva_id WHERE
--     status = 'Pendente') é a rede contra execução duplicada do cron, e é ele
--     -- não o NOT EXISTS da query que alimenta este INSERT -- quem garante a
--     regra quando duas réplicas rodam juntas. Deixar o 23505 subir e o service
--     tratar como benigno é mais simples que um ON CONFLICT com predicado
--     parcial, que devolveria zero linhas e faria o :one virar pgx.ErrNoRows --
--     um segundo caso de erro para o mesmo evento.
-- Os casts ::bigint em maquina_id e preventiva_id não são decoração: as duas
-- colunas são nullable no schema (precisam ser, para 'reparo' e para a origem
-- humana), e sem o cast o sqlc gera *int64 nos parâmetros. Aqui elas nunca são
-- nulas -- ck_solicitacao_alvo e ck_origem proíbem -- e ponteiro num job que
-- roda sozinho é só uma forma a mais de gravar NULL por engano.
INSERT INTO solicitacao_os (tenant_id, tipo, maquina_id, setor_id, preventiva_id, origem, descricao)
VALUES (
    sqlc.arg(tenant_id),
    'maquinario',
    sqlc.arg(maquina_id)::bigint,
    sqlc.arg(setor_id),
    sqlc.arg(preventiva_id)::bigint,
    'preventiva',
    sqlc.arg(descricao)
)
RETURNING id;

-- name: CriarSolicitacaoMaquinario :one
-- POST /solicitacoes/maquinario. `tipo`/`origem` literais, mesmo motivo de
-- CriarSolicitacaoPreventiva: ck_solicitacao_alvo exige maquina_id e proíbe
-- item_descricao para 'maquinario', ck_origem exige solicitante_id para
-- 'solicitante'. setor_id vem da máquina escolhida (o service resolve via
-- ObterMaquinaPorID antes de chamar isto -- solicitação não guarda loja).
--
-- RETURNING id, criado_em (não `*`): é só o que o service precisa para montar
-- a resposta via ObterSolicitacaoPorID logo em seguida, na mesma transação --
-- mesmo padrão de CadastrarMaquina relendo por Obter...PorID porque RETURNING
-- não enxerga tabela juntada (aqui nem precisaria do JOIN, mas mantém as duas
-- criações e a leitura com uma origem só de verdade).
INSERT INTO solicitacao_os (tenant_id, tipo, maquina_id, setor_id, solicitante_id, origem, descricao)
VALUES (
    sqlc.arg(tenant_id),
    'maquinario',
    sqlc.arg(maquina_id),
    sqlc.arg(setor_id),
    sqlc.arg(solicitante_id),
    'solicitante',
    sqlc.arg(descricao)
)
RETURNING id, criado_em;

-- name: CriarSolicitacaoReparo :one
-- POST /solicitacoes/reparo. Espelho da anterior para o outro lado de
-- ck_solicitacao_alvo: maquina_id fica NULL (nem entra na lista de colunas) e
-- item_descricao é obrigatório. setor_id aqui vem do escopo do solicitante
-- (ObterEscopoSessaoPorUsuario -- reparo não tem máquina para derivar loja),
-- não de uma query nova.
INSERT INTO solicitacao_os (tenant_id, tipo, item_descricao, setor_id, solicitante_id, origem, descricao)
VALUES (
    sqlc.arg(tenant_id),
    'reparo',
    sqlc.arg(item_descricao),
    sqlc.arg(setor_id),
    sqlc.arg(solicitante_id),
    'solicitante',
    sqlc.arg(descricao)
)
RETURNING id, criado_em;

-- name: CriarImpactoSolicitacao :exec
-- Um INSERT por marcador (hoje só existe 'Afeta Produção' -- marcador_impacto
-- é ENUM de um valor só, associativa porque o contrato já troca uma lista).
-- Chamada em loop pelo service, na mesma transação da solicitação: sem
-- solicitação persistida ainda não há id para pendurar o impacto.
INSERT INTO solicitacao_impacto (solicitacao_id, marcador) VALUES (sqlc.arg(solicitacao_id), sqlc.arg(marcador));

-- name: CriarAnexoSolicitacao :exec
-- `chave` é a KEY do objeto no R2 (bucketR2.UploadFoto sobe o arquivo ANTES
-- de abrir a transação -- precisa da key para este INSERT -- e devolve só a
-- key, nunca URL: a leitura é sempre assinada na hora, ver
-- back-end/CLAUDE.md "R2 -- storage de anexos"). mime_type e tamanho_bytes
-- saem de graça do header do multipart, sem custo de I/O extra.
INSERT INTO solicitacao_anexo (solicitacao_id, tipo, chave, mime_type, tamanho_bytes)
VALUES (sqlc.arg(solicitacao_id), sqlc.arg(tipo), sqlc.arg(chave), sqlc.arg(mime_type), sqlc.arg(tamanho_bytes));

-- name: ObterSolicitacaoPorID :one
-- Traz os denormalizados que SolicitacaoOS do front exige: maquinaNome/
-- maquinaCodigo/maquinaFotoUrl (reparo não tem máquina -- os três vêm NULL
-- via LEFT JOIN), setorNome/lojaId/lojaNome (sempre presentes -- toda
-- solicitação tem setor), solicitanteNome (NULL quando origem = 'preventiva')
-- e rejeitadoPorNome (NULL enquanto não rejeitada).
--
-- Impactos e anexos ficam de fora de propósito, em vez de um array_agg aqui
-- dentro: mesmo padrão de usuário/escopo (ObterUsuarioPorID não traz o
-- escopo -- quem busca é uma query própria). Evita GROUP BY sobre `s.*` e
-- mantém uma única forma de buscar cada filho, reaproveitada tanto para uma
-- solicitação (aqui) quanto para uma página inteira delas (as `...Das...`
-- plurais abaixo).
--
-- JOINs de setor/loja são INNER (NOT NULL, FK garante); máquina, solicitante
-- e rejeitado_por são LEFT (todos nullable no schema).
--
-- escopo_usuario_id é opcional, mesmo `escopoDe(usuarioId, perfil)` de
-- ListarSolicitacoes -- e todo chamador manda ele, sempre: GET
-- /solicitacoes/:id (aberto a qualquer perfil) para não deixar um
-- Solicitante enumerar id e ler foto/descrição de outro setor, e
-- AbrirOS/Rejeitar (só gestor/administrador na rota) para o Gestor não agir
-- em solicitação de loja que ele não atende. NULL só quando `escopoDe`
-- devolve NULL, ou seja, para administrador -- ele não tem escopo, a
-- ausência É o acesso total. Fora do escopo cai em pgx.ErrNoRows como se o
-- id não existisse -- 404, nunca 403, mesmo critério de "filtro do cliente
-- estreita, nunca amplia".
SELECT
    s.*,
    m.nome AS maquina_nome,
    m.numero_patrimonio AS maquina_codigo,
    m.foto_chave AS maquina_foto_chave,
    sc.nome AS setor_nome,
    sc.loja_id,
    l.nome AS loja_nome,
    sol.nome AS solicitante_nome,
    rej.nome AS rejeitado_por_nome
FROM solicitacao_os s
JOIN setor sc ON sc.tenant_id = s.tenant_id AND sc.id = s.setor_id
JOIN loja  l  ON l.tenant_id = sc.tenant_id AND l.id = sc.loja_id
LEFT JOIN maquina m   ON m.tenant_id = s.tenant_id AND m.id = s.maquina_id
LEFT JOIN usuario sol ON sol.tenant_id = s.tenant_id AND sol.id = s.solicitante_id
LEFT JOIN usuario rej ON rej.tenant_id = s.tenant_id AND rej.id = s.rejeitado_por_id
WHERE s.id = sqlc.arg(id) AND s.tenant_id = sqlc.arg(tenant_id)
  AND (
    sqlc.narg(escopo_usuario_id)::bigint IS NULL
    OR EXISTS (
      SELECT 1
      FROM usuario_escopo ue
      LEFT JOIN usuario_escopo_setor ues ON ues.escopo_id = ue.id
      WHERE ue.usuario_id = sqlc.narg(escopo_usuario_id)
        AND ue.loja_id = sc.loja_id
        AND (ue.acesso_total_setores OR ues.setor_id = s.setor_id)
    )
  );

-- name: ListarSolicitacoes :many
-- GET /solicitacoes -- a fila do Gestor. Array simples, sem paginação (o
-- front pagina no cliente, mesmo padrão de ListarMaquinas/ListarPreventivas).
--
-- status/tipo/lojaId/busca são combináveis e opcionais -- NULL não filtra.
-- Busca cobre descrição, nome da máquina e item do reparo (os dois campos que
-- a lista mostra como "alvo").
--
-- Escopo no WHERE via EXISTS sobre usuario_escopo, mesmo bloco de
-- ListarMaquinas/ListarPreventivas: NULL é o administrador (não filtra), os
-- demais perfis só enxergam solicitação cujo setor eles alcançam.
SELECT
    s.*,
    m.nome AS maquina_nome,
    m.numero_patrimonio AS maquina_codigo,
    m.foto_chave AS maquina_foto_chave,
    sc.nome AS setor_nome,
    sc.loja_id,
    l.nome AS loja_nome,
    sol.nome AS solicitante_nome,
    rej.nome AS rejeitado_por_nome
FROM solicitacao_os s
JOIN setor sc ON sc.tenant_id = s.tenant_id AND sc.id = s.setor_id
JOIN loja  l  ON l.tenant_id = sc.tenant_id AND l.id = sc.loja_id
LEFT JOIN maquina m   ON m.tenant_id = s.tenant_id AND m.id = s.maquina_id
LEFT JOIN usuario sol ON sol.tenant_id = s.tenant_id AND sol.id = s.solicitante_id
LEFT JOIN usuario rej ON rej.tenant_id = s.tenant_id AND rej.id = s.rejeitado_por_id
WHERE s.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(status)::status_solicitacao IS NULL OR s.status = sqlc.narg(status))
  AND (sqlc.narg(tipo)::tipo_solicitacao IS NULL OR s.tipo = sqlc.narg(tipo))
  AND (sqlc.narg(loja_id)::bigint IS NULL OR sc.loja_id = sqlc.narg(loja_id))
  AND (
    sqlc.narg(busca)::text IS NULL
    OR s.descricao ILIKE '%' || sqlc.narg(busca) || '%'
    OR m.nome ILIKE '%' || sqlc.narg(busca) || '%'
    OR s.item_descricao ILIKE '%' || sqlc.narg(busca) || '%'
  )
  AND (
    sqlc.narg(escopo_usuario_id)::bigint IS NULL
    OR EXISTS (
      SELECT 1
      FROM usuario_escopo ue
      LEFT JOIN usuario_escopo_setor ues ON ues.escopo_id = ue.id
      WHERE ue.usuario_id = sqlc.narg(escopo_usuario_id)
        AND ue.loja_id = sc.loja_id
        AND (ue.acesso_total_setores OR ues.setor_id = s.setor_id)
    )
  )
ORDER BY s.criado_em DESC;

-- name: ListarSolicitacoesDoSolicitante :many
-- GET /solicitacoes/minhas -- restrita ao próprio solicitante (nunca escopo:
-- é a lista pessoal, não a fila do gestor), paginada por LIMIT/OFFSET porque
-- model.RespostaPaginada exige os dois números (ContarSolicitacoesDoSolicitante
-- abaixo, mesmo WHERE, mesmo motivo de ContarUsuarios repetir o de
-- ListarUsuarios -- divergir dá total que não bate com a página).
SELECT
    s.*,
    m.nome AS maquina_nome,
    m.numero_patrimonio AS maquina_codigo,
    m.foto_chave AS maquina_foto_chave,
    sc.nome AS setor_nome,
    sc.loja_id,
    l.nome AS loja_nome,
    sol.nome AS solicitante_nome,
    rej.nome AS rejeitado_por_nome
FROM solicitacao_os s
JOIN setor sc ON sc.tenant_id = s.tenant_id AND sc.id = s.setor_id
JOIN loja  l  ON l.tenant_id = sc.tenant_id AND l.id = sc.loja_id
LEFT JOIN maquina m   ON m.tenant_id = s.tenant_id AND m.id = s.maquina_id
LEFT JOIN usuario sol ON sol.tenant_id = s.tenant_id AND sol.id = s.solicitante_id
LEFT JOIN usuario rej ON rej.tenant_id = s.tenant_id AND rej.id = s.rejeitado_por_id
WHERE s.tenant_id = sqlc.arg(tenant_id)
  AND s.solicitante_id = sqlc.arg(solicitante_id)
  AND (sqlc.narg(status)::status_solicitacao IS NULL OR s.status = sqlc.narg(status))
  AND (
    sqlc.narg(busca)::text IS NULL
    OR s.descricao ILIKE '%' || sqlc.narg(busca) || '%'
    OR m.nome ILIKE '%' || sqlc.narg(busca) || '%'
    OR s.item_descricao ILIKE '%' || sqlc.narg(busca) || '%'
  )
ORDER BY s.criado_em DESC
LIMIT sqlc.arg(limite) OFFSET sqlc.arg(deslocamento);

-- name: ContarSolicitacoesDoSolicitante :one
SELECT count(*)
FROM solicitacao_os s
LEFT JOIN maquina m ON m.tenant_id = s.tenant_id AND m.id = s.maquina_id
WHERE s.tenant_id = sqlc.arg(tenant_id)
  AND s.solicitante_id = sqlc.arg(solicitante_id)
  AND (sqlc.narg(status)::status_solicitacao IS NULL OR s.status = sqlc.narg(status))
  AND (
    sqlc.narg(busca)::text IS NULL
    OR s.descricao ILIKE '%' || sqlc.narg(busca) || '%'
    OR m.nome ILIKE '%' || sqlc.narg(busca) || '%'
    OR s.item_descricao ILIKE '%' || sqlc.narg(busca) || '%'
  );

-- name: ObterImpactosDaSolicitacao :many
-- Filhos de uma solicitação só, usada por ObterSolicitacaoPorID (GET /:id, e
-- pela criação, que relê por ali dentro da mesma transação).
SELECT marcador FROM solicitacao_impacto WHERE solicitacao_id = sqlc.arg(solicitacao_id);

-- name: ObterImpactosDasSolicitacoes :many
-- Mesma forma da anterior, para uma página inteira numa ida só ao banco --
-- mesmo padrão de ObterEscoposSessaoPorUsuarios (usuario_escopo.sql): quem
-- lista usa a plural para não fazer N+1, uma query por solicitação da página.
SELECT solicitacao_id, marcador
FROM solicitacao_impacto
WHERE solicitacao_id = ANY(sqlc.arg(solicitacao_ids)::bigint[])
ORDER BY solicitacao_id;

-- name: ObterAnexosDaSolicitacao :many
SELECT * FROM solicitacao_anexo WHERE solicitacao_id = sqlc.arg(solicitacao_id) ORDER BY id;

-- name: ObterAnexosDasSolicitacoes :many
-- Plural de propósito, mesmo motivo de ObterImpactosDasSolicitacoes: quem
-- monta uma página de solicitações busca os anexos de todas numa ida só.
SELECT * FROM solicitacao_anexo
WHERE solicitacao_id = ANY(sqlc.arg(solicitacao_ids)::bigint[])
ORDER BY solicitacao_id, id;

-- name: ObterResumoSolicitacoes :one
-- GET /solicitacoes/resumo -- os três contadores da Home do Solicitante.
-- `abertas`/`emAndamento`/`concluidas` não são estados de solicitacao_os
-- (que só tem Pendente/Convertida/Rejeitada): são o status da OrdemServico
-- que ela virou, por isso o LEFT JOIN com ordem_servico. Uma solicitação
-- Pendente conta como "aberta" (ainda não foi nem avaliada); Rejeitada não
-- entra em nenhum balde, de propósito -- não é trabalho aberto nem
-- concluído, e a tela nem mostra essa categoria.
--
-- count(*) FILTER (WHERE ...) e não três subqueries: uma passada só pela
-- tabela, sem repetir o FROM/WHERE três vezes.
SELECT
    count(*) FILTER (WHERE s.status = 'Pendente' OR o.status = 'Aberta')::bigint AS abertas,
    count(*) FILTER (WHERE o.status IN ('Em Andamento', 'Pausada'))::bigint AS em_andamento,
    count(*) FILTER (WHERE o.status = 'Concluída')::bigint AS concluidas
FROM solicitacao_os s
LEFT JOIN ordem_servico o ON o.solicitacao_id = s.id
WHERE s.tenant_id = sqlc.arg(tenant_id) AND s.solicitante_id = sqlc.arg(solicitante_id);

-- name: CriarOrdemServicoDeSolicitacao :one
-- Nasce da aprovação do Gestor (POST /:id/abrir-os): só o INSERT mínimo que
-- faz a linha existir, ck_os_executor validando o resto (tecnico_id e
-- urgencia NOT NULL, e como tipo aqui nunca é 'terceiros' -- solicitacao_os
-- só produz 'maquinario'/'reparo' -- empresa_terceirizada_id e
-- terceiro_acionado_em ficam NULL, satisfazendo a metade "terceiros" do
-- CHECK por vacuidade). O ciclo de vida (iniciar/pausar/encerrar) é fase 2,
-- fora daqui.
--
-- `urgencia` é o ENUM nivel_urgencia (migration 000007) -- sem
-- ObterUrgenciaPorNome/resolução nome->id: o front já manda o rótulo exato
-- ('Baixa'/'Média'/'Alta', front-end/src/tipos/ordemServico.ts) e o valor
-- viaja direto, mesmo padrão de `tipo`/`origem` em CriarSolicitacaoPreventiva.
--
-- `tipo` é tipo_os, não tipo_solicitacao -- tipos Postgres diferentes com os
-- mesmos rótulos textuais; o service converte (solicitacao.Tipo vira o
-- parâmetro aqui), não este arquivo.
--
-- `afeta_producao` vem do service, que já leu solicitacao_impacto: é aqui,
-- não em solicitacao_os, que o relógio de máquina parada (docs/modelagem,
-- "horas_parada só existe se afeta_producao") vai procurar o flag.
--
-- RETURNING id, aberta_em (não só id): `aberta_em` é o `dataAbertura` da
-- resposta de POST /:id/abrir-os (OrdemServico do front) -- sai do DEFAULT
-- now() sem precisar reler a linha inteira, mesmo espírito de
-- CriarSolicitacaoMaquinario devolvendo `criado_em` junto do `id`.
INSERT INTO ordem_servico (tenant_id, solicitacao_id, tipo, tecnico_id, urgencia, aberta_por_id, afeta_producao)
VALUES (
    sqlc.arg(tenant_id),
    sqlc.arg(solicitacao_id),
    sqlc.arg(tipo),
    sqlc.arg(tecnico_id),
    sqlc.arg(urgencia),
    sqlc.arg(aberta_por_id),
    sqlc.arg(afeta_producao)
)
RETURNING id, aberta_em;

-- name: MarcarSolicitacaoConvertida :execrows
-- `AND status = 'Pendente'` no WHERE, não só no id: sem isso, abrir OS duas
-- vezes seguidas passaria a segunda também (uq_os_solicitacao pegaria no
-- INSERT de ordem_servico, mas tarde -- é melhor a solicitação já recusar
-- antes de gastar o INSERT). 0 linhas afetadas é como o service sabe que não
-- era Pendente e devolve ErrConflitoIntegridade, sem tentar o INSERT da OS.
UPDATE solicitacao_os
SET status = 'Convertida'
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND status = 'Pendente';

-- name: RejeitarSolicitacao :execrows
-- ck_rejeicao exige os três juntos (motivo, autor, instante) -- por isso
-- entram juntos aqui, nunca um UPDATE incremental. Mesmo filtro
-- `status = 'Pendente'` de MarcarSolicitacaoConvertida e pelo mesmo motivo:
-- não rejeitar (nem re-rejeitar) o que já saiu do estado inicial.
UPDATE solicitacao_os
SET status = 'Rejeitada',
    motivo_rejeicao = sqlc.arg(motivo_rejeicao),
    rejeitado_por_id = sqlc.arg(rejeitado_por_id),
    rejeitada_em = now()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND status = 'Pendente';
