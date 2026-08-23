-- Reverte 000004 na ordem inversa exata do up.

ALTER TABLE maquina DROP COLUMN criticidade;

DROP TYPE nivel_criticidade;

CREATE TABLE nivel_criticidade (
    id         smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES empresa (id),
    nome       text NOT NULL,
    ordem      smallint NOT NULL,
    CONSTRAINT uq_nivel_criticidade_tenant UNIQUE (tenant_id, id)
);

ALTER TABLE maquina ADD COLUMN criticidade_id smallint;

ALTER TABLE maquina ADD CONSTRAINT fk_maquina_criticidade
    FOREIGN KEY (tenant_id, criticidade_id) REFERENCES nivel_criticidade (tenant_id, id);

CREATE INDEX idx_maquina_criticidade ON maquina (criticidade_id);
