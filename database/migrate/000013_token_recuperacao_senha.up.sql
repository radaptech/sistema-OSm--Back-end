-- Guarda o SHA-256 do token, nunca o token: quem ler o banco (backup, pgAdmin) não redefine a senha de ninguém.
ALTER TABLE usuario
    ADD COLUMN token_recuperacao_hash      text,
    ADD COLUMN token_recuperacao_expira_em timestamptz,
    -- Hash sem validade seria um token eterno; os dois nascem e morrem juntos.
    ADD CONSTRAINT ck_usuario_token_recuperacao CHECK (
        (token_recuperacao_hash IS NULL) = (token_recuperacao_expira_em IS NULL)
    );
