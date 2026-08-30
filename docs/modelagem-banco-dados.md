# Modelagem do Banco de Dados — Revisão 4

Documento gerado a partir da revisão do código do front-end (`/src/tipos`, `/src/servicos`,
`/src/paginas`) comparado ao modelo da revisão anterior.

- **Revisão 1** (03/08/2026): comparou o DER original com a interface e reescreveu o modelo — 21 entidades.
- **Revisão 2** (08/08/2026): incorpora os três tipos de OS, as empresas
  terceirizadas, o perfil Administrador e a evidência visual do defeito — **20 tabelas + 7 tipos ENUM**.
- **Revisão 3** (10/08/2026): dois ajustes puxados pelo front, sem mudança de
  forma do modelo — **PKs/FKs de negócio trocam de `uuid` para `bigint`** (seção 3.12) e o
  **front para de tratar setor como união estática** e passa a referenciá-lo por id em toda
  parte (`setorId`/`setorNome`, no mesmo padrão de `lojaId`/`lojaNome`) — o banco já modelava
  `setor` como tabela desde a revisão 1; era só o front que ainda não confiava nisso (ponto 3
  da seção 6, agora resolvido).
- **Revisão 4** (12/08/2026, este documento): **terceirizar deixou de ser um tipo de pedido e
  virou um desfecho da OS**, decidido pelo Técnico no meio da execução. É a maior mudança de
  forma desde a revisão 2 e desmonta três decisões antigas: a solicitação passa a ter só dois
  tipos, o tipo da OS deixa de ser imutável (e com ele cai a FK composta entre solicitação e
  OS), e o encerramento passa a existir para todos os tipos. Junto vieram a classificação
  **Predial/Corretiva** no encerramento, a flag **`afeta_producao`** governando o relógio de
  máquina parada, e a rejeição do Gestor com motivo — **19 tabelas + 9 tipos ENUM** (seção 1.4).
- **Revisão 4.1** (14/08/2026): correção de **fórmula**, sem mudança de forma — o relógio de
  máquina parada passa a começar em `solicitacao_os.criado_em`, e não em `ordem_servico.aberta_em`
  (seção 4). Continuam **19 tabelas + 9 tipos ENUM**: nenhuma coluna nasce ou muda, só a view
  `vw_os_horas` ganha um join e o contrato passa a devolver `dataSolicitacao` na OS.

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
| 1 | **Tipo de OS** | Uma única natureza de OS: máquina cadastrada, técnico do quadro, urgência. Todas as colunas obrigatórias para todos. | `tipo_os` (`maquinario` / `terceiros` / `reparo`) em **`solicitacao_os` e `ordem_servico`**. O tipo decide quais colunas são obrigatórias e quais são proibidas. Ver `/src/tipos/ordemServico.ts`, `servicoReparos.ts`. **→ revisto na revisão 4 (1.4.1): a solicitação ficou com dois tipos e o da OS virou mutável.** |
| 2 | **Empresa terceirizada** | Não existe. Toda OS aponta para um `tecnico_id`. | Nova tabela `empresa_terceirizada` (cadastro do Administrador, sem vínculo de loja) e FK na OS. Ver `/src/tipos/empresaTerceirizada.ts`. **→ revisto na revisão 4 (1.4.2): deixou de ser mutuamente exclusiva com `tecnico_id` — a OS terceirizada continua com o Técnico.** |
| 3 | **Pequeno Reparo sem máquina** | `solicitacao_os.maquina_id` `NOT NULL`. | O Solicitante digita o item na hora ("Lâmpada de LED"): `maquina_id` vira nullable e entra `item_descricao`, amarrados por CHECK. Ver `/src/tipos/reparo.ts`, `NovaSolicitacaoReparo.tsx`. |
| 4 | **4º perfil: Administrador** | Três perfis, todos delimitados por loja/setor. | `administrador` enxerga o tenant inteiro e é dono de todos os cadastros. Modelado como **zero linhas em `usuario_escopo`** — a ausência de escopo é o que significa acesso total. Ver `/src/tipos/autenticacao.ts`, `RotaProtegida.tsx`. |
| 5 | **Identificação da máquina** | `maquina.tag` com `UNIQUE (tenant_id, tag)`. | `numero_patrimonio` (do cliente, sempre existe) e `numero_serie` (do fabricante, pode faltar). Duas identidades de origens diferentes, com regras de unicidade diferentes. Ver `/src/tipos/maquina.ts`, `CadastrarMaquina.tsx`. |
| 6 | **Evidência visual do defeito** | Nenhum lugar para arquivo — a solicitação era só texto. | Foto **obrigatória** (bloqueia o envio) e vídeo opcional, capturados pela câmera do celular. Nova tabela `solicitacao_anexo`. Ver `UploadFoto.tsx`, `UploadVideo.tsx`. |
| 7 | **Custo** | Uma coluna `custo` em `os_encerramento`, preenchida pelo Técnico. | `custo_hora_tecnico` + `custo_manutencao` na nova tabela `os_custo`, com `lancado_por_id` / `lancado_em` — o Técnico informa, o Administrador revisa em Custos Pendentes. Ver `ModalEncerrarOrdemServico.tsx`, `AdministradorCustosPendentes.tsx`. |
| 8 | **Encerramento não é universal** | Toda OS concluída tinha um `os_encerramento` 1:1. | OS Terceiros **nascia** `Concluída` na aprovação do Gestor e nunca passava por Técnico. **→ desfeito na revisão 4 (1.4.3): voltou a ser universal — toda OS é encerrada pelo Técnico, inclusive a terceirizada.** |

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

| | Revisão 1 | Revisão 2 | Revisão 4 |
|---|---|---|---|
| Tabelas de negócio | 13 | 16 | 16 |
| Tabelas de domínio (lookup) | 8 | 4 | 3 |
| **Total de tabelas** | **21** | **20** | **19** |
| Tipos `ENUM` nativos | 0 | 7 | 9 |

Tabelas novas na revisão 2: `empresa_terceirizada`, `solicitacao_anexo`, `os_custo`.
Tabelas que viraram `ENUM` na revisão 2: `perfil_usuario`, `status_solicitacao`, `status_os`,
`marcador_impacto`.
Na revisão 4, `tipo_defeito` deixa de ser tabela de domínio e vira `ENUM` de dois valores
(1.4.4), e entra o `ENUM` `tipo_solicitacao` (1.4.1). Nenhuma tabela nova.

### 1.4 Mudanças estruturais da revisão 4

Todas saem de uma única decisão de negócio: **quem decide terceirizar é o Técnico, olhando a
máquina — não o Solicitante, ao relatar o problema, nem o Gestor, ao aprovar.** O Solicitante
descreve o que está quebrado; classificar e encaminhar é de quem executa.

#### 1.4.1 `terceiros` sai da solicitação: dois ENUM em vez de um

`solicitacao_os.tipo` passa a usar o novo `tipo_solicitacao` (`maquinario` | `reparo`).
`tipo_os` (três valores) continua existindo, mas só em `ordem_servico` e nas filhas dela.

A consequência pesada é que **`ordem_servico.tipo` deixou de ser imutável**: a OS nasce com o
tipo da solicitação e é *promovida* a `terceiros` quando o Técnico aciona uma empresa. Com
isso cai a FK composta `(solicitacao_id, tipo) → solicitacao_os (id, tipo)` da revisão 2 —
ela exigia domínios iguais e valor fixo, e agora nenhuma das duas coisas vale. As FKs
compostas **de dentro da OS para baixo** (`os_custo`, `os_encerramento`) continuam, com
`ON UPDATE CASCADE` (seção 5.2).

#### 1.4.2 A OS terceirizada continua com o Técnico

`empresa_terceirizada_id` deixa de ser mutuamente exclusiva com `tecnico_id`. Agora:

- `tecnico_id` e `urgencia_id` são **obrigatórios em toda OS** — inclusive a terceirizada;
- `empresa_terceirizada_id` é preenchida **se e somente se** `tipo = 'terceiros'`;
- entra `terceiro_acionado_em`, o instante do encaminhamento (não exposto no contrato hoje,
  mas é a única forma de responder "quando isso saiu da mão do time interno").

Acionar não muda status: se o atendimento vai demorar, o caminho continua sendo **pausar** com
o motivo — a espera não conta como hora trabalhada, e a máquina parada segue contando.

#### 1.4.3 Encerramento volta a ser universal

`os_encerramento` existe para os três tipos e o CHECK `tipo <> 'terceiros'` some. Quem recebe
o serviço da empresa é o Técnico, e é ele quem sabe escrever defeito constatado, causa raiz e
solução. Some junto a gambiarra registrada em 2.3 (o Administrador digitando a "Descrição do
Serviço Realizado" no lugar do `solucao`).

#### 1.4.4 `tipo_defeito` vira "Tipo de OS" e muda de lugar

Era uma tabela de domínio (`Mecânico`, `Elétrico`, `Hidráulico`, …) referenciada pela
**solicitação**. Virou um `ENUM` de dois valores — `Predial` | `Corretiva` — gravado no
**encerramento**, por quem executou. Duas mudanças em uma:

- **de lugar:** o Solicitante não distingue predial de corretiva; ele relata o problema. A
  classificação é fato de execução, não de abertura;
- **de forma:** com dois valores fechados que só mudam com deploy (o gráfico de rosca do
  Painel de Indicadores tem uma cor por valor), a regra da seção 2.4 manda `ENUM`, não tabela.

> O campo continua se chamando `tipo_defeito` no contrato e no banco, embora a interface o
> rotule "Tipo de OS": renomear exigiria mudança simultânea nas duas pontas, sem ganho.

#### 1.4.5 `afeta_producao`: só conta parada quem parou

O marcador `Afeta Produção` (único sobrevivente de `marcador_impacto`, que tinha três) deixou
de ser informativo: é ele que liga o relógio de máquina parada. `ordem_servico.afeta_producao`
copia a decisão da solicitação no momento da abertura, e **`horas_parada` só é calculada
quando a flag é verdadeira** (seção 4) — nas demais a API omite o campo e a tela escreve "Não
se aplica", nunca `0h`, que sugeriria uma parada instantânea.

A cópia é deliberada: a OS não deve mudar de comportamento se alguém editar a solicitação
depois.

> **Ajuste da revisão 4.1:** a segunda justificativa que estava aqui — "e o cálculo de horas não
> deveria precisar de join com a origem" — caiu junto com a mudança da seção 4: o relógio de parada
> agora **começa** em `solicitacao_os.criado_em`, então o join com a origem passou a ser necessário
> de qualquer jeito. A flag continua copiada, mas por outro motivo: `afeta_producao` é uma **decisão
> editável** (alguém pode corrigir o marcador da solicitação depois), enquanto `criado_em` é um
> **instante imutável** — copiar o primeiro congela a regra, ler o segundo por join é sempre seguro.

#### 1.4.6 Rejeição com motivo, autor e instante

`solicitacao_os` ganha `motivo_rejeicao`, `rejeitado_por_id` e `rejeitada_em`, amarrados por
CHECK ao `status = 'Rejeitada'` (seção 5.3). O status já existia desde a revisão 1, mas sem
lugar para o texto — e rejeitar em silêncio devolve o Solicitante ao ponto de partida: ele
reabre o mesmo pedido e a fila do Gestor nunca esvazia.

#### 1.4.7 `solicitacao_os.setor_id`: lacuna herdada, fechada aqui

Toda listagem do Gestor agrupa por Loja e Setor, mas o Pequeno Reparo não tem máquina — e era
da máquina que o setor vinha. A solicitação passa a carregar `setor_id` próprio: copiado do
setor da máquina no Maquinário, e do escopo do Solicitante no Reparo. Além de fechar o buraco,
o snapshot preserva o histórico se o Solicitante for movido de setor depois.

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

> **Ajustado na revisão 4.** A separação continua, mas os dois momentos deixaram de ser
> sequenciais: o Técnico já informa `custo_manutencao` (e `custo_hora_tecnico`, só no
> Maquinário) **no encerramento**, então a linha de `os_custo` nasce na mesma transação, com
> `lancado_por_id` = técnico. O Administrador entra depois só para **corrigir** — tipicamente
> conferindo o valor contra a nota fiscal da empresa terceirizada, e por isso `os_custo` ganhou
> `numero_nota_fiscal`, `serie_nota_fiscal` e `descricao_servico_terceiro` (os três só válidos
> em `tipo = 'terceiros'`, e todos opcionais). O antigo `descricao_servico` **obrigatório** em
> terceiros morreu com a gambiarra que ele resolvia: agora existe `os_encerramento.solucao`
> para todo tipo, escrito pelo Técnico (1.4.3).
>
> Efeito colateral a assumir: como o custo nasce junto do encerramento, `vw_os_custo_pendente`
> quase sempre vem vazia. A tela "Custos Pendentes" do Administrador por isso lista **toda OS
> `Concluída`**, com ou sem custo — ela é a fila de conferência, não a de digitação (seção 4).

### 2.4 `ENUM` no que é regra de código, tabela no que o cliente cadastra

**Viraram `ENUM` nativo:** `perfil_usuario`, `tipo_solicitacao`, `tipo_os`, `tipo_defeito`,
`origem_solicitacao`, `status_solicitacao`, `status_os`, `marcador_impacto`, `tipo_anexo`.
(`tipo_solicitacao` e `tipo_defeito` entraram na revisão 4 — ver 1.4.1 e 1.4.4.)

Nenhum deles ganha um valor novo sem alguém escrever código que o trate — então virar `INSERT` seria
uma flexibilidade falsa, que só serve para inserir valor que a aplicação não entende.

**Continuam tabela:** `area_tecnico`, `nivel_criticidade`, `nivel_urgencia`. São
vocabulário do cliente: um supermercado pode querer "Automação" onde outro quer "Ar-condicionado".

> **Ajustado depois da revisão 4.1.** `nivel_criticidade` (migration `000004`) e
> `nivel_urgencia` (migration `000007`) viraram `ENUM` também: nenhum dos dois provou ser
> vocabulário do cliente na prática — o front tipa os dois como tupla fixa
> (`niveisCriticidade`/`niveisUrgencia`) e nunca existiu tela de cadastro/edição de nível
> em nenhum dos dois. A tabela ficava vazia até alguém escrever um `INSERT` manual (mesma
> lacuna que `area_tecnico` teve até a `000006`), travando cadastro de máquina e abertura de
> OS respectivamente. `area_tecnico` continua tabela porque é vocabulário do cliente de
> verdade (o próprio exemplo "Automação" vs. "Ar-condicionado" já pressupõe um cadastro
> variando por tenant, e a `000006` prova que o produto espera `INSERT` novo ali). Efeito:
> das "três tabelas" do parágrafo original, sobra uma.

**Resultado:** nove listas de domínio caem para três tabelas, some um JOIN de toda listagem quente,
e uma limitação registrada na revisão 1 desaparece — o CHECK de "área de atuação é obrigatória para
técnico" precisava de subquery (que `CHECK` não aceita) e agora é uma expressão simples:

```sql
ALTER TABLE usuario ADD CONSTRAINT ck_usuario_area_tecnico
  CHECK ((perfil = 'tecnico') = (area_tecnico_id IS NOT NULL));
```

**Contrapartida assumida:** um marcador de impacto novo passa a ser `ALTER TYPE` + deploy, não
`INSERT`. Aceitável, porque um marcador novo exige código no front para exibi-lo de qualquer forma —
e a revisão 4 mostrou que o movimento tende a ser o contrário: `marcador_impacto` **encolheu** de
três valores para um (`Afeta Produção`), porque os outros dois não mudavam decisão nenhuma.

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
CLAUDE.md).

> O campo `dataPausa`, adicionado ao front nesta mesma leva, já era previsto aqui como
> `os_pausa.pausada_em` desde a revisão 1.

### 3.4 "Finalizada" não é um status

A OS finalizada é `status = 'Concluída'` **e** custo já lançado — regra que aparece em três telas
(aba do Gestor, aba do Técnico e tela do Administrador). Isso é uma **view**, não um sexto valor em
`status_os`.

Como valor de status, dois lugares passariam a afirmar a mesma coisa, e o `UPDATE` que esquecesse um
deles criaria uma OS finalizada sem custo.

### 3.5 `tipo` repetido é denormalização deliberada

O tipo desce da OS para `os_custo` e `os_encerramento`, e cada salto é travado por FK composta
(seção 5.2). Sem ela, "custo hora técnico só existe em maquinário" e "nota fiscal só existe em
terceiros" virariam trigger — um `CHECK` não enxerga a tabela pai. A FK composta faz o próprio
PostgreSQL garantir que a cópia nunca diverge.

> **Revisão 4:** o salto **de fora para dentro** (solicitação → OS) foi cortado. Os dois lados
> deixaram de compartilhar domínio (`tipo_solicitacao` × `tipo_os`) e o valor da OS deixou de ser
> imutável, então a FK composta ali não tinha mais o que garantir — ver 1.4.1. Dentro da OS a
> cadeia continua, agora com `ON UPDATE CASCADE`, porque o `tipo` do pai pode mudar (`maquinario`
> → `terceiros`) enquanto a OS ainda está aberta.
>
> A denormalização de `afeta_producao` (1.4.5) segue outra lógica: é **snapshot**, não cópia
> sincronizada. Editar a solicitação depois não deve mudar como a OS conta horas.

### 3.6 Patrimônio e série têm regras diferentes

`numero_patrimonio` é obrigatório e único no tenant. `numero_serie` ganha índice único **parcial**,
válido só quando preenchido — a série vem do fabricante e falta em equipamento antigo ou improvisado.

### 3.7 Técnico continua sendo `usuario`

O front tem dois serviços (`servicoUsuarios`, `servicoTecnicos`), mas no banco é uma tabela só, com
`perfil = 'tecnico'` e `area_tecnico_id` preenchido.

A separação é de leitura, não de escrita: `GET /tecnicos` é **projeção somente-leitura** para o
select de Técnico Responsável, e todo `INSERT`/`UPDATE`/`DELETE` de técnico passa por `/usuarios`.
Duas superfícies de escrita abririam a porta para o mesmo e-mail existir duas vezes no mesmo tenant.

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

`solicitacao_anexo.chave` (e `maquina.foto_chave`, mesmo motivo) guarda a chave do objeto no R2, não
um endereço assinado. A URL de acesso é gerada na leitura (`bucketR2.URLLeitura`), porque link
assinado expira — persistido, o banco acumula endereços que param de funcionar sem nada indicar que
quebraram. Sem coluna `bucket`: cada tipo de anexo já sobe pra um bucket fixo, escolhido no código
que registra a rota (`bucketR2.UploadFoto(url, bucket)`), não varia por linha — guardar isso no banco
seria flexibilidade que nada no contrato pede.

### 3.11 Impactos como N:N

`solicitacao_impacto` continua sendo a associativa (uma linha por marcador por solicitação), casando
com o array já usado no front (`impactos: MarcadorImpacto[]`). O que mudou na revisão 2 é que o
marcador deixou de ser FK para uma tabela de domínio e passou a ser coluna `ENUM` (seção 2.4).

> **Revisão 4:** sobrou **um** marcador (`Afeta Produção`) e ele deixou de ser informativo — é o que
> liga o relógio de máquina parada (1.4.5). Com um valor só, a associativa poderia virar um `boolean`
> em `solicitacao_os`; foi mantida porque o contrato troca uma lista e um marcador novo não mexeria
> em tabela nem em payload. A decisão que realmente importa está do outro lado: a OS **não** consulta
> esta tabela para calcular horas — ela carrega `afeta_producao` como snapshot.

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
| `horas_parada` | `data_fim − solicitacao_os.criado_em`, **só se `afeta_producao`** | Indicadores, encerramento |
| `horas_trabalhadas` | `(data_fim − iniciada_em) − Σ pausas posteriores a iniciada_em` | Card do Gestor, encerramento |
| `custo_total` | `COALESCE(custo_hora_tecnico, 0) + custo_manutencao` | Detalhes da OS, indicadores |

**Por que `horas_parada` pode ser `NULL`:** com `afeta_producao = false` a máquina seguiu operando
durante o atendimento — não houve parada para medir. O valor correto é a ausência do número, não
zero: `0h` seria uma parada que começou e terminou no mesmo instante, coisa que não aconteceu. A API
omite o campo e a tela escreve "Não se aplica" (1.4.5).

**Por que só as pausas posteriores a `iniciada_em`:** uma pausa dada com a OS ainda `Aberta` não
interrompe trabalho nenhum — a sessão de trabalho só existe depois de "Iniciar Atendimento". Como
`iniciada_em` é nulo até esse clique, toda pausa registrada depois dele é necessariamente uma pausa
de trabalho.

**Por que `horas_parada` não desconta pausas:** a máquina continua parada mesmo enquanto o técnico
espera uma peça. São dois relógios independentes (CLAUDE.md item 9).

**Por que o relógio começa na solicitação, e não na abertura da OS (revisão 4.1):** a máquina parou
quando o Solicitante relatou o problema, não quando o Gestor achou tempo de aprovar. Medindo a
partir de `aberta_em`, toda a espera na fila do Gestor sumia do indicador — justamente o pedaço que
o cliente pode encurtar, e o único que a operação controla. Pior: quanto mais devagar o Gestor
aprovasse, melhor ficaria o número. O contrato passa a devolver esse instante na OS
(`dataSolicitacao`, denormalizado do join), para que a tela consiga explicar de onde vem o total.

> **Migração:** OS encerradas antes da mudança não guardam nada de errado — `horas_parada` nunca
> foi coluna, então o novo valor sai da view no próximo SELECT, retroativo por construção. O que
> muda são os indicadores históricos, que sobem. Se a comparação com números já divulgados
> importar, a saída é a de sempre: recortar o gráfico por data, não congelar a fórmula.

```sql
-- Horas de uma OS encerrada: nenhuma das duas é coluna.
CREATE VIEW vw_os_horas AS
SELECT os.id AS ordem_servico_id,
       -- Parada conta desde o pedido do Solicitante: a espera na fila do Gestor é parada real.
       CASE WHEN os.afeta_producao
            THEN EXTRACT(EPOCH FROM (e.data_fim - s.criado_em)) / 3600
       END AS horas_parada,
       EXTRACT(EPOCH FROM (e.data_fim - os.iniciada_em)) / 3600
         - COALESCE((SELECT SUM(EXTRACT(EPOCH FROM (p.retomada_em - p.pausada_em))) / 3600
                       FROM os_pausa p
                      WHERE p.ordem_servico_id = os.id
                        AND p.pausada_em >= os.iniciada_em), 0) AS horas_trabalhadas
  FROM ordem_servico os
  JOIN os_encerramento e ON e.ordem_servico_id = os.id
  JOIN solicitacao_os s ON s.id = os.solicitacao_id;

-- "OS Finalizada" do Gestor, do Técnico e do Administrador: estado derivado, não um status.
CREATE VIEW vw_os_finalizada AS
SELECT os.*,
       COALESCE(c.custo_hora_tecnico, 0) + c.custo_manutencao AS custo_total
  FROM ordem_servico os
  JOIN os_custo c ON c.ordem_servico_id = os.id
 WHERE os.status = 'Concluída';

-- Custo ainda não lançado. Desde a revisão 4 é caso raro (o Técnico informa os valores no
-- encerramento), mas continua sendo a resposta certa para "que OS está sem custo?".
CREATE VIEW vw_os_custo_sem_lancamento AS
SELECT os.*
  FROM ordem_servico os
 WHERE os.status = 'Concluída'
   AND NOT EXISTS (SELECT 1 FROM os_custo c WHERE c.ordem_servico_id = os.id);
```

> **A tela "Custos Pendentes" não usa a view acima.** Ela lista **toda** OS `Concluída`
> (`GET /ordens-servico?status=Concluída`), com ou sem custo lançado, porque virou fila de
> **conferência**: o valor já veio do Técnico e o Administrador confere contra a nota fiscal
> (seção 2.3). A view fica para relatório e alerta operacional.

> **Nota sobre indicadores:** `GET /indicadores/maquinas/:id` calcula MTTR/MTBF/custo a partir do
> histórico real de OS encerradas. Estas views são a base para esse cálculo — e o gráfico de rosca
> agrupa por `os_encerramento.tipo_defeito` (`Predial` / `Corretiva`), não mais por um tipo de
> defeito informado na abertura (1.4.4).

---

## 5. Constraints que o banco deve garantir

Regras que hoje só existem em Zod no front-end. Se ficarem apenas lá, um `INSERT` direto ou um
segundo cliente da API furam todas elas.

### 5.1 Tipos ENUM

```sql
CREATE TYPE perfil_usuario     AS ENUM ('solicitante','tecnico','gestor','administrador');
CREATE TYPE tipo_solicitacao   AS ENUM ('maquinario','reparo');
CREATE TYPE tipo_os            AS ENUM ('maquinario','terceiros','reparo');
CREATE TYPE tipo_defeito       AS ENUM ('Predial','Corretiva');
CREATE TYPE origem_solicitacao AS ENUM ('solicitante','preventiva');
CREATE TYPE status_solicitacao AS ENUM ('Pendente','Convertida','Rejeitada');
CREATE TYPE status_os          AS ENUM ('Aberta','Em Andamento','Pausada','Concluída');
CREATE TYPE marcador_impacto   AS ENUM ('Afeta Produção');
CREATE TYPE tipo_anexo         AS ENUM ('foto','video');
```

> `tipo_solicitacao` e `tipo_os` são dois tipos de propósito, e não um só reaproveitado: só a OS
> pode ser `terceiros`. Um `CHECK (tipo <> 'terceiros')` na solicitação resolveria hoje, mas
> deixaria o valor proibido visível em todo select gerado a partir do domínio.

### 5.2 Coerência dos tipos (revista na revisão 4)

```sql
-- Reparo não tem máquina cadastrada; Maquinário exige a FK e proíbe o texto livre.
-- Setor é obrigatório nos dois: no Maquinário vem da máquina, no Reparo do escopo
-- do Solicitante (1.4.7).
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_solicitacao_alvo CHECK (
  (tipo = 'reparo'     AND maquina_id IS NULL     AND item_descricao IS NOT NULL) OR
  (tipo = 'maquinario' AND maquina_id IS NOT NULL AND item_descricao IS NULL));

-- Toda OS tem técnico e urgência. A empresa terceirizada existe se e somente se o
-- Técnico acionou uma -- o que é exatamente o que 'terceiros' significa.
ALTER TABLE ordem_servico ADD CONSTRAINT ck_os_executor CHECK (
  tecnico_id IS NOT NULL AND urgencia_id IS NOT NULL AND
  ((tipo = 'terceiros') = (empresa_terceirizada_id IS NOT NULL)) AND
  ((empresa_terceirizada_id IS NOT NULL) = (terceiro_acionado_em IS NOT NULL)));

-- Dentro da OS o tipo continua se propagando por FK composta, sem trigger: a OS expõe
-- o par (id, tipo) e as filhas referenciam o par inteiro, então um CHECK local enxerga
-- o tipo sem consultar a tabela pai. ON UPDATE CASCADE porque o tipo da OS pode ser
-- promovido a 'terceiros' enquanto ela está aberta (1.4.1).
ALTER TABLE ordem_servico   ADD CONSTRAINT uq_os_tipo UNIQUE (id, tipo);
ALTER TABLE os_custo        ADD CONSTRAINT fk_custo_os_tipo
  FOREIGN KEY (ordem_servico_id, tipo) REFERENCES ordem_servico (id, tipo)
  ON UPDATE CASCADE;
ALTER TABLE os_encerramento ADD CONSTRAINT fk_encerramento_os_tipo
  FOREIGN KEY (ordem_servico_id, tipo) REFERENCES ordem_servico (id, tipo)
  ON UPDATE CASCADE;

-- A OS nasce com o tipo da solicitação e só pode ser promovida a 'terceiros'.
-- CHECK não vê o valor anterior: isto é uma trigger BEFORE UPDATE.
CREATE OR REPLACE FUNCTION trg_os_tipo_promocao() RETURNS trigger AS $$
BEGIN
  IF NEW.tipo <> OLD.tipo AND NEW.tipo <> 'terceiros' THEN
    RAISE EXCEPTION 'tipo da OS só muda para terceiros (era %, tentou %)', OLD.tipo, NEW.tipo;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

-- Custo hora do técnico só existe no Maquinário: em 'terceiros' quem trabalhou foi a
-- empresa, e em 'reparo' o serviço não cobra hora técnica. Dados da nota fiscal, o
-- espelho disso: só em 'terceiros'.
ALTER TABLE os_custo ADD CONSTRAINT ck_custo_por_tipo CHECK (
  (tipo = 'maquinario' OR custo_hora_tecnico IS NULL) AND
  (tipo = 'terceiros'  OR (numero_nota_fiscal IS NULL AND serie_nota_fiscal IS NULL
                           AND descricao_servico_terceiro IS NULL)));
```

> **O que saiu daqui na revisão 4:** a FK composta `(solicitacao_id, tipo)` entre OS e
> solicitação (domínios diferentes, valor mutável — 1.4.1) e o
> `ck_encerramento_sem_terceiros` (toda OS é encerrada pelo Técnico — 1.4.3).

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

-- Rejeição carrega motivo, autor e instante -- os três juntos ou nenhum (1.4.6).
ALTER TABLE solicitacao_os ADD CONSTRAINT ck_rejeicao CHECK (
  (status = 'Rejeitada') = (motivo_rejeicao IS NOT NULL AND rejeitado_por_id IS NOT NULL
                            AND rejeitada_em IS NOT NULL));

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
> **Resolvido na revisão 4:** terceirização vira desfecho da OS e não tipo de pedido, o setor
> passa a ser coluna da solicitação (ponto 6 abaixo) e a rejeição ganha motivo (seção 1.4).

1. ~~**Storage real dos anexos.**~~ **Resolvido na migration `000003` — sem coluna `bucket`.**
   `solicitacao_anexo.url` virou `chave` e `maquina.foto_url` virou `foto_chave` (seção 3.10):
   R2 real (Cloudflare) no lugar do `blob:` do navegador, chave prefixada por tenant
   (`bucketR2.UploadFoto`), URL assinada gerada na leitura (`bucketR2.URLLeitura`). Sem coluna
   `bucket`: o bucket de cada tipo de anexo é fixo no código que registra a rota, não varia por
   linha. Falta política de retenção: foto de OS de 2019 continua ocupando espaço — ainda em aberto.

2. **Histórico de lançamento de custo.** *(mais urgente desde a revisão 4)*
   `os_custo` é 1:1 com a OS e guarda quem lançou e quando — mas se o Administrador corrigir o valor,
   o anterior se perde. Isso deixou de ser hipótese: agora a linha **nasce** com o valor do Técnico e
   o fluxo normal é o Administrador editá-la contra a nota fiscal (seção 2.3), então o valor
   original é sobrescrito em toda OS terceirizada. Se o número for para relatório contábil, vale
   `os_custo_historico` em append-only, com a linha vigente sendo a mais recente.

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
   fornecedores diferentes e o Técnico não puder acionar um que não atende a região dele, entra uma
   N:N `empresa_terceirizada_loja` — e o select do `ModalAcionarTerceiro` passa a filtrar pela loja
   da OS.

6. ~~**Setor do Pequeno Reparo.**~~ **Resolvido na revisão 4** (1.4.7): `solicitacao_os.setor_id`.

7. **Trocar o técnico de uma OS.**
   Hoje `ordem_servico.tecnico_id` é fixo, e é isso que sustenta a inferência de autoria das
   transições (item 4 acima): quem iniciou/pausou/encerrou só pode ter sido ele. No dia em que uma OS
   puder ser reatribuída — férias, turno, especialidade errada — a inferência quebra e `os_evento`
   deixa de ser opcional. Vale decidir a ordem: reatribuição **depois** da auditoria, nunca antes.
