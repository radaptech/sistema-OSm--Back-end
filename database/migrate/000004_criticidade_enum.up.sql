-- ==========================================================================
-- nivel_criticidade: tabela por tenant -> ENUM.
--
-- A tabela existia para permitir niveis customizados por tenant, mas nada
-- customiza: o front tipa niveis_criticidade como uma tupla fixa
-- ('Baixa','Média','Alta' em front-end/src/tipos/maquina.ts) e nao ha tela de
-- cadastro/edicao de nivel em lugar nenhum. Na pratica sobrava uma tabela que
-- nenhuma migration/seed populava -- a mesma lacuna de area_tecnico -- e que
-- deixaria o cadastro de maquina falhando ate alguem inserir as tres linhas
-- em cada tenant novo.
--
-- Como ENUM o valor nasce junto do schema, some a coluna criticidade_id, some
-- a FK composta e some a resolucao nome->id do service. Mesma escolha ja
-- feita para tipo_defeito ('Predial','Corretiva'), que tambem e lista fixa
-- com o rotulo exato do front.
--
-- nivel_urgencia continua tabela de proposito: nao esta no caminho deste CRUD
-- e ordem_servico ainda nao tem escrita nenhuma.
--
-- ⚠️ Tabela e tipo dividem o mesmo namespace no Postgres, entao a tabela
-- precisa sair antes de o tipo homonimo entrar -- dai a ordem abaixo.
-- ==========================================================================

ALTER TABLE maquina DROP CONSTRAINT fk_maquina_criticidade;

-- Indice era do lado filho da FK. Sem FK e com tres valores possiveis, um
-- indice em criticidade nao paga o proprio custo -- nao e recriado.
DROP INDEX IF EXISTS idx_maquina_criticidade;

ALTER TABLE maquina DROP COLUMN criticidade_id;

DROP TABLE nivel_criticidade;

CREATE TYPE nivel_criticidade AS ENUM ('Baixa','Média','Alta');

-- NOT NULL: NovaMaquinaPayload exige criticidade (z.enum, sem optional) e a
-- coluna nova entra em tabela comprovadamente vazia -- nao existe nenhum
-- caminho de INSERT em maquina no codigo ainda.
ALTER TABLE maquina ADD COLUMN criticidade nivel_criticidade NOT NULL;
