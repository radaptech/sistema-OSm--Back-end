-- Reverte 000010: nota fiscal volta a valer so em 'terceiros'.
--
-- ⚠️ Descer isto com nota fiscal ja gravada numa OS de maquinario ou reparo
-- FALHA, e falha de proposito. O CHECK antigo nao tem como aceitar essas
-- linhas, e limpa-las aqui apagaria justamente o documento que embasa o
-- custo -- calado, e sem ninguem ter pedido. Se a reversao for mesmo
-- necessaria com dado dentro, o caminho e decidir antes o que fazer com
-- essas notas, nao afrouxar esta migration.
ALTER TABLE os_custo DROP CONSTRAINT ck_custo_por_tipo;

ALTER TABLE os_custo ADD CONSTRAINT ck_custo_por_tipo CHECK (
    (tipo = 'maquinario' OR custo_hora_tecnico IS NULL) AND
    (tipo = 'terceiros'  OR (numero_nota_fiscal IS NULL AND serie_nota_fiscal IS NULL
                             AND descricao_servico_terceiro IS NULL)));
