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
