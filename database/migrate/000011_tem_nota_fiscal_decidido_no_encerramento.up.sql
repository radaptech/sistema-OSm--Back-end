-- ==========================================================================
-- Nota fiscal deixa de ser adivinhada e passa a ser DECLARADA.
--
-- A 000010 soltou a nota fiscal do tipo da OS, mas deixou um buraco: nada
-- distingue "esta OS nao gerou nota" de "gerou e ninguem preencheu ainda". O
-- Administrador abria Custos Pendentes e via os campos de NF em toda OS, sem
-- saber em quais valia a pena cobrar o documento.
--
-- Quem sabe a resposta e o Tecnico, no momento em que encerra: foi ele que
-- executou o servico e sabe se houve compra (peca, material, fatura da
-- empresa externa) ou se foi so mao de obra. tem_nota_fiscal e essa
-- declaracao, gravada por CriarCusto na mesma transacao do encerramento.
--
-- DEFAULT false porque a resposta segura e "nao teve": um servico sem compra
-- e o caso comum, e marcar nota que nao existe convidaria a cobrar um
-- documento inexistente. O Administrador PODE corrigir depois
-- (AtualizarCusto tambem grava a coluna) -- Tecnico esquecer de marcar nao
-- pode deixar a OS sem onde lancar a nota.
--
-- Backfill pelo dado que ja existe: linha com numero de nota gravado
-- obviamente teve nota. As demais ficam no DEFAULT.
-- ==========================================================================
ALTER TABLE os_custo ADD COLUMN tem_nota_fiscal boolean NOT NULL DEFAULT false;

UPDATE os_custo SET tem_nota_fiscal = true WHERE numero_nota_fiscal IS NOT NULL;

-- CHECK proprio, e nao mais uma clausula dentro de ck_custo_por_tipo: sao
-- eixos diferentes. ck_custo_por_tipo fala do TIPO da OS (o que a natureza do
-- servico permite); este fala do que o Tecnico DECLAROU nesta OS especifica.
-- Misturar os dois num CHECK so deixaria a mensagem de erro ambigua.
--
-- So a direcao "declarou que nao teve" e travada. O contrario -- declarou que
-- teve e ainda nao preencheu -- e estado legitimo e frequente: e exatamente a
-- fila de conferencia do Administrador em Custos Pendentes.
ALTER TABLE os_custo ADD CONSTRAINT ck_custo_nota_fiscal CHECK (
    tem_nota_fiscal OR (numero_nota_fiscal IS NULL AND serie_nota_fiscal IS NULL));
