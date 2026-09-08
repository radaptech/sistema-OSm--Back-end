-- ==========================================================================
-- Custo deixa de ser UM par de valores e vira LISTA; nota fiscal deixa de ser
-- UMA e vira lista tambem.
--
-- O caso que quebrou o modelo antigo e trivial e comum: uma serra fita em que
-- o rolamento E a fita quebraram. Sao duas tarefas, cada uma com sua peca e
-- sua mao de obra, e desde a 000010 podem ser duas compras em lojas
-- diferentes -- logo, duas notas. Com custo_manutencao sendo uma coluna
-- escalar, o Tecnico era obrigado a somar as duas pecas de cabeca e digitar o
-- total; o Administrador abria Custos Pendentes, via um numero unico e nao
-- tinha contra o que conferir. E com numero_nota_fiscal/serie_nota_fiscal
-- sendo um par escalar, a segunda nota simplesmente nao tinha onde entrar.
--
-- Duas tabelas filhas, uma para cada lista, SEM vinculo entre elas:
--
--   os_custo_item   -- uma TAREFA lancada pelo Tecnico no encerramento,
--                      com o custo de material E o de mao de obra dela
--   os_nota_fiscal  -- um documento registrado pelo Administrador
--
-- Nao ha FK ligando item a nota, e isso e decisao, nao lacuna. Uma nota so
-- pode cobrir as duas pecas (compra unica) e uma peca pode nao ter nota
-- nenhuma (estoque proprio), entao qualquer amarracao 1:1 estaria errada
-- metade das vezes. Se um dia a conciliacao item-a-item for pedida, ela entra
-- como uma coluna nullable os_custo_item.nota_fiscal_id, sem mexer no que
-- existe aqui.
--
-- ⚠️ os_custo.custo_manutencao e custo_hora_tecnico CONTINUAM existindo e
-- passam a ser a SOMA dos itens, escrita pelo service na mesma transacao.
-- Isso contraria a secao 3.2 da modelagem ("e um total que se calcula"), e a
-- excecao e consciente: as duas colunas sao lidas por vw_os_finalizada, por
-- ListarOrdensServico, por ListarHistoricoOsDaMaquina (indicadores) e por
-- meia duzia de telas. Deriva-las na leitura significaria reescrever duas
-- views, duas queries quentes e os overrides do sqlc para nao mudar UMA
-- pixel na tela. A trava contra divergencia e que o servidor NUNCA aceita o
-- total do cliente: ele soma os itens que acabou de gravar. Ha teste
-- conferindo a soma depois do commit.
--
-- ⚠️ tem_nota_fiscal SOBREVIVE, e nao virou "a lista esta vazia". Os dois
-- estados sao diferentes e a 000011 existe justamente por isso: "esta OS nao
-- gerou nota" e "gerou e ninguem preencheu ainda" precisam ser distinguiveis,
-- e a fila de conferencia do Administrador e feita do segundo caso. Lista
-- vazia sozinha nao separa os dois.
-- ==========================================================================

-- --------------------------------------------------------------------------
-- 1. os_custo_item -- uma linha por TAREFA, com os dois valores dela
-- --------------------------------------------------------------------------
-- A linha e uma TAREFA da OS, nao um valor solto: "trocar o rolamento" custa
-- a peca E a mao de obra de instala-la. Por isso as duas colunas de dinheiro
-- convivem na mesma linha, em vez de uma coluna `valor` com um ENUM dizendo
-- de que tipo ela e.
--
-- A alternativa (uma linha por VALOR, com categoria) foi construida primeiro e
-- descartada na tela: para lancar duas pecas de uma OS de maquinario o Tecnico
-- tinha que criar uma terceira linha so para a hora tecnica, escolhendo a
-- categoria num select -- e o formulario o bloqueava ate ele fazer isso. Duas
-- pecas nao sao duas mao de obra; duas TAREFAS e que sao.
--
-- Pendura em ordem_servico, e nao em os_custo, para poder carregar o par
-- (ordem_servico_id, tipo) da FK composta -- mesmo padrao de os_custo e
-- os_encerramento (secao 3.5). E o que deixa ck_custo_item_hora_tecnico ser
-- um CHECK local em vez de um trigger: sem o tipo denormalizado na propria
-- linha, um CHECK nao teria como enxergar a tabela pai. uq_custo_os ja
-- garante um os_custo por OS, entao nao ha ambiguidade sobre a qual custo o
-- item pertence.
CREATE TABLE os_custo_item (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id           bigint NOT NULL REFERENCES empresa (id),
    ordem_servico_id    bigint NOT NULL,
    tipo                tipo_os NOT NULL,
    descricao           text NOT NULL,
    custo_manutencao    numeric(12,2) NOT NULL,
    custo_hora_tecnico  numeric(12,2),
    criado_em           timestamptz NOT NULL DEFAULT now(),

    -- Espelha ck_custo_nao_negativo. Zero e valor legitimo em todo o fluxo
    -- (peca em garantia, servico sem material), por isso >= e nao >.
    CONSTRAINT ck_custo_item_valores CHECK (
        custo_manutencao >= 0 AND COALESCE(custo_hora_tecnico, 0) >= 0),

    -- Uma linha de dinheiro sem nome nao e conferivel contra nota nenhuma --
    -- o Administrador leria "R$ 180,00" sem saber do que se trata. btrim
    -- porque `binding:"required"` do Go passa numa string de espacos.
    CONSTRAINT ck_custo_item_descricao CHECK (btrim(descricao) <> ''),

    -- A metade de ck_custo_por_tipo que fala de hora tecnica, aplicada agora
    -- linha a linha: em 'terceiros' quem trabalhou foi a empresa externa e em
    -- 'reparo' o servico nao cobra hora tecnica. NULL (e nao zero) e o que a
    -- coluna guarda nesses dois tipos, igual a os_custo. A outra metade do
    -- CHECK antigo (descricao_servico_terceiro) nao tem equivalente aqui e
    -- continua em os_custo, intacta.
    CONSTRAINT ck_custo_item_hora_tecnico CHECK (
        tipo = 'maquinario' OR custo_hora_tecnico IS NULL)
);

ALTER TABLE os_custo_item ADD CONSTRAINT fk_custo_item_os_tipo
    FOREIGN KEY (ordem_servico_id, tipo) REFERENCES ordem_servico (id, tipo)
    ON UPDATE CASCADE;

-- Coerencia de tenant pelo par (secao 5.3). os_custo nao tem esta FK, mas
-- devia: sem ela nada no BANCO impede um item do tenant A pendurado numa OS
-- do tenant B, so a disciplina do WHERE. Custa uma linha, entao entra.
ALTER TABLE os_custo_item ADD CONSTRAINT fk_custo_item_os_tenant
    FOREIGN KEY (tenant_id, ordem_servico_id) REFERENCES ordem_servico (tenant_id, id);

-- --------------------------------------------------------------------------
-- 2. os_nota_fiscal -- uma linha por documento
-- --------------------------------------------------------------------------
-- Numero e serie, e mais nada. Nao ha coluna de VALOR de propósito: o valor
-- ja esta nos itens, e uma segunda fonte para a mesma grandeza so cria a
-- pergunta "qual dos dois esta certo" quando as duas discordarem. A nota aqui
-- e o DOCUMENTO que embasa o custo, exatamente o papel que
-- numero_nota_fiscal/serie_nota_fiscal cumpriam antes.
CREATE TABLE os_nota_fiscal (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id         bigint NOT NULL REFERENCES empresa (id),
    ordem_servico_id  bigint NOT NULL,
    numero            text NOT NULL,
    serie             text,
    criado_em         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_nota_fiscal_numero CHECK (btrim(numero) <> ''),

    -- NULLS NOT DISTINCT (Postgres 15+) porque serie e opcional e, no UNIQUE
    -- comum, NULL nunca colide com NULL -- ou seja, a mesma nota sem serie
    -- entraria quantas vezes o Administrador clicasse em salvar. Com ele, o
    -- clique duplo e a re-submissao do formulario batem na constraint em vez
    -- de duplicar o documento.
    CONSTRAINT uq_nota_fiscal_os UNIQUE NULLS NOT DISTINCT (ordem_servico_id, numero, serie)
);

ALTER TABLE os_nota_fiscal ADD CONSTRAINT fk_nota_fiscal_os_tenant
    FOREIGN KEY (tenant_id, ordem_servico_id) REFERENCES ordem_servico (tenant_id, id);

-- --------------------------------------------------------------------------
-- 3. Indices das FKs (secao 5.5)
-- --------------------------------------------------------------------------
-- ordem_servico_id em cada uma e a leitura em lote das duas listas
-- (ObterItensDeCustoDasOrdensServico / ObterNotasFiscaisDasOrdensServico,
-- molde de ObterPausasDasOrdensServico). tenant_id acompanha o padrao das
-- outras filhas da OS.
CREATE INDEX idx_custo_item_ordem_servico ON os_custo_item (ordem_servico_id);
CREATE INDEX idx_custo_item_tenant ON os_custo_item (tenant_id);
CREATE INDEX idx_nota_fiscal_ordem_servico ON os_nota_fiscal (ordem_servico_id);
CREATE INDEX idx_nota_fiscal_tenant ON os_nota_fiscal (tenant_id);

-- --------------------------------------------------------------------------
-- 4. Backfill -- antes de dropar as colunas velhas
-- --------------------------------------------------------------------------
-- Toda OS ja encerrada vira uma lista de um item so, para que a tela de
-- edicao do Administrador nao abra vazia e zere um custo que existe. Sem
-- isto, salvar uma OS antiga em Custos Pendentes gravaria a soma de uma lista
-- vazia, ou seja, apagaria o valor.
INSERT INTO os_custo_item (
    tenant_id, ordem_servico_id, tipo, descricao, custo_manutencao, custo_hora_tecnico
)
SELECT tenant_id, ordem_servico_id, tipo, 'Servico executado',
       custo_manutencao, custo_hora_tecnico
  FROM os_custo;

-- A nota so existe onde o numero existe. ck_custo_nota_fiscal ja garantia que
-- numero preenchido implica tem_nota_fiscal, entao o trigger criado logo
-- abaixo nao rejeitaria nenhuma destas linhas -- mas ele so e criado DEPOIS
-- deste INSERT de qualquer forma, para o backfill nao depender da ordem.
INSERT INTO os_nota_fiscal (tenant_id, ordem_servico_id, numero, serie)
SELECT tenant_id, ordem_servico_id, numero_nota_fiscal, serie_nota_fiscal
  FROM os_custo
 WHERE numero_nota_fiscal IS NOT NULL;

-- --------------------------------------------------------------------------
-- 5. O que substitui ck_custo_nota_fiscal
-- --------------------------------------------------------------------------
-- A regra nao mudou: nota so entra em OS declarada como tendo nota. O que
-- mudou e que ela deixou de caber num CHECK -- a declaracao esta em os_custo
-- e a nota em os_nota_fiscal, e CHECK nao enxerga outra tabela.
--
-- Trigger comum, nao DEFERRABLE (diferente de fn_check_solicitacao_tem_foto):
-- ali a regra e "o filho TEM que existir", que so da para avaliar no fim da
-- transacao; aqui e "o filho NAO pode existir", e o pai ja esta gravado
-- quando o filho chega. BEFORE porque nao ha nada a fazer depois de decidir
-- que a linha nao entra.
--
-- So a direcao "declarou que nao teve" e travada, igual a 000011. O contrario
-- -- declarou que teve e a lista ainda esta vazia -- continua sendo estado
-- legitimo e frequente: e a propria fila de conferencia do Administrador.
--
-- A direcao inversa (desmarcar a declaracao com nota ja cadastrada) NAO tem
-- trigger e e responsabilidade do service, que apaga as notas na mesma
-- escrita -- mesmo desenho do front, que ja limpava numero e serie ao
-- desmarcar. Um trigger em os_custo aqui teria que decidir sozinho entre
-- apagar as notas do usuario e recusar a edicao, e nenhuma das duas e decisao
-- de banco.
CREATE OR REPLACE FUNCTION fn_check_nota_fiscal_declarada() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM os_custo c
                    WHERE c.ordem_servico_id = NEW.ordem_servico_id
                      AND c.tem_nota_fiscal) THEN
        RAISE EXCEPTION 'nota fiscal exige que a ordem de servico esteja declarada como tendo nota fiscal';
    END IF;
    RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER trg_nota_fiscal_declarada
    BEFORE INSERT OR UPDATE ON os_nota_fiscal
    FOR EACH ROW EXECUTE FUNCTION fn_check_nota_fiscal_declarada();

-- --------------------------------------------------------------------------
-- 6. As colunas velhas saem
-- --------------------------------------------------------------------------
-- Manter as duas ao lado de os_nota_fiscal daria DOIS lugares para gravar o
-- mesmo fato, e nada garantiria qual deles a tela leu. ck_custo_nota_fiscal
-- cai junto porque as colunas que ele cita deixam de existir; quem passa a
-- guardar a regra e o trigger acima.
--
-- vw_os_finalizada e vw_os_custo_sem_lancamento nao bloqueiam o DROP: as duas
-- fazem SELECT os.* sobre ordem_servico e citam de os_custo apenas
-- custo_hora_tecnico e custo_manutencao, que ficam.
ALTER TABLE os_custo DROP CONSTRAINT ck_custo_nota_fiscal;
ALTER TABLE os_custo DROP COLUMN numero_nota_fiscal;
ALTER TABLE os_custo DROP COLUMN serie_nota_fiscal;
