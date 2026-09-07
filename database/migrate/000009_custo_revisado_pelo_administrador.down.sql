-- Reverte 000009: a marca de conferencia de custo volta a ser so do
-- navegador (localStorage do front). Nada a preservar -- a coluna e
-- derivavel de novo pela acao do Administrador em Custos Pendentes.
ALTER TABLE os_custo DROP COLUMN custo_revisado_em;
