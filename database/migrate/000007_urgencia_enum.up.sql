-- ==========================================================================
-- nivel_urgencia: tabela por tenant -> ENUM.
--
-- Mesma lacuna e mesmo motivo de 000004 (nivel_criticidade): o front tipa
-- niveisUrgencia como tupla fixa ('Baixa','Média','Alta',
-- front-end/src/tipos/ordemServico.ts) e nao ha tela de cadastro/edicao de
-- urgencia em lugar nenhum -- so o ModalAbrirOrdemServico faz .map() sobre a
-- constante. nivel_urgencia ficou tabela em 000004 so porque "nao esta no
-- caminho deste CRUD e ordem_servico ainda nao tem escrita nenhuma"
-- (comentario da propria 000004) -- agora tem: CriarOrdemServicoDeSolicitacao
-- (solicitacao_os.sql) grava em ordem_servico.urgencia_id pela primeira vez,
-- e sem seed a tabela vazia travaria POST /solicitacoes/:id/abrir-os em todo
-- tenant, a mesma lacuna que area_tecnico tinha antes de 000006.
--
-- ⚠️ Diferente de 000004: aqui existem views (`SELECT os.*`) penduradas na
-- coluna -- vw_os_finalizada e vw_os_custo_sem_lancamento. Postgres expande
-- `os.*` no momento da criacao da view, entao as duas dependem de
-- urgencia_id por nome mesmo sem citar a coluna no texto -- um DROP COLUMN
-- direto falha com "other objects depend on it". As duas saem antes do
-- ALTER e voltam depois, com o texto identico (a mudanca de urgencia_id
-- para urgencia so aparece via `os.*`, nao no corpo da view).
--
-- ck_os_executor referencia urgencia_id -- precisa recriar apontando para a
-- coluna nova (o resto do CHECK nao muda). idx_os_urgencia some pelo mesmo
-- motivo de idx_maquina_criticidade em 000004: tres valores nao pagam o
-- custo de um indice.
--
-- Tabela e tipo dividem namespace no Postgres -- a tabela sai antes do tipo
-- homonimo entrar, dai a ordem abaixo.
-- ==========================================================================

DROP VIEW vw_os_custo_sem_lancamento;
DROP VIEW vw_os_finalizada;

ALTER TABLE ordem_servico DROP CONSTRAINT ck_os_executor;
ALTER TABLE ordem_servico DROP CONSTRAINT fk_os_urgencia;
DROP INDEX idx_os_urgencia;
ALTER TABLE ordem_servico DROP COLUMN urgencia_id;

DROP TABLE nivel_urgencia;

CREATE TYPE nivel_urgencia AS ENUM ('Baixa','Média','Alta');

-- NOT NULL: toda OS exige urgencia (regra ja documentada -- so mudou de FK
-- para ENUM) e a coluna nova entra numa ordem_servico que ainda nao tem
-- nenhum caminho de escrita no codigo -- a tabela continua vazia ate
-- abrir-os existir.
ALTER TABLE ordem_servico ADD COLUMN urgencia nivel_urgencia NOT NULL;

ALTER TABLE ordem_servico ADD CONSTRAINT ck_os_executor CHECK (
    tecnico_id IS NOT NULL AND urgencia IS NOT NULL AND
    ((tipo = 'terceiros') = (empresa_terceirizada_id IS NOT NULL)) AND
    ((empresa_terceirizada_id IS NOT NULL) = (terceiro_acionado_em IS NOT NULL)));

-- Texto identico ao de 000001 -- so o que `os.*` expande mudou (urgencia no
-- lugar de urgencia_id).
CREATE VIEW vw_os_finalizada AS
SELECT os.*,
       COALESCE(c.custo_hora_tecnico, 0) + c.custo_manutencao AS custo_total
  FROM ordem_servico os
  JOIN os_custo c ON c.ordem_servico_id = os.id
 WHERE os.status = 'Concluída';

CREATE VIEW vw_os_custo_sem_lancamento AS
SELECT os.*
  FROM ordem_servico os
 WHERE os.status = 'Concluída'
   AND NOT EXISTS (SELECT 1 FROM os_custo c WHERE c.ordem_servico_id = os.id);
