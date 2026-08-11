# Modelagem do Banco de Dados — Revisão 3

Documento gerado a partir da revisão do código do front-end (`/src/tipos`, `/src/servicos`,
`/src/paginas`) comparado ao modelo da revisão anterior.

- **Revisão 1** (03/08/2026): comparou o DER original com a interface e reescreveu o modelo — 21 entidades.
- **Revisão 2** (08/08/2026): incorpora os três tipos de OS, as empresas
  terceirizadas, o perfil Administrador e a evidência visual do defeito — **20 tabelas + 7 tipos ENUM**.
- **Revisão 3** (10/08/2026, este documento): dois ajustes puxados pelo front, sem mudança de
  forma do modelo — **PKs/FKs de negócio trocam de `uuid` para `bigint`** (seção 3.12) e o
  **front para de tratar setor como união estática** e passa a referenciá-lo por id em toda
  parte (`setorId`/`setorNome`, no mesmo padrão de `lojaId`/`lojaNome`) — o banco já modelava
  `setor` como tabela desde a revisão 1; era só o front que ainda não confiava nisso (ponto 3
  da seção 6, agora resolvido).

O diagrama em si está em [`der-banco-dados.mmd`](./der-banco-dados.mmd) (Mermaid, pronto para colar
em <https://mermaid.live>), com [`.svg`](./der-banco-dados.svg) e [`.png`](./der-banco-dados.png)
gerados a partir dele.

**Para regerar as imagens após editar o `.mmd`:**

```bash
npm run docs:der
```

> ⚠️ **Armadilha do Mermaid:** uma linha de comentário `%%` **sem conteúdo** quebra o parser do
> `erDiagram` (testado na 11.16.0) com um erro que aponta para a linha errada. Use `%% ---` como
> separador dentro de blocos de comentário, nunca `%%` sozinho.

---

## 1. O que mudou em relação à revisão 1

### 1.1 Mudanças estruturais (alto impacto)

| # | Tema | Revisão 1 | O que a interface exige hoje |
|---|---|---|---|
| 1 | **Tipo de OS** | Uma única natureza de OS: máquina cadastrada, técnico do quadro, urgência. Todas as colunas obrigatórias para todos. | `tipo_os` (`maquinario` / `terceiros` / `reparo`) em **`solicitacao_os` e `ordem_servico`**. O tipo decide quais colunas são obrigatórias e quais são proibidas. Ver `/src/tipos/ordemServico.ts`, `servicoReparos.ts`, `servicoOSTerceiros.ts`. |
| 2 | **Empresa terceirizada** | Não existe. Toda OS aponta para um `tecnico_id`. | Nova tabela `empresa_terceirizada` (cadastro do Administrador, sem vínculo de loja) e FK na OS **mutuamente exclusiva** com `tecnico_id`. Ver `/src/tipos/empresaTerceirizada.ts`, `ModalAprovarOSTerceiros.tsx`. |
| 3 | **Pequeno Reparo sem máquina** | `solicitacao_os.maquina_id` `NOT NULL`. | O Solicitante digita o item na hora ("Lâmpada de LED"): `maquina_id` vira nullable e entra `item_descricao`, amarrados por CHECK. Ver `/src/tipos/reparo.ts`, `NovaSolicitacaoReparo.tsx`. |
| 4 | **4º perfil: Administrador** | Três perfis, todos delimitados por loja/setor. | `administrador` enxerga o tenant inteiro e é dono de todos os cadastros. Modelado como **zero linhas em `usuario_escopo`** — a ausência de escopo é o que significa acesso total. Ver `/src/tipos/autenticacao.ts`, `RotaProtegida.tsx`. |
| 5 | **Identificação da máquina** | `maquina.tag` com `UNIQUE (tenant_id, tag)`. | `numero_patrimonio` (do cliente, sempre existe) e `numero_serie` (do fabricante, pode faltar). Duas identidades de origens diferentes, com regras de unicidade diferentes. Ver `/src/tipos/maquina.ts`, `CadastrarMaquina.tsx`. |
| 6 | **Evidência visual do defeito** | Nenhum lugar para arquivo — a solicitação era só texto. | Foto **obrigatória** (bloqueia o envio) e vídeo opcional, capturados pela câmera do celular. Nova tabela `solicitacao_anexo`. Ver `UploadFoto.tsx`, `UploadVideo.tsx`. |
| 7 | **Custo** | Uma coluna `custo` em `os_encerramento`, preenchida pelo Técnico. | `custo_hora_tecnico` + `custo_manutencao` na nova tabela `os_custo`, com `lancado_por_id` / `lancado_em` — o Técnico informa, o Administrador revisa em Custos Pendentes. Ver `ModalEncerrarOrdemServico.tsx`, `AdministradorCustosPendentes.tsx`. |
| 8 | **Encerramento não é universal** | Toda OS concluída tinha um `os_encerramento` 1:1. | OS Terceiros **nasce** `Concluída` na aprovação do Gestor e nunca passa por Técnico — não há defeito constatado nem causa raiz. `os_encerramento` só existe para `maquinario` e `reparo`. Ver `servicoSolicitacoes.aprovarTerceiros`. |

### 1.2 O que se manteve da revisão 1

- **`empresa` é o tenant** — decisão mantida integralmente (seção 3.1).
- **Hierarquia `Empresa > Loja > Setor`** e o agrupamento por loja no Painel do Gestor.
- **`usuario_escopo` + `usuario_escopo_setor`** — o mesmo modelo de escopo, agora servindo quatro perfis.
- **`os_pausa` como histórico**, e não campo sobrescrito — decisão que se provou ainda mais acertada
  (seção 3.3): é dela que sai o cálculo de horas trabalhadas.
- **`maquina` referencia apenas `setor_id`**; a loja vem por join.
- **`data_inicio` em um lugar só** (`ordem_servico.iniciada_em`).
- **`tenant_id` denormalizado** nas tabelas profundas, com coerência garantida por FK composta.

### 1.3 Contagem

| | Revisão 1 | Revisão 2 |
|---|---|---|
| Tabelas de negócio | 13 | 16 |
| Tabelas de domínio (lookup) | 8 | 4 |
| **Total de tabelas** | **21** | **20** |
| Tipos `ENUM` nativos | 0 | 7 |

Tabelas novas: `empresa_terceirizada`, `solicitacao_anexo`, `os_custo`.
Tabelas que viraram `ENUM`: `perfil_usuario`, `status_solicitacao`, `status_os`, `marcador_impacto`.

---

## 2. Decisões travadas na revisão 2

Quatro pontos que mudavam o desenho das tabelas e não davam para resolver lendo o código.

### 2.1 Pequeno Reparo: `maquina_id` nullable + `item_descricao`

Uma única `solicitacao_os` serve os três tipos. O CHECK garante coerência nos dois sentidos:
`tipo = 'reparo'` exige o texto livre e proíbe a FK; os demais tipos exigem a FK e proíbem o texto.
Não existe estado intermediário válido.

**Alternativas descartadas:** cadastrar cada lâmpada como "ativo" contraria a razão de existir do
Pequeno Reparo (não exigir cadastro prévio); e uma tabela filha 1:1 transformaria toda listagem do
Gestor — que mistura os três tipos — em JOIN condicional.

### 2.2 Anexos: tabela, não colunas

Duas colunas (`foto_url`, `video_url`) travariam o modelo em exatamente uma foto e um vídeo, e a
terceira foto viraria `ALTER TABLE`. Com `solicitacao_anexo`, o mesmo desenho aguenta N arquivos e
já tem onde pendurar o que o storage real vai exigir (chave do objeto, mime type, tamanho).

**Preço:** "pelo menos uma foto" deixa de ser um `NOT NULL` e passa a exigir trigger deferida
(seção 5.4).

### 2.3 Custo separado da execução

`os_encerramento` e `os_custo` são dois fatos distintos, de atores distintos, em momentos distintos:
o Técnico registra **o que ele fez**; o Administrador registra **quanto custou**.

Separar deixa `os_custo` servir os três tipos sem coluna nullable por tipo, e mantém
`os_encerramento` coerente — se a linha existe, todos os campos dela estão preenchidos. Ganho extra:
`lancado_por_id` / `lancado_em` respondem quem mexeu no valor, pergunta que sempre aparece quando o
número vai para um relatório.

Para OS Terceiros, a "Descrição do Serviço Realizado" que o Administrador digita vive em
`os_custo.descricao_servico` — mesmo ator, mesmo momento, mesmo formulário. Isso substitui a gambiarra
atual do front, que reaproveita o campo `solucao` da OS para esse texto.

### 2.4 `ENUM` no que é regra de código, tabela no que o cliente cadastra

**Viraram `ENUM` nativo:** `perfil_usuario`, `tipo_os`, `origem_solicitacao`, `status_solicitacao`,
`status_os`, `marcador_impacto`, `tipo_anexo`.

Nenhum deles ganha um valor novo sem alguém escrever código que o trate — então virar `INSERT` seria
uma flexibilidade falsa, que só serve para inserir valor que a aplicação não entende.

**Continuam tabela:** `tipo_defeito`, `area_tecnico`, `nivel_criticidade`, `nivel_urgencia`. São
vocabulário do cliente: um supermercado pode querer "Automação" onde outro quer "Ar-condicionado".

**Resultado:** nove listas de domínio caem para quatro tabelas, some um JOIN de toda listagem quente,
e uma limitação registrada na revisão 1 desaparece — o CHECK de "área de atuação é obrigatória para
técnico" precisava de subquery (que `CHECK` não aceita) e agora é uma expressão simples:

```sql
ALTER TABLE usuario ADD CONSTRAINT ck_usuario_area_tecnico
  CHECK ((perfil = 'tecnico') = (area_tecnico_id IS NOT NULL));
```

**Contrapartida assumida:** um 4º marcador de impacto passa a ser `ALTER TYPE` + deploy, não `INSERT`.
Aceitável, porque um marcador novo exige código no front para exibi-lo de qualquer forma.

---

## 3. Boas práticas aplicadas na modelagem

### 3.1 `empresa` **é** o tenant

Não existe tabela `tenant` separada. **`empresa` é a raiz multi-tenant**: um cliente SaaS = uma
empresa = um subdomínio. A coluna `empresa.subdominio` (UNIQUE) é o que o `api.ts` extrai de
`window.location.hostname` e envia no header `X-tenant-ID`.

Todas as demais tabelas de negócio carregam a FK **`tenant_id` → `empresa.id`**. O nome é `tenant_id`
(e não `empresa_id`) de propósito: deixa explícito que aquilo é o discriminador de isolamento, que é
o papel que a coluna cumpre nas policies de RLS.

> Consequência prática: `loja` tem **apenas** `tenant_id`. Não existe `loja.empresa_id`, porque seria
> a mesma coluna com dois nomes.

Nas tabelas profundas o `tenant_id` é **tecnicamente derivável** por join. É mantido denormalizado de
propósito: permite que a policy de RLS filtre sem join, e transforma o isolamento entre clientes em
algo que o banco garante em cada tabela — não em algo que depende do `WHERE` correto na aplicação.
O preço é garantir a coerência (seção 5.3).

### 3.2 Horas trabalhadas não é coluna

O front guarda `horasTrabalhadasAcumuladas` e `sessaoAtualInicio` porque não tem banco. No modelo,
**nenhum dos dois existe**: `os_pausa` já registra cada intervalo parado, com início e fim, e as duas
grandezas saem daí (seção 4).

É um total que se calcula. Guardado, ele passa a poder discordar das pausas que o originaram — e aí
não se sabe qual dos dois está certo.

### 3.3 Pausa é histórico, não campo sobrescrito

`motivoPausa` + `statusAntesDaPausa` como colunas da OS perdem o histórico: se o técnico pausa três
vezes, só a última sobrevive.

`os_pausa` (`motivo`, `pausada_em`, `retomada_em`, `status_anterior`) preserva as três — e é o que
torna possível calcular MTTR e horas de parada corretamente no Painel de Indicadores (item 8 do
CLAUDE.md), hoje gerado por mock.

> O campo `dataPausa`, adicionado ao front nesta mesma leva, já era previsto aqui como
> `os_pausa.pausada_em` desde a revisão 1.

### 3.4 "Finalizada" não é um status

A OS finalizada é `status = 'Concluída'` **e** custo já lançado — regra que aparece em três telas
(aba do Gestor, aba do Técnico e tela do Administrador). Isso é uma **view**, não um sexto valor em
`status_os`.

Como valor de status, dois lugares passariam a afirmar a mesma coisa, e o `UPDATE` que esquecesse um
deles criaria uma OS finalizada sem custo.

### 3.5 `tipo` repetido é denormalização deliberada

O tipo desce da solicitação para a OS e dela para `os_custo` e `os_encerramento`. É a única
denormalização deliberada do modelo, e cada salto é travado por FK composta (seção 5.2).

Sem ela, "terceiros não tem técnico" e "terceiros não tem encerramento" virariam trigger — um `CHECK`
não enxerga a tabela pai. A FK composta faz o próprio PostgreSQL garantir que a cópia nunca diverge.

### 3.6 Patrimônio e série têm regras diferentes

`numero_patrimonio` é obrigatório e único no tenant. `numero_serie` ganha índice único **parcial**,
válido só quando preenchido — a série vem do fabricante e falta em equipamento antigo ou improvisado.

### 3.7 Técnico continua sendo `usuario`

O front mantém `USUARIOS_MOCK` e `TECNICOS_MOCK` separados. No banco é uma tabela só, com
`perfil = 'tecnico'` e `area_tecnico_id` preenchido.

A separação é conveniência do mock, não regra de negócio. Duplicar login e e-mail em duas tabelas
abriria a porta para o mesmo e-mail existir duas vezes no mesmo tenant.

> Consequência: o roteamento interno de `servicoUsuarios.criar` para `servicoTecnicos` (CLAUDE.md
> item 7) some na integração real — vira um `INSERT` só, com `perfil` diferente.

### 3.8 Escopo de acesso unificado para os quatro perfis

`usuario_escopo` (usuário + loja + `acesso_total_setores`) + `usuario_escopo_setor` cobre todos com a
mesma estrutura:

- **Solicitante:** 1 escopo, `acesso_total = false`, exatamente 1 setor.
- **Técnico:** N escopos com `acesso_total = true` (ele enxerga por designação na OS, não por setor).
- **Gestor:** N escopos, cada um com `acesso_total` ou uma lista de setores.
- **Administrador:** **nenhum escopo** — a ausência de linhas é o que significa "todo o tenant".

> Este modelo já resolve a limitação registrada no CLAUDE.md item 7 — "um Gestor com acesso parcial
> numa loja e total noutra exigiria editar o usuário depois". No banco isso é natural; só o
> formulário é que ainda não expõe.

### 3.9 Empresa terceirizada é do tenant, não da loja

Sem `loja_id`: o cadastro pertence à empresa cliente, e a mesma assistência técnica atende quantas
lojas forem necessárias. Vincular à loja obrigaria a cadastrar o mesmo fornecedor três vezes, com
três telefones que envelhecem em ritmos diferentes.

### 3.10 Anexo aponta para chave, não para URL

`solicitacao_anexo.url` guarda a chave do objeto no storage, não um endereço assinado. A URL de
acesso é gerada na leitura, porque link assinado expira — persistido, o banco acumula endereços que
param de funcionar sem nada indicar que quebraram.

### 3.11 Impactos como N:N

`solicitacao_impacto` continua sendo a associativa (uma linha por marcador por solicitação), casando
com o array já usado no front (`impactos: MarcadorImpacto[]`). O que mudou é que o marcador deixou de
ser FK para uma tabela de domínio e passou a ser coluna `ENUM` (seção 2.4).

### 3.12 Convenções gerais

- **Nomenclatura:** `snake_case`, tabelas no singular, em Português-BR (consistente com a regra de
  idioma do projeto).
- **PKs (revisto na revisão 3):** `bigint` (identity/serial) em **todas** as tabelas — negócio e
  domínio, inclusive `tenant_id`. A revisão 2 usava `uuid` nas entidades de negócio para não deixar
  o id enumerável numa API pública; a revisão 3 reverte isso porque o front inteiro (`/src/tipos`)
  já trata `id` como `number` — não existe uma única tela lidando com id como string — e o
  `tenant_id` isolando os dados por cliente já resolve o vazamento entre tenants (seção 5.3) mesmo
  com id sequencial. Manter `uuid` só para essa propriedade teórica exigiria reescrever todo o front
  sem ganho prático hoje; fica registrado como possível endurecimento futuro, não como decisão a
  refazer sem necessidade.
- **Datas:** `timestamptz` (nunca `timestamp` sem fuso); `date` só onde não há hora
  (`preventiva.proxima_data`).
- **Dinheiro:** `numeric(12,2)` — nunca `float`/`real`, que acumula erro de centavos.
- **Senha:** `senha_hash` (bcrypt/argon2). O DER original tinha `login.senha`, o que sugeria texto puro.
- **`ENUM`:** valores escritos exatamente como o front os envia, acentos inclusive
  (`'Em Andamento'`, `'Concluída'`, `'Afeta Produção'`), para não exigir tradução na borda.
- **Auditoria:** `criado_em` em todas as tabelas; `atualizado_em` via trigger onde houver edição.
- **Soft delete:** flags `ativo`/`ativa` para não perder histórico de OS.

---

## 4. O que o banco calcula, em vez de guardar

Três números aparecem em tela e nenhum deles é coluna.

| Grandeza | Fórmula | Onde aparece |
|---|---|---|
| `horas_parada` | `data_fim − ordem_servico.aberta_em` | Indicadores, encerramento |
| `horas_trabalhadas` | `(data_fim − iniciada_em) − Σ pausas posteriores a iniciada_em` | Card do Gestor, encerramento |
| `custo_total` | `COALESCE(custo_hora_tecnico, 0) + custo_manutencao` | Detalhes da OS, indicadores |

**Por que só as pausas posteriores a `iniciada_em`:** uma pausa dada com a OS ainda `Aberta` não
interrompe trabalho nenhum — a sessão de trabalho só existe depois de "Iniciar Atendimento". Como
`iniciada_em` é nulo até esse clique, toda pausa registrada depois dele é necessariamente uma pausa
de trabalho.

**Por que `horas_parada` não desconta pausas:** a máquina continua parada mesmo enquanto o técnico
espera uma peça. São dois relógios independentes (CLAUDE.md item 9).

```sql
-- Horas de uma OS encerrada: nenhuma das duas é coluna.
CREATE VIEW vw_os_horas AS
SELECT os.id AS ordem_servico_id,
       EXTRACT(EPOCH FROM (e.data_fim - os.aberta_em)) / 3600 AS horas_parada,
       EXTRACT(EPOCH FROM (e.data_fim - os.iniciada_em)) / 3600
         - COALESCE((SELECT SUM(EXTRACT(EPOCH FROM (p.retomada_em - p.pausada_em))) / 3600
                       FROM os_pausa p
                      WHERE p.ordem_servico_id = os.id
                        AND p.pausada_em >= os.iniciada_em), 0) AS horas_trabalhadas
  FROM ordem_servico os
  JOIN os_encerramento e ON e.ordem_servico_id = os.id;

-- "OS Finalizada" do Gestor, do Técnico e do Administrador: estado derivado, não um status.
CREATE VIEW vw_os_finalizada AS
SELECT os.*,
       COALESCE(c.custo_hora_tecnico, 0) + c.custo_manutencao AS custo_total
  FROM ordem_servico os
  JOIN os_custo c ON c.ordem_servico_id = os.id
 WHERE os.status = 'Concluída';

-- O complemento exato: a tela "Custos Pendentes" do Administrador.
CREATE VIEW vw_os_custo_pendente AS
SELECT os.*
  FROM ordem_servico os
 WHERE os.status = 'Concluída'
   AND NOT EXISTS (SELECT 1 FROM os_custo c WHERE c.ordem_servico_id = os.id);
```

> **Nota sobre indicadores:** `servicoIndicadores` hoje gera MTTR/MTBF/custo mockados e
> determinísticos, desligados dos fechamentos reais (CLAUDE.md item 8). Estas views são o alvo para
> onde ele deve apontar na integração.

---

## 5. Constraints que o banco deve garantir

Regras que hoje só existem em Zod no front-end. Se ficarem apenas lá, um `INSERT` direto ou um
segundo cliente da API furam todas elas.

### 5.1 Tipos ENUM

```sql
CREATE TYPE perfil_usuario     AS ENUM ('solicitante','tecnico','gestor','administrador');
CREATE TYPE tipo_os            AS ENUM ('maquinario','terceiros','reparo');
CREATE TYPE origem_solicitacao AS ENUM ('solicitante','preventiva');
CREATE TYPE status_solicitacao AS ENUM ('Pendente','Convertida','Rejeitada');
CREATE TYPE status_os          AS ENUM ('Aberta','Em Andamento','Pausada','Concluída');
CREATE TYPE marcador_impacto   AS ENUM ('Afeta Produção','Parada Parcial','Retrabalho');
CREATE TYPE tipo_anexo         AS ENUM ('foto','video');
```

### 5.2 Coerência dos três tipos de OS

```sql
-- Reparo não tem máquina cadastrada nem tipo de defeito; os outros dois exigem ambos.
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_solicitacao_alvo CHECK (
  (tipo =  'reparo' AND maquina_id IS NULL     AND item_descricao IS NOT NULL
                    AND tipo_defeito_id IS NULL) OR
  (tipo <> 'reparo' AND maquina_id IS NOT NULL AND item_descricao IS NULL
                    AND tipo_defeito_id IS NOT NULL));

-- Ou técnico interno com urgência, ou empresa terceirizada. Nunca os dois, nunca nenhum.
ALTER TABLE ordem_servico ADD CONSTRAINT ck_os_executor CHECK (
  (tipo =  'terceiros' AND empresa_terceirizada_id IS NOT NULL
                       AND tecnico_id IS NULL     AND urgencia_id IS NULL) OR
  (tipo <> 'terceiros' AND empresa_terceirizada_id IS NULL
                       AND tecnico_id IS NOT NULL AND urgencia_id IS NOT NULL));

-- O tipo se propaga por FK composta em toda a cadeia, sem trigger: a solicitação expõe
-- o par (id, tipo), a OS referencia o par inteiro, e as filhas da OS fazem o mesmo.
-- Assim um CHECK local já enxerga o tipo e não precisa consultar a tabela pai.
ALTER TABLE solicitacao_os  ADD CONSTRAINT uq_solicitacao_tipo UNIQUE (id, tipo);
ALTER TABLE ordem_servico   ADD CONSTRAINT fk_os_solicitacao_tipo
  FOREIGN KEY (solicitacao_id, tipo) REFERENCES solicitacao_os (id, tipo);

ALTER TABLE ordem_servico   ADD CONSTRAINT uq_os_tipo UNIQUE (id, tipo);
ALTER TABLE os_custo        ADD CONSTRAINT fk_custo_os_tipo
  FOREIGN KEY (ordem_servico_id, tipo) REFERENCES ordem_servico (id, tipo);
ALTER TABLE os_encerramento ADD CONSTRAINT fk_encerramento_os_tipo
  FOREIGN KEY (ordem_servico_id, tipo) REFERENCES ordem_servico (id, tipo);

-- Terceiros nunca passa por técnico: não existe encerramento para esse tipo.
ALTER TABLE os_encerramento ADD CONSTRAINT ck_encerramento_sem_terceiros
  CHECK (tipo <> 'terceiros');

-- Terceiros: o Administrador registra o que a empresa fez, e não há custo hora de técnico.
ALTER TABLE os_custo ADD CONSTRAINT ck_custo_por_tipo CHECK (
  (tipo =  'terceiros' AND custo_hora_tecnico IS NULL AND descricao_servico IS NOT NULL) OR
  (tipo <> 'terceiros' AND descricao_servico IS NULL));
```

### 5.3 Cardinalidades, unicidade e coerência de tenant

```sql
-- Uma solicitação vira no máximo uma OS; uma OS tem no máximo um encerramento e um custo.
ALTER TABLE ordem_servico   ADD CONSTRAINT uq_os_solicitacao  UNIQUE (solicitacao_id);
ALTER TABLE os_encerramento ADD CONSTRAINT uq_encerramento_os UNIQUE (ordem_servico_id);
ALTER TABLE os_custo        ADD CONSTRAINT uq_custo_os        UNIQUE (ordem_servico_id);

-- Uma OS não pode ter duas pausas abertas simultaneamente.
CREATE UNIQUE INDEX uq_pausa_aberta ON os_pausa (ordem_servico_id)
  WHERE retomada_em IS NULL;

-- Uma preventiva não gera duas solicitações pendentes ao mesmo tempo
-- (mas PODE gerar várias ao longo do tempo, a cada ciclo de intervalo_dias).
CREATE UNIQUE INDEX uq_preventiva_pendente ON solicitacao_os (preventiva_id)
  WHERE preventiva_id IS NOT NULL AND status = 'Pendente';

-- Origem da solicitação: humana ou automática, nunca as duas.
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_origem CHECK (
  ((origem = 'preventiva')  = (preventiva_id  IS NOT NULL)) AND
  ((origem = 'solicitante') = (solicitante_id IS NOT NULL)));

-- Área de atuação: obrigatória para técnico, proibida para os demais perfis.
ALTER TABLE usuario ADD CONSTRAINT ck_usuario_area_tecnico
  CHECK ((perfil = 'tecnico') = (area_tecnico_id IS NOT NULL));

-- Unicidade com escopo de tenant.
ALTER TABLE setor   ADD CONSTRAINT uq_setor_loja     UNIQUE (loja_id, nome);
ALTER TABLE usuario ADD CONSTRAINT uq_usuario_email  UNIQUE (tenant_id, email);
ALTER TABLE maquina ADD CONSTRAINT uq_maquina_patrim UNIQUE (tenant_id, numero_patrimonio);

-- Série vem do fabricante e pode faltar: índice único parcial, não UNIQUE comum.
CREATE UNIQUE INDEX uq_maquina_serie ON maquina (tenant_id, numero_serie)
  WHERE numero_serie IS NOT NULL;

-- Valores.
ALTER TABLE os_custo   ADD CONSTRAINT ck_custo_nao_negativo
  CHECK (custo_manutencao >= 0 AND COALESCE(custo_hora_tecnico, 0) >= 0);
ALTER TABLE preventiva ADD CONSTRAINT ck_intervalo CHECK (intervalo_dias > 0);
```

**Coerência do `tenant_id` denormalizado.** O banco precisa impedir que uma máquina do tenant A
aponte para um setor do tenant B. A forma de resolver **sem trigger** é o mesmo padrão de chave
composta usado para o `tipo`: o pai expõe o par `(tenant_id, id)` e a filha referencia o par inteiro.

```sql
ALTER TABLE loja  ADD CONSTRAINT uq_loja_tenant  UNIQUE (tenant_id, id);
ALTER TABLE setor ADD CONSTRAINT uq_setor_tenant UNIQUE (tenant_id, id);

ALTER TABLE setor   ADD CONSTRAINT fk_setor_loja
  FOREIGN KEY (tenant_id, loja_id)  REFERENCES loja  (tenant_id, id);
ALTER TABLE maquina ADD CONSTRAINT fk_maquina_setor
  FOREIGN KEY (tenant_id, setor_id) REFERENCES setor (tenant_id, id);
```

O mesmo padrão vale para `solicitacao_os → maquina`, `ordem_servico → solicitacao_os` e as filhas da
OS. É o que torna o vazamento entre tenants **estruturalmente impossível**, em vez de depender de
disciplina no código.

### 5.4 Duas regras que `CHECK` não alcança

Ambas precisam de trigger `CONSTRAINT ... DEFERRABLE INITIALLY DEFERRED`, avaliada no fim da
transação:

1. **Toda solicitação tem pelo menos um anexo do tipo `foto`.** A foto do defeito bloqueia o envio no
   front, mas a linha do anexo só pode existir depois da solicitação — então a verificação não cabe
   num `NOT NULL` nem num `CHECK` de linha.
2. **Usuário com `perfil = 'administrador'` não tem escopo.** Para ele, o acesso total ao tenant é
   justamente a ausência de linhas em `usuario_escopo`; um escopo cadastrado seria uma contradição
   silenciosa.

### 5.5 Índices e isolamento

**Índices:** todas as FKs, mais `(tecnico_id, status)` em `ordem_servico` — a consulta exata do
Painel do Técnico — e `(tenant_id, status)` em `solicitacao_os`, que é a aba Solicitações do Gestor.

**Isolamento multi-tenant:** com `tenant_id` nas tabelas de negócio, o caminho no PostgreSQL é
**Row Level Security** com uma policy por tabela usando `current_setting('app.tenant_id')`,
garantindo o corte no banco e não só na aplicação.

---

## 6. Pontos em aberto (decisões que dependem de você)

> **Resolvido na revisão 1:** `empresa` **é** o tenant (seção 3.1).
> **Resolvido na revisão 2:** forma do Pequeno Reparo, anexos como tabela, custo separado da
> execução, e `ENUM` versus tabela de domínio (seção 2).
> **Resolvido na revisão 3:** PKs/FKs de negócio viram `bigint` (seção 3.12) e o front alinha
> `setor` ao modelo — era o único lado ainda tratando setor como união estática (ponto 3 abaixo,
> mantido riscado por registro histórico).

1. **Storage real dos anexos.**
   Hoje foto e vídeo viram `blob:` do navegador via `URL.createObjectURL` — somem ao recarregar a
   página. Em produção precisam de bucket (S3, MinIO, R2), e aí `solicitacao_anexo` ganha `bucket` e
   `chave` no lugar de uma `url` solta. Vale também definir política de retenção: foto de OS de 2019
   continua ocupando espaço.

2. **Histórico de lançamento de custo.**
   `os_custo` é 1:1 com a OS e guarda quem lançou e quando — mas se o Administrador corrigir o valor,
   o anterior se perde. Se o número for para relatório contábil, vale `os_custo_historico` em
   append-only, com a linha vigente sendo a mais recente.

3. ~~**`setor` dinâmico vs. enum estático.**~~ **Resolvido na revisão 3.**
   O banco sempre modelou setor como tabela — era o front que ainda validava `setor` contra a união
   estática `setoresDisponiveis` (removida de `/src/tipos/maquina.ts`). Os formulários (`CadastrarMaquina`,
   `CadastrarUsuario`) passaram a consumir `servicoSetores.listar()` em cascata a partir da loja
   escolhida, e todo tipo que referenciava setor por nome (`Maquina`, `SolicitacaoOS`, `OrdemServico`,
   `PreventivaListada`, `SessaoUsuario`, `EscopoAcessoGestor`, `Usuario`) passou a referenciá-lo por id
   (`setorId`/`setorNome`, ou `setoresIds` onde é uma lista). Ver CLAUDE.md, regra "Setor é cadastro
   dinâmico, referenciado por id".

4. **Auditoria de toda transição de status.**
   Modelei o status atual mais o histórico de pausas. Se for preciso saber quem moveu a OS de Aberta
   para Em Andamento e quando, vale `os_evento` (`status_anterior`, `status_novo`, `usuario_id`,
   `ocorrido_em`) — e aí `os_pausa` poderia até ser derivada dela.

5. **Empresa terceirizada por loja.**
   Modelada como cadastro do tenant, sem vínculo de loja. Se na prática cada filial negociar com
   fornecedores diferentes e o Gestor não puder escolher um que não atende a região dele, entra uma
   N:N `empresa_terceirizada_loja` — e o select de aprovação passa a filtrar pela loja da solicitação.
