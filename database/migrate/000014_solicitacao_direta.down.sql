-- Postgres não remove valor de ENUM: o tipo é recriado sem 'direta'. Falha se
-- já existir solicitação direta -- de propósito, apagar histórico de OS não é
-- papel de um down.
ALTER TABLE solicitacao_os DROP CONSTRAINT ck_origem;

ALTER TYPE origem_solicitacao RENAME TO origem_solicitacao_old;
CREATE TYPE origem_solicitacao AS ENUM ('solicitante','preventiva');
ALTER TABLE solicitacao_os ALTER COLUMN origem TYPE origem_solicitacao USING origem::text::origem_solicitacao;
DROP TYPE origem_solicitacao_old;

ALTER TABLE solicitacao_os ADD CONSTRAINT ck_origem CHECK (
    ((origem = 'preventiva')  = (preventiva_id  IS NOT NULL)) AND
    ((origem = 'solicitante') = (solicitante_id IS NOT NULL)));
