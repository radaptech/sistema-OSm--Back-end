-- ==========================================================================
-- Foto obrigatoria so na solicitacao aberta por gente.
--
-- fn_check_solicitacao_tem_foto exigia anexo do tipo foto em TODA solicitacao.
-- A regra nasceu do fluxo humano e esta certa ali: o Solicitante fotografa o
-- problema e o Gestor avalia pela foto antes de abrir a OS -- o front torna a
-- foto obrigatoria nos dois tipos que ele abre.
--
-- Mas a solicitacao de origem 'preventiva' nao tem foto e nao tem como ter:
-- ninguem fotografou nada, ela nasce de uma data no calendario quando o job
-- percorre as preventivas vencidas (docs/modelagem-banco-dados.md 3.9, e o
-- ModalManutencaoPreventiva em front-end/CLAUDE.md). Com o trigger como
-- estava, o INSERT do job falhava no commit:
--
--   ERROR: solicitacao 1 precisa de ao menos um anexo do tipo foto
--
-- Ou seja: a abertura automatica de solicitacao estava bloqueada pelo schema,
-- nao so por falta de codigo.
--
-- O corte usa `origem` e nao `solicitante_id` porque ck_origem ja amarra os
-- dois ((origem = 'solicitante') = (solicitante_id IS NOT NULL)), e `origem` e
-- o campo que diz a intencao. Continua CONSTRAINT TRIGGER DEFERRABLE: a linha
-- do anexo so pode existir depois da solicitacao, entao a checagem tem que
-- rodar no fim da transacao.
-- ==========================================================================

CREATE OR REPLACE FUNCTION fn_check_solicitacao_tem_foto() RETURNS trigger AS $$
BEGIN
    IF NEW.origem = 'solicitante' AND NOT EXISTS (
        SELECT 1 FROM solicitacao_anexo
         WHERE solicitacao_id = NEW.id AND tipo = 'foto'
    ) THEN
        RAISE EXCEPTION 'solicitação % precisa de ao menos um anexo do tipo foto', NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
