-- ==========================================================================
-- Solicitacao OS -- Schema inicial (DER revisao 4)
-- Fonte: docs/der-banco-dados.mmd + docs/modelagem-banco-dados.md
-- Gerado a partir das secoes 3, 4 e 5 do documento de modelagem.
-- ==========================================================================

-- --------------------------------------------------------------------------
-- 0. Extensoes
-- --------------------------------------------------------------------------

-- citext: e-mail de usuario e comparado sem diferenciar maiusculas/minusculas
-- (uq_usuario_email usa esse tipo em vez de lower(email) em cada consulta).
CREATE EXTENSION IF NOT EXISTS citext;

-- --------------------------------------------------------------------------
-- 1. Tipos ENUM (secao 5.1) -- listas que so mudam com deploy
-- --------------------------------------------------------------------------

CREATE TYPE perfil_usuario     AS ENUM ('solicitante','tecnico','gestor','administrador');
CREATE TYPE tipo_solicitacao   AS ENUM ('maquinario','reparo');
CREATE TYPE tipo_os            AS ENUM ('maquinario','terceiros','reparo');
CREATE TYPE tipo_defeito       AS ENUM ('Predial','Corretiva');
CREATE TYPE origem_solicitacao AS ENUM ('solicitante','preventiva');
CREATE TYPE status_solicitacao AS ENUM ('Pendente','Convertida','Rejeitada');
CREATE TYPE status_os          AS ENUM ('Aberta','Em Andamento','Pausada','Concluída');
CREATE TYPE marcador_impacto   AS ENUM ('Afeta Produção');
CREATE TYPE tipo_anexo         AS ENUM ('foto','video');

-- --------------------------------------------------------------------------
-- 2. Raiz multi-tenant e estrutura organizacional (secao 3.1)
-- empresa E o tenant: 1 cliente SaaS = 1 empresa = 1 subdominio.
-- --------------------------------------------------------------------------

CREATE TABLE empresa (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subdominio  text NOT NULL,
    nome        text NOT NULL,
    ativa       boolean NOT NULL DEFAULT true,
    criado_em   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_empresa_subdominio UNIQUE (subdominio)
);

CREATE TABLE loja (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES empresa (id),
    nome       text NOT NULL,
    ativa      boolean NOT NULL DEFAULT true,
    criado_em  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_loja_tenant_nome UNIQUE (tenant_id, nome),
    -- Habilita a FK composta (tenant_id, loja_id) que setor usa abaixo.
    CONSTRAINT uq_loja_tenant UNIQUE (tenant_id, id)
);

CREATE TABLE setor (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES empresa (id),
    loja_id    bigint NOT NULL,
    nome       text NOT NULL,
    ativo      boolean NOT NULL DEFAULT true,
    criado_em  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_setor_loja UNIQUE (loja_id, nome),
    CONSTRAINT uq_setor_tenant UNIQUE (tenant_id, id)
);

-- Cadastro do Administrador, sem vinculo de loja: o mesmo fornecedor atende
-- quantas lojas do tenant forem necessarias (secao 3.9).
CREATE TABLE empresa_terceirizada (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id      bigint NOT NULL REFERENCES empresa (id),
    nome           text NOT NULL,
    especialidade  text,
    telefone       text,
    ativa          boolean NOT NULL DEFAULT true,
    criado_em      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_empresa_terceirizada_nome UNIQUE (tenant_id, nome),
    CONSTRAINT uq_empresa_terceirizada_tenant UNIQUE (tenant_id, id)
);

-- --------------------------------------------------------------------------
-- 3. Tabelas de dominio (secao 2.4) -- vocabulario do cliente, cadastravel
-- --------------------------------------------------------------------------

CREATE TABLE area_tecnico (
    id         smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES empresa (id),
    nome       text NOT NULL,
    CONSTRAINT uq_area_tecnico_tenant UNIQUE (tenant_id, id)
);

CREATE TABLE nivel_criticidade (
    id         smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES empresa (id),
    nome       text NOT NULL,
    ordem      smallint NOT NULL,
    CONSTRAINT uq_nivel_criticidade_tenant UNIQUE (tenant_id, id)
);

CREATE TABLE nivel_urgencia (
    id         smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES empresa (id),
    nome       text NOT NULL,
    ordem      smallint NOT NULL,
    CONSTRAINT uq_nivel_urgencia_tenant UNIQUE (tenant_id, id)
);

-- --------------------------------------------------------------------------
-- 4. Usuarios e escopo de acesso (secao 3.8)
-- Um unico modelo de escopo serve aos quatro perfis:
--   solicitante   -> 1 escopo, 1 setor
--   tecnico       -> N escopos (lojas), sem setor
--   gestor        -> N escopos, cada um com setores ou acesso total
--   administrador -> NENHUM escopo (a ausencia significa o tenant inteiro)
-- --------------------------------------------------------------------------

CREATE TABLE usuario (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id        bigint NOT NULL REFERENCES empresa (id),
    perfil           perfil_usuario NOT NULL,
    area_tecnico_id  smallint,
    nome             text NOT NULL,
    email            citext NOT NULL,
    senha_hash       text NOT NULL,
    telefone         text,
    ativo            boolean NOT NULL DEFAULT true,
    ultimo_acesso    timestamptz,
    criado_em        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_usuario_email UNIQUE (tenant_id, email),
    CONSTRAINT uq_usuario_tenant UNIQUE (tenant_id, id)
);

CREATE TABLE usuario_escopo (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id             bigint NOT NULL REFERENCES usuario (id),
    loja_id                bigint NOT NULL REFERENCES loja (id),
    acesso_total_setores   boolean NOT NULL DEFAULT false,
    criado_em              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE usuario_escopo_setor (
    escopo_id  bigint NOT NULL REFERENCES usuario_escopo (id),
    setor_id   bigint NOT NULL REFERENCES setor (id),
    PRIMARY KEY (escopo_id, setor_id)
);

-- --------------------------------------------------------------------------
-- 5. Maquinas e manutencao preventiva
-- A maquina referencia apenas setor_id: a loja vem por join, evitando um
-- par (loja, setor) que possa se contradizer.
-- --------------------------------------------------------------------------

CREATE TABLE maquina (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id          bigint NOT NULL REFERENCES empresa (id),
    setor_id           bigint NOT NULL,
    criticidade_id     smallint,
    numero_patrimonio  text NOT NULL,
    numero_serie       text,
    nome               text NOT NULL,
    descricao          text,
    marca              text,
    modelo             text,
    foto_url           text,
    ativa              boolean NOT NULL DEFAULT true,
    criado_em          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_maquina_patrim UNIQUE (tenant_id, numero_patrimonio),
    CONSTRAINT uq_maquina_tenant UNIQUE (tenant_id, id)
);

-- Serie vem do fabricante e pode faltar: indice unico parcial, nao UNIQUE comum.
CREATE UNIQUE INDEX uq_maquina_serie ON maquina (tenant_id, numero_serie)
    WHERE numero_serie IS NOT NULL;

CREATE TABLE preventiva (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id       bigint NOT NULL REFERENCES empresa (id),
    maquina_id      bigint NOT NULL,
    descricao       text NOT NULL,
    intervalo_dias  integer NOT NULL,
    proxima_data    date NOT NULL,
    ativa           boolean NOT NULL DEFAULT true,
    criado_em       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_intervalo CHECK (intervalo_dias > 0),
    CONSTRAINT uq_preventiva_tenant UNIQUE (tenant_id, id)
);

-- --------------------------------------------------------------------------
-- 6. Solicitacao de OS -- so os dois tipos que o Solicitante abre
-- maquinario: exige maquina_id
-- reparo:     exige item_descricao (texto livre), sem maquina
-- Nao existe solicitacao "de terceiros": terceirizar e decisao do Tecnico
-- depois da OS aberta. A classificacao Predial/Corretiva tambem nao esta
-- aqui -- quem relata o problema nao classifica o servico (ver
-- os_encerramento.tipo_defeito, secao 6 abaixo).
-- --------------------------------------------------------------------------

CREATE TABLE solicitacao_os (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id         bigint NOT NULL REFERENCES empresa (id),
    tipo              tipo_solicitacao NOT NULL,
    maquina_id        bigint,
    item_descricao    text,
    setor_id          bigint NOT NULL,
    solicitante_id    bigint,
    preventiva_id     bigint,
    status            status_solicitacao NOT NULL DEFAULT 'Pendente',
    origem            origem_solicitacao NOT NULL,
    descricao         text NOT NULL,
    motivo_rejeicao   text,
    rejeitado_por_id  bigint,
    rejeitada_em      timestamptz,
    criado_em         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_solicitacao_tenant UNIQUE (tenant_id, id)
);

-- Marcador unico hoje (Afeta Producao), mas associativa e nao boolean: o
-- contrato ja troca uma lista (impactos) e um marcador novo nao mexe na
-- tabela (secao 3.11).
CREATE TABLE solicitacao_impacto (
    solicitacao_id  bigint NOT NULL REFERENCES solicitacao_os (id),
    marcador        marcador_impacto NOT NULL,
    PRIMARY KEY (solicitacao_id, marcador)
);

-- Foto do defeito obrigatoria e video opcional. Tabela em vez de duas
-- colunas: aguenta N arquivos e ja tem onde guardar o que o storage exige.
CREATE TABLE solicitacao_anexo (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    solicitacao_id  bigint NOT NULL REFERENCES solicitacao_os (id),
    tipo            tipo_anexo NOT NULL,
    url             text NOT NULL,
    mime_type       text NOT NULL,
    tamanho_bytes   bigint NOT NULL,
    criado_em       timestamptz NOT NULL DEFAULT now()
);

-- --------------------------------------------------------------------------
-- 7. Ordem de Servico e seu ciclo de vida
-- Toda OS tem tecnico responsavel e urgencia, e toda OS passa pelo mesmo
-- ciclo (iniciar / pausar / retomar / encerrar). O tipo comeca igual ao da
-- solicitacao e vira 'terceiros' quando o Tecnico aciona uma empresa
-- externa -- acionar nao encerra nada e nao tira a OS dele.
-- --------------------------------------------------------------------------

CREATE TABLE ordem_servico (
    id                       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id                bigint NOT NULL REFERENCES empresa (id),
    solicitacao_id           bigint NOT NULL,
    tipo                     tipo_os NOT NULL,
    tecnico_id               bigint NOT NULL,
    urgencia_id              smallint NOT NULL,
    empresa_terceirizada_id  bigint,
    terceiro_acionado_em     timestamptz,
    aberta_por_id            bigint NOT NULL,
    afeta_producao           boolean NOT NULL,
    status                   status_os NOT NULL DEFAULT 'Aberta',
    aberta_em                timestamptz NOT NULL DEFAULT now(),
    iniciada_em              timestamptz,
    criado_em                timestamptz NOT NULL DEFAULT now(),
    -- Uma solicitacao vira no maximo uma OS (secao 5.3).
    CONSTRAINT uq_os_solicitacao UNIQUE (solicitacao_id),
    -- Habilita a FK composta (ordem_servico_id, tipo) que os_custo e
    -- os_encerramento usam, com ON UPDATE CASCADE (o tipo pode ser
    -- promovido a 'terceiros' com a OS ja aberta -- secao 1.4.1).
    CONSTRAINT uq_os_tipo UNIQUE (id, tipo),
    CONSTRAINT uq_os_tenant UNIQUE (tenant_id, id)
);

-- Historico, nao campo sobrescrito: tres pausas seguidas precisam das tres
-- linhas. E dele que sai o calculo de horas trabalhadas (secao 3.3).
CREATE TABLE os_pausa (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ordem_servico_id  bigint NOT NULL REFERENCES ordem_servico (id),
    status_anterior   status_os NOT NULL,
    motivo            text NOT NULL,
    pausada_em        timestamptz NOT NULL DEFAULT now(),
    retomada_em       timestamptz,
    -- Front tipa isso como StatusRetomavel ('Aberta' | 'Em Andamento') --
    -- ver tipos/ordemServico.ts.
    CONSTRAINT ck_pausa_status_anterior CHECK (status_anterior IN ('Aberta','Em Andamento'))
);

-- Registro de execucao do Tecnico. Existe para os tres tipos: mesmo a OS
-- terceirizada e encerrada por ele, que recebe o servico da empresa. E aqui
-- que a OS e classificada como Predial ou Corretiva (secao 1.4.4).
CREATE TABLE os_encerramento (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id           bigint NOT NULL REFERENCES empresa (id),
    ordem_servico_id    bigint NOT NULL,
    tipo                tipo_os NOT NULL,
    tipo_defeito        tipo_defeito NOT NULL,
    encerrado_por_id    bigint NOT NULL,
    data_fim            timestamptz NOT NULL DEFAULT now(),
    defeito_constatado  text NOT NULL,
    causa_raiz          text NOT NULL,
    solucao             text NOT NULL,
    criado_em           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_encerramento_os UNIQUE (ordem_servico_id)
);

-- Lancamento financeiro, separado da execucao: a linha nasce no encerramento
-- (o Tecnico informa os valores) e o Administrador corrige depois em Custos
-- Pendentes. Sua existencia e o que torna a OS "finalizada" (secao 2.3).
CREATE TABLE os_custo (
    id                          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id                   bigint NOT NULL REFERENCES empresa (id),
    ordem_servico_id            bigint NOT NULL,
    tipo                        tipo_os NOT NULL,
    custo_hora_tecnico          numeric(12,2),
    custo_manutencao            numeric(12,2) NOT NULL,
    numero_nota_fiscal          text,
    serie_nota_fiscal           text,
    descricao_servico_terceiro  text,
    lancado_por_id              bigint NOT NULL,
    lancado_em                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_custo_os UNIQUE (ordem_servico_id),
    CONSTRAINT ck_custo_nao_negativo
        CHECK (custo_manutencao >= 0 AND COALESCE(custo_hora_tecnico, 0) >= 0)
);

-- --------------------------------------------------------------------------
-- 8. Coerencia dos tipos (secao 5.2, revista na revisao 4)
-- --------------------------------------------------------------------------

-- Reparo nao tem maquina cadastrada; Maquinario exige a FK e proibe o texto
-- livre. Setor e obrigatorio nos dois: no Maquinario vem da maquina, no
-- Reparo do escopo do Solicitante (secao 1.4.7).
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_solicitacao_alvo CHECK (
    (tipo = 'reparo'     AND maquina_id IS NULL     AND item_descricao IS NOT NULL) OR
    (tipo = 'maquinario' AND maquina_id IS NOT NULL AND item_descricao IS NULL));

-- Toda OS tem tecnico e urgencia. A empresa terceirizada existe se e somente
-- se o Tecnico acionou uma -- o que e exatamente o que 'terceiros' significa.
ALTER TABLE ordem_servico ADD CONSTRAINT ck_os_executor CHECK (
    tecnico_id IS NOT NULL AND urgencia_id IS NOT NULL AND
    ((tipo = 'terceiros') = (empresa_terceirizada_id IS NOT NULL)) AND
    ((empresa_terceirizada_id IS NOT NULL) = (terceiro_acionado_em IS NOT NULL)));

-- Dentro da OS o tipo se propaga por FK composta, sem trigger: a OS expoe o
-- par (id, tipo) e as filhas referenciam o par inteiro, entao um CHECK local
-- enxerga o tipo sem consultar a tabela pai. ON UPDATE CASCADE porque o tipo
-- da OS pode ser promovido a 'terceiros' enquanto ela esta aberta.
ALTER TABLE os_custo ADD CONSTRAINT fk_custo_os_tipo
    FOREIGN KEY (ordem_servico_id, tipo) REFERENCES ordem_servico (id, tipo)
    ON UPDATE CASCADE;

ALTER TABLE os_encerramento ADD CONSTRAINT fk_encerramento_os_tipo
    FOREIGN KEY (ordem_servico_id, tipo) REFERENCES ordem_servico (id, tipo)
    ON UPDATE CASCADE;

-- Custo hora do tecnico so existe no Maquinario: em 'terceiros' quem
-- trabalhou foi a empresa, e em 'reparo' o servico nao cobra hora tecnica.
-- Dados da nota fiscal, o espelho disso: so em 'terceiros'.
ALTER TABLE os_custo ADD CONSTRAINT ck_custo_por_tipo CHECK (
    (tipo = 'maquinario' OR custo_hora_tecnico IS NULL) AND
    (tipo = 'terceiros'  OR (numero_nota_fiscal IS NULL AND serie_nota_fiscal IS NULL
                             AND descricao_servico_terceiro IS NULL)));

-- --------------------------------------------------------------------------
-- 9. Cardinalidades, unicidade e regras de valor (secao 5.3)
-- --------------------------------------------------------------------------

-- Uma OS nao pode ter duas pausas abertas simultaneamente.
CREATE UNIQUE INDEX uq_pausa_aberta ON os_pausa (ordem_servico_id)
    WHERE retomada_em IS NULL;

-- Uma preventiva nao gera duas solicitacoes pendentes ao mesmo tempo (mas
-- PODE gerar varias ao longo do tempo, a cada ciclo de intervalo_dias).
CREATE UNIQUE INDEX uq_preventiva_pendente ON solicitacao_os (preventiva_id)
    WHERE preventiva_id IS NOT NULL AND status = 'Pendente';

-- Origem da solicitacao: humana ou automatica, nunca as duas.
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_origem CHECK (
    ((origem = 'preventiva')  = (preventiva_id  IS NOT NULL)) AND
    ((origem = 'solicitante') = (solicitante_id IS NOT NULL)));

-- Rejeicao carrega motivo, autor e instante -- os tres juntos ou nenhum
-- (secao 1.4.6).
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_rejeicao CHECK (
    (status = 'Rejeitada') = (motivo_rejeicao IS NOT NULL AND rejeitado_por_id IS NOT NULL
                              AND rejeitada_em IS NOT NULL));

-- Area de atuacao: obrigatoria para tecnico, proibida para os demais perfis.
ALTER TABLE usuario ADD CONSTRAINT ck_usuario_area_tecnico
    CHECK ((perfil = 'tecnico') = (area_tecnico_id IS NOT NULL));

-- --------------------------------------------------------------------------
-- 10. Coerencia do tenant_id denormalizado (secao 5.3, "Coerencia do
-- tenant_id denormalizado")
-- O banco precisa impedir que um registro do tenant A aponte para um
-- registro do tenant B. Resolvido sem trigger: o pai expoe o par
-- (tenant_id, id) (ja declarado em cada CREATE TABLE acima) e a filha
-- referencia o par inteiro. E o que torna o vazamento entre tenants
-- estruturalmente impossivel, em vez de depender de disciplina no codigo.
-- Aplicado a toda relacao entre tabelas que carregam tenant_id -- o mesmo
-- padrao documentado explicitamente para setor->loja e maquina->setor.
-- --------------------------------------------------------------------------

ALTER TABLE setor ADD CONSTRAINT fk_setor_loja
    FOREIGN KEY (tenant_id, loja_id) REFERENCES loja (tenant_id, id);

ALTER TABLE maquina ADD CONSTRAINT fk_maquina_setor
    FOREIGN KEY (tenant_id, setor_id) REFERENCES setor (tenant_id, id);

ALTER TABLE maquina ADD CONSTRAINT fk_maquina_criticidade
    FOREIGN KEY (tenant_id, criticidade_id) REFERENCES nivel_criticidade (tenant_id, id);

ALTER TABLE preventiva ADD CONSTRAINT fk_preventiva_maquina
    FOREIGN KEY (tenant_id, maquina_id) REFERENCES maquina (tenant_id, id);

ALTER TABLE usuario ADD CONSTRAINT fk_usuario_area_tecnico_tenant
    FOREIGN KEY (tenant_id, area_tecnico_id) REFERENCES area_tecnico (tenant_id, id);

ALTER TABLE solicitacao_os ADD CONSTRAINT fk_solicitacao_maquina
    FOREIGN KEY (tenant_id, maquina_id) REFERENCES maquina (tenant_id, id);

ALTER TABLE solicitacao_os ADD CONSTRAINT fk_solicitacao_setor
    FOREIGN KEY (tenant_id, setor_id) REFERENCES setor (tenant_id, id);

ALTER TABLE solicitacao_os ADD CONSTRAINT fk_solicitacao_solicitante
    FOREIGN KEY (tenant_id, solicitante_id) REFERENCES usuario (tenant_id, id);

ALTER TABLE solicitacao_os ADD CONSTRAINT fk_solicitacao_rejeitado_por
    FOREIGN KEY (tenant_id, rejeitado_por_id) REFERENCES usuario (tenant_id, id);

ALTER TABLE solicitacao_os ADD CONSTRAINT fk_solicitacao_preventiva
    FOREIGN KEY (tenant_id, preventiva_id) REFERENCES preventiva (tenant_id, id);

ALTER TABLE ordem_servico ADD CONSTRAINT fk_os_solicitacao
    FOREIGN KEY (tenant_id, solicitacao_id) REFERENCES solicitacao_os (tenant_id, id);

ALTER TABLE ordem_servico ADD CONSTRAINT fk_os_urgencia
    FOREIGN KEY (tenant_id, urgencia_id) REFERENCES nivel_urgencia (tenant_id, id);

ALTER TABLE ordem_servico ADD CONSTRAINT fk_os_tecnico
    FOREIGN KEY (tenant_id, tecnico_id) REFERENCES usuario (tenant_id, id);

ALTER TABLE ordem_servico ADD CONSTRAINT fk_os_aberta_por
    FOREIGN KEY (tenant_id, aberta_por_id) REFERENCES usuario (tenant_id, id);

ALTER TABLE ordem_servico ADD CONSTRAINT fk_os_empresa_terceirizada
    FOREIGN KEY (tenant_id, empresa_terceirizada_id) REFERENCES empresa_terceirizada (tenant_id, id);

ALTER TABLE os_encerramento ADD CONSTRAINT fk_encerramento_tecnico
    FOREIGN KEY (tenant_id, encerrado_por_id) REFERENCES usuario (tenant_id, id);

ALTER TABLE os_custo ADD CONSTRAINT fk_custo_lancado_por
    FOREIGN KEY (tenant_id, lancado_por_id) REFERENCES usuario (tenant_id, id);

-- --------------------------------------------------------------------------
-- 11. Triggers (secao 5.2 e 5.4) -- regras que CHECK nao alcanca
-- --------------------------------------------------------------------------

-- A OS nasce com o tipo da solicitacao e so pode ser promovida a
-- 'terceiros'. CHECK nao ve o valor anterior: isto e uma trigger BEFORE UPDATE.
CREATE FUNCTION fn_os_tipo_promocao() RETURNS trigger AS $$
BEGIN
    IF NEW.tipo <> OLD.tipo AND NEW.tipo <> 'terceiros' THEN
        RAISE EXCEPTION 'tipo da OS só muda para terceiros (era %, tentou %)', OLD.tipo, NEW.tipo;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_os_tipo_promocao
    BEFORE UPDATE OF tipo ON ordem_servico
    FOR EACH ROW EXECUTE FUNCTION fn_os_tipo_promocao();

-- Toda solicitacao tem pelo menos um anexo do tipo foto. A foto do defeito
-- bloqueia o envio no front, mas a linha do anexo so pode existir depois da
-- solicitacao -- entao a verificacao nao cabe num NOT NULL nem num CHECK de
-- linha. Precisa de trigger CONSTRAINT ... DEFERRABLE, avaliada no fim da
-- transacao (secao 5.4, ponto 1).
CREATE FUNCTION fn_check_solicitacao_tem_foto() RETURNS trigger AS $$
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

CREATE CONSTRAINT TRIGGER trg_solicitacao_tem_foto
    AFTER INSERT OR UPDATE ON solicitacao_os
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_check_solicitacao_tem_foto();

-- Usuario com perfil administrador nao tem escopo: para ele, o acesso total
-- ao tenant e justamente a ausencia de linhas em usuario_escopo; um escopo
-- cadastrado seria uma contradicao silenciosa (secao 5.4, ponto 2).
CREATE FUNCTION fn_check_usuario_escopo_nao_admin() RETURNS trigger AS $$
DECLARE
    v_perfil perfil_usuario;
BEGIN
    SELECT perfil INTO v_perfil FROM usuario WHERE id = NEW.usuario_id;
    IF v_perfil = 'administrador' THEN
        RAISE EXCEPTION 'usuário administrador (id %) não pode ter escopo cadastrado', NEW.usuario_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_usuario_escopo_nao_admin
    AFTER INSERT OR UPDATE ON usuario_escopo
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_check_usuario_escopo_nao_admin();

-- Mesma regra, na direcao oposta: um usuario nao pode virar administrador
-- enquanto ainda tiver escopo cadastrado.
CREATE FUNCTION fn_check_usuario_vira_admin_sem_escopo() RETURNS trigger AS $$
BEGIN
    IF NEW.perfil = 'administrador' AND EXISTS (
        SELECT 1 FROM usuario_escopo WHERE usuario_id = NEW.id
    ) THEN
        RAISE EXCEPTION 'usuário % não pode virar administrador com escopo já cadastrado', NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_usuario_admin_sem_escopo
    AFTER UPDATE OF perfil ON usuario
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_check_usuario_vira_admin_sem_escopo();

-- --------------------------------------------------------------------------
-- 12. Indices (secao 5.5)
-- Alem das FKs, cobertas em boa parte pelas UNIQUE com tenant_id como
-- coluna lider acima: (tecnico_id, status) em ordem_servico -- a consulta
-- exata do Painel do Tecnico -- e (tenant_id, status) em solicitacao_os,
-- que e a aba Solicitacoes do Gestor.
-- --------------------------------------------------------------------------

CREATE INDEX idx_solicitacao_tenant_status ON solicitacao_os (tenant_id, status);
CREATE INDEX idx_solicitacao_maquina ON solicitacao_os (maquina_id);
CREATE INDEX idx_solicitacao_setor ON solicitacao_os (setor_id);
CREATE INDEX idx_solicitacao_solicitante ON solicitacao_os (solicitante_id);
CREATE INDEX idx_solicitacao_rejeitado_por ON solicitacao_os (rejeitado_por_id);
CREATE INDEX idx_anexo_solicitacao ON solicitacao_anexo (solicitacao_id);

CREATE INDEX idx_os_tecnico_status ON ordem_servico (tecnico_id, status);
CREATE INDEX idx_os_urgencia ON ordem_servico (urgencia_id);
CREATE INDEX idx_os_aberta_por ON ordem_servico (aberta_por_id);
CREATE INDEX idx_os_empresa_terceirizada ON ordem_servico (empresa_terceirizada_id);

CREATE INDEX idx_pausa_ordem_servico ON os_pausa (ordem_servico_id);
CREATE INDEX idx_encerramento_tenant ON os_encerramento (tenant_id);
CREATE INDEX idx_encerramento_encerrado_por ON os_encerramento (encerrado_por_id);
CREATE INDEX idx_custo_tenant ON os_custo (tenant_id);
CREATE INDEX idx_custo_lancado_por ON os_custo (lancado_por_id);

CREATE INDEX idx_maquina_setor ON maquina (setor_id);
CREATE INDEX idx_maquina_criticidade ON maquina (criticidade_id);

CREATE INDEX idx_usuario_area_tecnico ON usuario (area_tecnico_id);
CREATE INDEX idx_usuario_escopo_usuario ON usuario_escopo (usuario_id);
CREATE INDEX idx_usuario_escopo_loja ON usuario_escopo (loja_id);
CREATE INDEX idx_usuario_escopo_setor_setor ON usuario_escopo_setor (setor_id);

-- --------------------------------------------------------------------------
-- 13. O que o banco calcula, em vez de guardar (secao 4)
-- Tres numeros aparecem em tela e nenhum deles e coluna.
-- --------------------------------------------------------------------------

-- Horas de uma OS encerrada: nenhuma das duas e coluna. horas_parada so
-- existe quando afeta_producao e verdadeira -- nas demais a maquina seguiu
-- operando e nao ha parada para medir (secao 1.4.5).
CREATE VIEW vw_os_horas AS
SELECT os.id AS ordem_servico_id,
       CASE WHEN os.afeta_producao
            THEN EXTRACT(EPOCH FROM (e.data_fim - os.aberta_em)) / 3600
       END AS horas_parada,
       EXTRACT(EPOCH FROM (e.data_fim - os.iniciada_em)) / 3600
         - COALESCE((SELECT SUM(EXTRACT(EPOCH FROM (p.retomada_em - p.pausada_em))) / 3600
                       FROM os_pausa p
                      WHERE p.ordem_servico_id = os.id
                        AND p.pausada_em >= os.iniciada_em), 0) AS horas_trabalhadas
  FROM ordem_servico os
  JOIN os_encerramento e ON e.ordem_servico_id = os.id;

-- "OS Finalizada" do Gestor, do Tecnico e do Administrador: estado derivado,
-- nao um status (secao 3.4).
CREATE VIEW vw_os_finalizada AS
SELECT os.*,
       COALESCE(c.custo_hora_tecnico, 0) + c.custo_manutencao AS custo_total
  FROM ordem_servico os
  JOIN os_custo c ON c.ordem_servico_id = os.id
 WHERE os.status = 'Concluída';

-- Custo ainda nao lancado. Desde a revisao 4 e caso raro (o Tecnico informa
-- os valores no encerramento), mas continua sendo a resposta certa para
-- "que OS esta sem custo?". A tela "Custos Pendentes" do Administrador NAO
-- usa esta view -- ela lista toda OS Concluida, porque virou fila de
-- conferencia contra a nota fiscal, nao de digitacao.
CREATE VIEW vw_os_custo_sem_lancamento AS
SELECT os.*
  FROM ordem_servico os
 WHERE os.status = 'Concluída'
   AND NOT EXISTS (SELECT 1 FROM os_custo c WHERE c.ordem_servico_id = os.id);
