-- Reverte 000007 na ordem inversa exata do up: views -> constraint/coluna
-- nova -> tipo -> tabela antiga -> coluna/constraint/indice antigos -> views
-- de volta.

DROP VIEW vw_os_custo_sem_lancamento;
DROP VIEW vw_os_finalizada;

ALTER TABLE ordem_servico DROP CONSTRAINT ck_os_executor;
ALTER TABLE ordem_servico DROP COLUMN urgencia;

DROP TYPE nivel_urgencia;

CREATE TABLE nivel_urgencia (
    id         smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES empresa (id),
    nome       text NOT NULL,
    ordem      smallint NOT NULL,
    CONSTRAINT uq_nivel_urgencia_tenant UNIQUE (tenant_id, id)
);

ALTER TABLE ordem_servico ADD COLUMN urgencia_id smallint;

ALTER TABLE ordem_servico ADD CONSTRAINT fk_os_urgencia
    FOREIGN KEY (tenant_id, urgencia_id) REFERENCES nivel_urgencia (tenant_id, id);

CREATE INDEX idx_os_urgencia ON ordem_servico (urgencia_id);

ALTER TABLE ordem_servico ADD CONSTRAINT ck_os_executor CHECK (
    tecnico_id IS NOT NULL AND urgencia_id IS NOT NULL AND
    ((tipo = 'terceiros') = (empresa_terceirizada_id IS NOT NULL)) AND
    ((empresa_terceirizada_id IS NOT NULL) = (terceiro_acionado_em IS NOT NULL)));

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
