-- Falha se já houver OS de terceiros com mão de obra lançada -- de propósito:
-- apagar valor de custo não é papel de um down.
ALTER TABLE os_custo_item DROP CONSTRAINT ck_custo_item_hora_tecnico;
ALTER TABLE os_custo_item ADD CONSTRAINT ck_custo_item_hora_tecnico CHECK (
    tipo = 'maquinario' OR custo_hora_tecnico IS NULL);

ALTER TABLE os_custo DROP CONSTRAINT ck_custo_por_tipo;
ALTER TABLE os_custo ADD CONSTRAINT ck_custo_por_tipo CHECK (
    (tipo = 'maquinario' OR custo_hora_tecnico IS NULL) AND
    (tipo = 'terceiros'  OR descricao_servico_terceiro IS NULL));
