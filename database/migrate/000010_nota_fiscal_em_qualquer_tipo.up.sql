-- ==========================================================================
-- Nota fiscal deixa de ser exclusividade de OS de terceiros.
--
-- ck_custo_por_tipo nasceu espelhando uma premissa (secao 5.2): dado de nota
-- fiscal so existiria em 'terceiros', porque a unica nota em jogo era a
-- fatura da empresa externa. Na pratica nao e -- maquinario troca peca
-- comprada com nota, reparo consome material comprado com nota. O
-- Administrador precisa registrar o documento que embasa o custo em QUALQUER
-- tipo de OS, e hoje o CHECK o impede.
--
-- Afrouxa APENAS numero_nota_fiscal e serie_nota_fiscal. As outras duas
-- regras continuam de pe, cada uma por motivo proprio:
--
--   custo_hora_tecnico so em 'maquinario' -- em 'terceiros' quem trabalhou
--   foi a empresa e em 'reparo' o servico nao cobra hora tecnica;
--
--   descricao_servico_terceiro so em 'terceiros' -- o campo descreve o que a
--   EMPRESA EXTERNA fez, e nao ha equivalente nos outros tipos: o que o
--   Tecnico fez ja mora em os_encerramento.solucao, que existe para todos
--   (1.4.3).
--
-- Nada muda na denormalizacao de `tipo` (secao 3.5): a FK composta
-- (ordem_servico_id, tipo) continua sendo o que deixa este CHECK enxergar o
-- tipo sem consultar a tabela pai, e as duas regras que sobraram ainda
-- dependem dela.
--
-- Sem backfill: a mudanca so AMPLIA o que e aceito, entao toda linha ja
-- gravada continua valida.
-- ==========================================================================
ALTER TABLE os_custo DROP CONSTRAINT ck_custo_por_tipo;

ALTER TABLE os_custo ADD CONSTRAINT ck_custo_por_tipo CHECK (
    (tipo = 'maquinario' OR custo_hora_tecnico IS NULL) AND
    (tipo = 'terceiros'  OR descricao_servico_terceiro IS NULL));
