-- ==========================================================================
-- Origem 'direta': Gestor/Administrador abre a OS sem passar pela fila.
--
-- Pedido dos gestores: quando o Solicitante não está disponível, quem aprova
-- abre a OS direto pelo próprio perfil. A solicitação continua existindo pelo
-- mesmo motivo da de preventiva (horas_parada mede desde criado_em,
-- uq_os_solicitacao exige uma por OS) e já nasce 'Convertida', com a OS na
-- mesma transação.
--
-- Origem nova, e não 'solicitante' com outro autor, por causa da foto:
-- fn_check_solicitacao_tem_foto (000005) exige anexo para origem =
-- 'solicitante', e a OS direta é aberta sem foto -- a foto existe para o
-- Gestor avaliar antes de aprovar, e aqui quem abre É quem aprovaria. O
-- trigger não muda: ele já corta por origem = 'solicitante'.
--
-- ck_origem reescrito: solicitante_id passa a valer para toda origem humana
-- (solicitante e direta -- é "quem abriu"), proibido só na preventiva. Escrito
-- sem citar 'direta' de propósito: o Postgres recusa usar um valor de ENUM na
-- mesma transação que o criou ("unsafe use of new value"), e assim a
-- migration fica numa só.
-- ==========================================================================

ALTER TYPE origem_solicitacao ADD VALUE 'direta';

ALTER TABLE solicitacao_os DROP CONSTRAINT ck_origem;
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_origem CHECK (
    ((origem = 'preventiva') = (preventiva_id  IS NOT NULL)) AND
    ((origem = 'preventiva') = (solicitante_id IS NULL)));
