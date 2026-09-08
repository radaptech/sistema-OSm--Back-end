-- Reverte 000012: custo volta a ser um par de valores e nota fiscal volta a
-- ser uma so.
--
-- ⚠️ ESTA DESCIDA PERDE DADO assim que o recurso for usado de verdade, e nao
-- ha como nao perder: o modelo antigo tem UMA coluna para cada grandeza e o
-- novo tem N linhas. O que sobrevive:
--
--   custo_manutencao / custo_hora_tecnico -- INTEIROS. Eles nunca deixaram de
--   existir e ja carregam a soma dos itens (o service escreve os dois na
--   mesma transacao), entao nenhum centavo se perde. O que se perde e a
--   DISCRIMINACAO por tarefa: "rolamento 180 + fita 240" volta a ser "420",
--   sem dizer de que era feito nem qual mao de obra pertencia a qual peca.
--
--   nota fiscal -- so a PRIMEIRA por OS, por criado_em. A segunda nota em
--   diante desaparece, porque nao existe segunda coluna para recebe-la.
--
-- Ou seja: a descida e limpa enquanto ninguem tiver lancado mais de um item
-- nem mais de uma nota na mesma OS. Depois disso ela e uma decisao, nao um
-- rollback -- mesmo aviso que a 000008 carrega. O rollback do Railway reverte
-- o binario, nao o schema.

-- 1. As colunas voltam, nullable, para o backfill poder rodar antes do CHECK.
ALTER TABLE os_custo ADD COLUMN numero_nota_fiscal text;
ALTER TABLE os_custo ADD COLUMN serie_nota_fiscal  text;

-- 2. A primeira nota de cada OS volta para as colunas. DISTINCT ON precisa
--    que o ORDER BY comece pela mesma expressao do DISTINCT ON; criado_em
--    depois dele e o que escolhe a mais antiga, e o id desempata notas
--    gravadas no mesmo instante (o backfill da subida grava todas com o
--    mesmo now()).
UPDATE os_custo c
   SET numero_nota_fiscal = n.numero,
       serie_nota_fiscal  = n.serie
  FROM (SELECT DISTINCT ON (ordem_servico_id)
               ordem_servico_id, numero, serie
          FROM os_nota_fiscal
         ORDER BY ordem_servico_id, criado_em, id) n
 WHERE n.ordem_servico_id = c.ordem_servico_id;

-- 3. O CHECK da 000011 volta, agora que as colunas existem e estao coerentes
--    com tem_nota_fiscal (o trigger que sai logo abaixo garantia isso).
ALTER TABLE os_custo ADD CONSTRAINT ck_custo_nota_fiscal CHECK (
    tem_nota_fiscal OR (numero_nota_fiscal IS NULL AND serie_nota_fiscal IS NULL));

-- 4. Ordem inversa da subida: trigger e funcao, depois indices, tabelas e o
--    tipo por ultimo (tabela e tipo dividem namespace -- ver 000004).
DROP TRIGGER trg_nota_fiscal_declarada ON os_nota_fiscal;
DROP FUNCTION fn_check_nota_fiscal_declarada();

DROP INDEX idx_nota_fiscal_tenant;
DROP INDEX idx_nota_fiscal_ordem_servico;
DROP INDEX idx_custo_item_tenant;
DROP INDEX idx_custo_item_ordem_servico;

DROP TABLE os_nota_fiscal;
DROP TABLE os_custo_item;
