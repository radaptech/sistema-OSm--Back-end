-- ==========================================================================
-- Sessão de login guardada no banco -- substitui o JWT de 8h.
--
-- Antes o cookie levava um JWT assinado que valia sozinho até o exp: o
-- servidor não guardava nada, então "Sair" só apagava o cookie daquele
-- aparelho (uma cópia do token seguia valendo), desativar um usuário não o
-- derrubava das rotas (só do /autenticacao/sessao) e o perfil gravado no token
-- só mudava no próximo login. E 8h fixas obrigavam quem usa o app no celular a
-- logar de novo no meio do dia.
--
-- Agora o cookie leva um segredo aleatório e esta tabela guarda o HASH dele
-- (SHA-256: o segredo tem 256 bits aleatórios, não é senha de humano -- mesmo
-- raciocínio do token de recuperação de senha). Vazar o banco não entrega
-- sessão nenhuma.
--
-- A cada request o middleware procura a linha e confere, junto, usuário ativo
-- e empresa ativa (o perfil também sai daqui, sempre o atual). A sessão:
--   - estica sozinha com o uso (ultimo_uso_em);
--   - cai depois de 14 dias SEM uso;
--   - cai 60 dias depois do login, mesmo com uso diário (teto absoluto);
--   - cai na hora no logout, ao desativar o usuário ou ao trocar a senha dele
--     (aí todas as sessões do usuário).
-- Os prazos moram no service (sessaoService.go), não aqui: o banco só guarda
-- os instantes.
--
-- Derrubar é DELETE, não uma coluna de "revogada" (exceção ao soft delete,
-- como dispositivo_push): sessão não é entidade com histórico, e linha que não
-- existe é o jeito mais difícil de esquecer um filtro. As vencidas por prazo
-- são apagadas na limpeza do login.
-- ==========================================================================

CREATE TABLE sessao (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id      bigint NOT NULL REFERENCES empresa (id),
    usuario_id     bigint NOT NULL,
    token_hash     bytea NOT NULL,
    criada_em      timestamptz NOT NULL DEFAULT now(),
    ultimo_uso_em  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT fk_sessao_usuario
        FOREIGN KEY (tenant_id, usuario_id) REFERENCES usuario (tenant_id, id),
    CONSTRAINT uq_sessao_token_hash UNIQUE (token_hash),
    CONSTRAINT ck_sessao_token_hash CHECK (length(token_hash) = 32)
);

-- Revogar todas as sessões de um usuário (desativar, trocar senha).
CREATE INDEX idx_sessao_usuario ON sessao (tenant_id, usuario_id);
