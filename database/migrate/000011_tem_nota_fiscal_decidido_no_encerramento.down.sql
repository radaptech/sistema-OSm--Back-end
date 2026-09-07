-- Reverte 000011: volta a nao existir declaracao de nota fiscal.
--
-- Desce sem perda real: numero_nota_fiscal/serie_nota_fiscal continuam onde
-- estao, e a informacao que a coluna carregava ("teve nota?") volta a ser
-- deduzida da presenca do numero -- que e exatamente de onde o backfill da
-- subida a tirou. O que se perde e o caso "declarou que teve mas ainda nao
-- preencheu", que vira indistinguivel de "nao teve".
ALTER TABLE os_custo DROP CONSTRAINT ck_custo_nota_fiscal;

ALTER TABLE os_custo DROP COLUMN tem_nota_fiscal;
