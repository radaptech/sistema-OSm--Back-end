-- ==========================================================================
-- Reverte os renomes de 000003_anexo_chave_r2.up.sql.
-- ==========================================================================

ALTER TABLE solicitacao_anexo RENAME COLUMN chave TO url;
ALTER TABLE maquina RENAME COLUMN foto_chave TO foto_url;
