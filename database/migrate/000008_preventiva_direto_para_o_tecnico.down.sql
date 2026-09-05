-- Reverte 000008 na ordem inversa da subida: indice de volta, coluna de volta
-- a NOT NULL, coluna nova removida.
--
-- ⚠️ Descer esta migration com OS de preventiva ja gravada FALHA, e falha de
-- proposito. Duas razoes, nessa ordem:
--
--   aberta_por_id voltando a NOT NULL nao tem valor para preencher nas OS que
--   o job abriu -- nao houve ator. Inventar um aqui gravaria autoria falsa
--   justamente na coluna que responde "quem abriu";
--
--   uq_preventiva_pendente sobre solicitacao 'Pendente' nao conflita com nada
--   ja gravado (as do job nascem 'Convertida'), entao ele volta limpo -- mas
--   volta sem proteger a corrida, porque quem protege agora e o FOR UPDATE do
--   job.
--
-- Se a reversao for mesmo necessaria com dado dentro, o caminho e decidir
-- antes o que fazer com essas OS (apagar, ou eleger um responsavel), nao
-- afrouxar esta migration.

CREATE UNIQUE INDEX uq_preventiva_pendente ON solicitacao_os (preventiva_id)
    WHERE preventiva_id IS NOT NULL AND status = 'Pendente';

ALTER TABLE ordem_servico ALTER COLUMN aberta_por_id SET NOT NULL;

DROP INDEX idx_preventiva_tecnico;

ALTER TABLE preventiva DROP CONSTRAINT fk_preventiva_tecnico;

ALTER TABLE preventiva DROP COLUMN tecnico_id;
