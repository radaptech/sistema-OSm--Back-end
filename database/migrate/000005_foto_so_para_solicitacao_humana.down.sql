-- Reverte 000005: volta a exigir foto em toda solicitacao, inclusive na de
-- origem 'preventiva'. Corpo identico ao de 000001.
--
-- ⚠️ Descer esta migration com solicitacao automatica ja gravada nao apaga
-- nada (o trigger so roda em INSERT/UPDATE da propria linha), mas qualquer
-- UPDATE posterior numa dessas linhas passa a falhar.

CREATE OR REPLACE FUNCTION fn_check_solicitacao_tem_foto() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM solicitacao_anexo
         WHERE solicitacao_id = NEW.id AND tipo = 'foto'
    ) THEN
        RAISE EXCEPTION 'solicitação % precisa de ao menos um anexo do tipo foto', NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
