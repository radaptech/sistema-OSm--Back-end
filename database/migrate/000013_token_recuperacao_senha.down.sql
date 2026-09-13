ALTER TABLE usuario
    DROP CONSTRAINT ck_usuario_token_recuperacao,
    DROP COLUMN token_recuperacao_expira_em,
    DROP COLUMN token_recuperacao_hash;
