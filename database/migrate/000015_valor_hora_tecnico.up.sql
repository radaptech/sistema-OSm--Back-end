-- ==========================================================================
-- Valor/hora no cadastro do Técnico.
--
-- Até aqui o técnico não tinha valor/hora: o custo da mão de obra era digitado
-- por ele em cada encerramento (os_custo_item.custo_hora_tecnico), podendo
-- variar por OS. O Administrador passou a cadastrar a tarifa de referência do
-- técnico junto com o resto do cadastro.
--
-- É REFERÊNCIA, não regra: nenhuma coluna de custo passa a ser derivada dela.
-- O que a OS custou continua sendo o que foi lançado nela -- trocar a tarifa
-- amanhã não pode reescrever o custo das OS de ontem, e é por isso que o valor
-- não é lido por nenhum cálculo de custo/indicador.
--
-- Nullable e sem default: técnico já cadastrado não tem tarifa conhecida, e
-- zero seria mentira (zero é valor legítimo, "não cobra hora"). numeric(12,2)
-- pelo mesmo motivo de os_custo: é dinheiro.
-- ==========================================================================

ALTER TABLE usuario ADD COLUMN valor_hora numeric(12,2);

-- Só técnico tem tarifa, e nunca negativa. Mesmo eixo de
-- ck_usuario_area_tecnico, mas sem a obrigatoriedade: área é necessária para
-- o técnico existir, valor/hora não. Trocar o perfil de técnico para outro
-- tem que zerar a coluna junto -- o service manda NULL fora do perfil técnico.
ALTER TABLE usuario ADD CONSTRAINT ck_usuario_valor_hora CHECK (
    valor_hora IS NULL OR (perfil = 'tecnico' AND valor_hora >= 0));
