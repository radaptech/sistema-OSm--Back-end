-- ==========================================================================
-- OS de terceiros passa a ter mão de obra: Valor Peças + Valor Mão de Obra.
--
-- Até aqui a hora técnica era proibida em 'terceiros' com o argumento de que
-- "quem trabalhou foi a empresa externa" -- e por isso a OS terceirizada só
-- tinha um valor por tarefa (custo_manutencao). Na prática a fatura da
-- empresa separa peças de mão de obra, e o Técnico precisa lançar as duas.
--
-- Nenhuma coluna nova: os_custo_item já é uma TAREFA com material E mão de
-- obra (000012). Em terceiros, custo_manutencao é o valor das peças e
-- custo_hora_tecnico é a mão de obra cobrada pela EMPRESA, não pelo Técnico
-- -- a tela troca os rótulos pelo tipo da OS. Reaproveitar a coluna mantém
-- custo_total, vw_os_finalizada e os indicadores somando a mão de obra sem
-- tocar em nenhum deles; uma coluna nova exigiria somá-la em cada um.
--
-- Só 'reparo' continua sem mão de obra: o serviço é pequeno demais para ser
-- cobrado à parte.
-- ==========================================================================

ALTER TABLE os_custo_item DROP CONSTRAINT ck_custo_item_hora_tecnico;
ALTER TABLE os_custo_item ADD CONSTRAINT ck_custo_item_hora_tecnico CHECK (
    tipo <> 'reparo' OR custo_hora_tecnico IS NULL);

-- descricao_servico_terceiro continua só em 'terceiros' (000010).
ALTER TABLE os_custo DROP CONSTRAINT ck_custo_por_tipo;
ALTER TABLE os_custo ADD CONSTRAINT ck_custo_por_tipo CHECK (
    (tipo <> 'reparo' OR custo_hora_tecnico IS NULL) AND
    (tipo = 'terceiros' OR descricao_servico_terceiro IS NULL));
