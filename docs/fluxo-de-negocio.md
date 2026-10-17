# Fluxo de negócio: o que está pronto e o que falta

O estado de cada fase e o porquê das decisões de negócio já tomadas.
Leia antes de implementar endpoint do miolo do fluxo (solicitação, OS, indicadores).

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

## O que falta no back (retomar aqui)

Cadastros: **completos**. Solicitações (fase 1): **completo** — ver seção própria
"Solicitações" abaixo. `GET /ordens-servico`: **completo** — com ele o Painel do Gestor
fica inteiro (ver "Ordem de serviço — listagem" abaixo). `GET /indicadores/maquinas/:id`:
**completo** — ver "Indicadores de máquina" abaixo. **Ciclo de vida da Ordem de
serviço**: **completo** — `iniciar`/`pausar`/`retomar`/`acionar-terceiro`/`encerrar`
(Técnico, `PainelTecnico`) e `custo` (Administrador, correção pós-encerramento em
`AdministradorCustosPendentes`), todos ESCRITA, nenhum do Gestor. Destravou os dois
cards mortos do painel do Administrador e, de quebra, tira os indicadores do zero (que
liam um histórico de encerramentos que ninguém escrevia ainda — o aviso de "painel
responde só zeros" em "Indicadores de máquina" abaixo deixou de valer).

`custo` (`POST /ordens-servico/:id/custo`, `CorrigirCusto` em
`internal/service/ordemServico.go`) não é uma criação, é uma correção: `os_custo` já
nasce em `Encerrar`, junto do `os_encerramento` (o Técnico já lança os dois custos ao
fechar a OS). O Administrador só ajusta depois, tipicamente conferindo o Custo de
Manutenção contra a nota fiscal de uma OS terceirizada. Por isso o service exige a OS já
`Concluída` (senão não existe `os_custo` pra atualizar) e repete os CHECKs em Go pelo
mesmo motivo de `Encerrar`: sem isso, o erro que sobe é o `CHECK` do banco estourando,
genérico, em vez de dizer qual campo está errado. Sobraram dois — hora técnica só em
`maquinario` (agora checada **por tarefa**, dentro de `gravarItensDeCusto`) e
`descricaoServicoTerceiro` só em `terceiros`. **`numeroNotaFiscal` e `serieNotaFiscal`
saíram dessa restrição na migration `000010`** e, na `000012`, saíram de `os_custo`
inteiras: valem em qualquer tipo (maquinário troca peça comprada com nota, reparo consome
material com nota) e agora são **várias** por OS — ver "Custo itemizado" abaixo.

**Quem decide se há nota é o Técnico, no encerramento** (`temNotaFiscal` no
`EncerramentoOrdemServicoPayload`, migration `000011`): foi ele que executou e sabe se
houve compra ou se foi só mão de obra. É essa declaração — e não o tipo da OS — que faz
a tela do Administrador pedir número e série. `CorrigirCusto` aceita o mesmo campo, de
propósito: se o Técnico esquecer de marcar, o Administrador corrige na hora de lançar,
senão a nota que ele tem na mão não teria onde entrar. Desmarcar **apaga as notas** na
mesma escrita — `gravarNotasFiscais` roda o `DELETE` e não reinsere nada, que é a direção
que `trg_nota_fiscal_declarada` não cobre de propósito.

⚠️ **Custo `0` é valor legítimo em todo o fluxo** (serviço sem peça, conserto em
garantia): os bindings são `gte=0`, nunca `required` — que rejeitaria zero em campo
numérico — e `ck_custo_item_valores` permite. Uma OS de custo zero conta como
`finalizada` normalmente: o que importa é a linha de `os_custo` existir, não o valor ser
positivo. `TestEncerrar/encerra com custo zero` e `TestCorrigirCusto/custo zero é aceito
e volta como zero` existem para travar isso.

## Custo itemizado por tarefa e notas fiscais em lista (migration `000012`)

O caso que quebrou o modelo antigo é trivial e comum: uma serra fita em que o rolamento
E a fita quebraram. Com `custo_manutencao` sendo uma coluna escalar, o Técnico somava as
duas peças de cabeça e digitava o total; o Administrador abria Custos Pendentes, via um
número único e não tinha contra o que conferir. E com `numero_nota_fiscal`/
`serie_nota_fiscal` sendo um par escalar, a segunda nota — duas compras em lojas
diferentes — não tinha onde entrar.

Duas tabelas filhas de `ordem_servico`, **sem vínculo entre elas**: `os_custo_item` (o
que o Técnico lança no encerramento) e `os_nota_fiscal` (o que o Administrador registra
na conferência). Não há FK ligando item a nota, e isso é decisão: uma nota pode cobrir as
duas peças e uma peça pode não ter nota nenhuma (estoque próprio), então qualquer
amarração 1:1 estaria errada metade das vezes. Se a conciliação item a item for pedida um
dia, ela entra como `os_custo_item.nota_fiscal_id` nullable, sem mexer no que existe.

⚠️ **A linha de custo é uma TAREFA, com os dois valores dela** — descrição, custo de
manutenção e custo de hora técnica na mesma linha. A primeira versão foi uma linha por
VALOR, com um ENUM `categoria_custo` dizendo de qual das duas grandezas ela era, e foi
descartada na tela: para lançar duas peças de uma OS de maquinário o Técnico tinha que
criar uma terceira linha só para a mão de obra, escolhendo a categoria num select, e o
formulário o **bloqueava** até ele fazer isso. Duas peças não são duas mãos de obra; duas
tarefas é que são.

⚠️ **Não existe mais "maquinário exige hora técnica".** A regra vinha de `custoHoraTecnico`
ser um campo escalar obrigatório; com uma linha por tarefa ela obrigava a inventar uma
tarefa só para carregar a mão de obra numa OS que trocou duas peças e não cobrou hora
nenhuma. Sem hora lançada o agregado é **zero** em maquinário (a coluna existe sempre lá) e
**NULL** nos outros dois tipos, que é o que `ck_custo_por_tipo` exige. `TestEncerrar` e
`TestCorrigirCusto` têm o subteste "maquinário sem hora técnica soma zero, não é erro".

- **`os_custo` não sumiu, e continua guardando os dois agregados** — eles são a soma da
  coluna correspondente dos itens, escrita pelo service na mesma transação. Isso contraria
  a seção 3.2 da modelagem e a exceção está justificada na própria migration: as duas
  colunas são lidas por `vw_os_finalizada`, `ListarOrdensServico` e
  `ListarHistoricoOsDaMaquina`, e derivá-las na leitura seria reescrever tudo isso para
  não mudar nada na tela. A trava contra divergência é o servidor **nunca** aceitar o
  total do cliente: o payload não carrega total, o service soma o que gravou, e
  `TestCorrigirCusto/duas notas na mesma OS persistem as duas` confere a soma no fim.
- **`tem_nota_fiscal` sobreviveu** e não virou "a lista está vazia": lista vazia com a
  declaração marcada é estado legítimo e frequente — é a própria fila de conferência do
  Administrador (`000011` existe justamente para separar os dois casos).
- **`uq_nota_fiscal_os` usa `NULLS NOT DISTINCT`** (Postgres 15+) porque série é opcional
  e, no `UNIQUE` comum, NULL nunca colide com NULL: a mesma nota sem série entraria
  quantas vezes o Administrador clicasse em salvar. Violação vira **409**, e o `case` de
  `ErrDadoDuplicado` no `switch` de `Custo` foi adicionado junto — até a `000011` esta
  rota era um `UPDATE` 1:1, sem como colidir, e sem esse ramo o Administrador levava 500.
- ⚠️ A correção do Administrador **substitui** a itemização inteira que o Técnico escreveu
  (ponto 2 da seção 6 da modelagem), mas o que ele lançou **não se perde mais**: está no evento
  `encerrada` de `os_evento`, e cada correção grava o seu `custo_corrigido` (ver "Linha do
  tempo da OS" abaixo).

`AtualizarCusto` grava `os_custo.custo_revisado_em = now()` de quebra: toda passagem do
Administrador por aqui É a conferência. `GET /ordens-servico` projeta a coluna como
`custo.revisadoEm`, e é ela que separa as pílulas **"Pendentes"** (ninguém conferiu) e
**"Revisadas"** em `AdministradorCustosPendentes` — antes era `localStorage`, a OS
trocava de aba só no navegador de quem salvou; agora vale para todos os Administradores.

O que já existe e NÃO precisa ser refeito: a criação da OS (`AbrirOS`, fase 1 — a OS
nasce da aprovação do Gestor, nunca de um `POST /ordens-servico`) e a **leitura**
(`GET /ordens-servico`), que já projetava encerramento, custo, horas e pausas antes
mesmo de essas linhas existirem.

Prontos e testados, fora da lista: os **indicadores de máquina**, o **job de preventiva
vencida** (falta só o Cron Job no Railway) — cada um com seção própria abaixo — e a **recuperação de senha por
e-mail** (migration `000013`; falta só `RESEND_API_KEY`/`URL_FRONTEND_FORMATO` em produção
e o domínio verificado no Resend — ver "Recuperação de senha" em `docs/api-e-rotas.md`).

**O que ainda não existe, de propósito ou por falta de pedido:** `GET /tecnicos/:id` (o
front não chama), aviso ao Técnico quando o **Gestor** abre uma OS para ele (só a OS de
preventiva avisa), reatribuir o Técnico de uma OS (ponto 7 da seção 6 da modelagem; a
auditoria que ele pedia já existe, ver "Linha do tempo" abaixo), revogação de token (`token_version`, ver "Revogação" em
`docs/api-e-rotas.md`) e rotação de backup antigo no R2.

Listagem nova que precise recortar por escopo usa `atorDaRota` no controller +
`escopoDe(usuarioId, perfil)` no service, com o `EXISTS` no `WHERE` — ver "Escopo no
`WHERE`" em "Queries e repository". Rota com arquivo usa `corpoMultipart`, sem arquivo usa
`corpoJSON`.

## Solicitações (fase 1, feito)

`sqlc → service → controller → rotas`, nessa ordem, cada camada testada contra Postgres
real antes de seguir pra próxima (histórico: PR #11, `radaptech/sistema-OSm--Back-end`).

- **As duas criações humanas** (`POST /solicitacoes/maquinario`, `/reparo`) sobem foto
  (obrigatória — é a evidência que o Gestor avalia antes de aprovar) e vídeo (opcional,
  só em `/maquinario`, até 8s/40MB — o teto é do servidor, o corte de duração é do front,
  `UploadVideo.tsx`) ANTES de abrir a transação, mesmo padrão de
  `MaquinarioInsert.FotoChave`: falhar o upload não deixa resíduo no banco.
  `CadastrarSolicitacaoMaquinario` valida que a máquina pertence ao **próprio setor** do
  Solicitante (`resolverSetorSolicitante`, via `ObterEscopoSessaoPorUsuario`) — mesma
  regra que já filtra o dropdown em `GET /maquinas`, aplicada de novo do lado da escrita
  contra um POST direto escolhendo máquina de outro setor.
- **`GET /solicitacoes/:id` tem escopo no `WHERE`** (`ObterSolicitacaoPorID`, mesmo
  `EXISTS` de `ListarSolicitacoes`/`ListarMaquinas`) — foi adicionado durante a fase 1
  depois de notar que a rota, aberta a qualquer perfil, deixaria um Solicitante enumerar
  id e ler foto/descrição de outro setor sem isso. Todo chamador manda
  `escopoDe(usuarioId, perfil)`, inclusive `AbrirOS`/`Rejeitar` — NULL só quando é
  administrador (que não tem escopo, a ausência É o acesso total).
- **`AbrirOS` devolve um `OrdemServico` deliberadamente incompleto** (sem `tecnicoNome`/
  `tecnicoArea`/`empresaTerceirizada*`, todos opcionais no contrato do front) — buscar
  esses JOINs agora seria refazer o trabalho que a fase 2 (Ordem de serviço, ver "O que
  falta") já vai precisar fazer direito, com `os_encerramento`/`os_custo` no meio.
- **`AnexoSolicitacao.Url` é `*string`, não `string`** — o front declara `url: string`
  sem `?` (sempre presente), mas se a assinatura da URL falhar no controller não tem como
  inventar uma: string vazia sairia como `""`, parecendo uma URL válida até a mídia
  tentar carregar. `null` é honesto sobre o que aconteceu; mesma folga de
  `Maquinario.FotoUrl`, só que sem `omitempty` (o campo continua sempre emitido).
- **`nivel_urgencia` virou ENUM** na migration `000007` — mesma lacuna e mesmo motivo de
  `nivel_criticidade` (000004): tupla fixa no front, sem tela de cadastro, tabela vazia
  travaria `POST /:id/abrir-os` em todo tenant. Ver "Migrations" e a nota em
  `docs/modelagem-banco-dados.md` (seção 2.4).
- Testado: `internal/service/solicitacaoOsIntegracao_test.go` (16 subtestes: as duas
  criações persistindo de verdade, as 4 recusas, escopo nas duas listagens e no
  obter-por-id, resumo, abrir-os e rejeitar) e
  `controller/solicitacaoController_test.go` (mapa erro→status, foto obrigatória,
  content-type recusado antes do R2, upload sem R2 configurado, validação de payload,
  ator sempre do token, chave crua nunca vazando na resposta).

## Ordem de serviço — listagem (`GET /ordens-servico`, feito)

Um endpoint para os **três** painéis; o que muda é o filtro que cada um manda:
Gestor sem filtro (abas "OS em Andamento"/"OS Finalizadas"), Técnico `?tecnicoId=`,
Administrador `?status=Concluída` (Custos Pendentes) e `?finalizada=true` (OS
Finalizadas). Array simples por padrão; com `?pagina=` vira `RespostaPaginada` de 12 (o histórico do Administrador e a
aba OS Finalizadas do Gestor). Para essa aba existem ainda `GET /ordens-servico/resumo` (contagem por
loja/setor, mesmos filtros e mesmo escopo — a soma é o `total` da lista, e há teste trancando isso) e
`GET /ordens-servico/:id` (uma OS no escopo, para o link de notificação). Os **filtros avançados** do
Gestor foram para o `WHERE`: `?maquina=` (nome ou patrimônio), `?dataInicio=`/`?dataFim=` (dia de
abertura, fuso de São Paulo), `?valorMinimo=`/`?valorMaximo=` (custo total lançado; OS sem custo não
combina). `?busca=` passou a olhar também o patrimônio.

Por cima disso as telas estreitam com `?busca=`, `?tipo=`, `?lojaId=` e `?setorId=`,
todos opcionais e cumulativos. **`?setorId=` recorta pelo setor da SOLICITAÇÃO de
origem** (`s.setor_id`) — `ordem_servico` não tem setor próprio. É a mesma coluna que o
`EXISTS` de escopo compara, então filtro e escopo falam da mesma coisa e o cliente nunca
amplia: gestor pedindo setor fora do escopo dele recebe lista vazia, não a do vizinho.

**Não existe `POST /ordens-servico`, e não é esquecimento**: a OS nasce de
`POST /solicitacoes/:id/abrir-os` (a aprovação do Gestor), do job de preventiva ou de
`POST /solicitacoes/direta` (ver "OS direta" abaixo) — os três criam a solicitação antes. `uq_os_solicitacao` garante
que toda OS vem de uma solicitação, e criar direto pularia a aprovação — que é o ponto do
fluxo. Nenhum teste insere em `ordem_servico` na mão: todos passam por `AbrirOS`.

Escopo no `WHERE` via o mesmo `EXISTS` de `ListarSolicitacoes`, sobre o setor da
**solicitação de origem** — `ordem_servico` não tem `setor_id` próprio, e nem deveria: a
OS é da solicitação, não de um lugar.

- ⚠️ **`?status=` vem separado por VÍRGULA, não repetido.** `montarQuery` no front faz
  `busca.set(chave, valor.join(','))` para todo array, então `?status=Aberta,Em Andamento`
  é uma chave só. `ctx.QueryArray` devolveria um item com a vírgula dentro e o cast
  `::status_os` estouraria em 22P02 — 500 numa tela que só queria filtrar. O parse é
  `strings.Split` + validação item a item contra `statusOsValidos` (400, nunca 500).
- ⚠️ **O parâmetro `status` entra como `text[]` e só vira `status_os` dentro do `ANY`.**
  Como `status_os[]` direto o pgx não acha plano de encode (`unknown type (OID ...):
  cannot find encode plan`) — ele conhece os arrays built-in, não um ARRAY de ENUM nosso,
  e registrar o tipo custaria um `AfterConnect` em `config/conn.go` por enum. O cast
  **volta** para `status_os` antes de comparar, senão `idx_os_tecnico_status` para de valer.
- ⚠️ **`horas_*` e `custo_*` são projetadas CRUAS, sem `::float8`** — e isso é o oposto do
  reflexo. O `::float8` faz duas coisas ruins de uma vez: o sqlc perde o vínculo com a
  coluna (então o override do `sqlc.yaml` deixa de casar, porque ele casa por NOME DE
  COLUNA) **e** a expressão passa a ser tipada como NOT NULL — sai `float64` e o `Scan`
  quebra no primeiro NULL, que é o caso comum (OS aberta não tem horas nem custo).
  Coluna crua + override para `pgtype.Float8` resolve os dois. `pointer: true` não serve:
  ele só vale onde o sqlc **já** concluiu que é nullable.
  As quatro são NULL em estado legítimo: `horas_*` só existem em OS encerrada
  (`vw_os_horas` é INNER em `os_encerramento`), `horas_parada` some também quando
  `afeta_producao` é falsa (o front exibe "Não se aplica", que **não** é zero), e
  `custo_hora_tecnico` é nulo em reparos anteriores à `000017` (até ali `ck_custo_por_tipo` o proibia). Hoje todo tipo de OS lança, por tarefa, Valor Peças (`custo_manutencao`) e Valor Mão de Obra (`custo_hora_tecnico`); em terceiros (`000016`) a mão de obra é da EMPRESA.
- ⚠️ **O override de `numeric` no `sqlc.yaml` nunca casou nada** — o `db_type` correto é
  `pg_catalog.numeric`, não `numeric`. É por isso que `shopspring/decimal` não está no
  `go.mod` e `models.go` seguia com `pgtype.Numeric`. Nunca doeu porque nenhuma query
  tocava coluna `numeric` antes desta. Deixado como está de propósito: consertá-lo traria
  `decimal.Decimal`, que serializa como **string** em JSON contra um front que tipa
  `number`. A **escrita** do custo (`CriarCusto`/`AtualizarCusto`) não precisou dele:
  as duas colunas já tinham override próprio por NOME (`*.custo_hora_tecnico`/
  `*.custo_manutencao`, ambos `pgtype.Float8`, casando em qualquer tabela/query), então
  esse override de `numeric` genérico segue morto do mesmo jeito — nenhuma query nova
  precisou dele.
- ⚠️ **`vw_os_horas` devolve `numeric`** mesmo sem coluna numeric envolvida:
  `EXTRACT(EPOCH ...)` retorna numeric desde o Postgres 14.
- **`area_tecnico` é LEFT JOIN aqui, INNER em `ListarTecnicos`.** Lá o `WHERE` já garante
  `perfil = 'tecnico'` e `ck_usuario_area_tecnico` exige a coluna. Aqui não: `fk_os_tecnico`
  aponta pra `usuario` sem checar perfil, e `AtualizarUsuario` zera `area_tecnico_id` ao
  tirar alguém do perfil técnico. Com INNER, **promover a gestor um técnico com OS aberta
  apagaria essas OS da listagem inteira**, calado — e é o Gestor quem olha a listagem. Há
  teste trancando isso.
- **`finalizada` é derivado, não coluna** (encerramento MAIS custo lançado). A expressão
  aparece **duas vezes** — projeção e filtro — porque o Postgres não deixa referenciar
  alias do SELECT no `WHERE`; divergir as duas dá uma listagem que se contradiz, e há
  teste conferindo. `vw_os_finalizada` não serve: ela é `JOIN os_custo` e só devolve as
  finalizadas. O `::boolean` no fim é obrigatório (sem ele vira `*bool`, mesma armadilha
  do `vencida` em `preventiva.sql`).
  ⚠️ A fila "Custos Pendentes" do Administrador é **`?status=Concluída`**, não
  `?finalizada=false`: ela lista toda OS concluída, com ou sem custo, porque virou fila de
  conferência contra a nota fiscal.
- **`custoTotal` é somado no model, não no SELECT** — uma expressão a mais na query seria
  mais uma chance de cair na armadilha do numeric, e a conta é uma soma.
- **`os_pausa` vem por query separada** (`ObterPausasDasOrdensServico`, em lote): 1:N no
  JOIN duplicaria a OS por pausa. Mesmo desenho de `ObterAnexosDasSolicitacoes`.
  `pausaAtual` é a de `retomada_em` nulo (`uq_pausa_aberta` garante no máximo uma) e vem
  **repetida** dentro de `pausas` — não substitui o histórico.
- **`model.OrdemServico` é uma struct só para os dois caminhos** (`GET /ordens-servico` e
  `POST /:id/abrir-os`), porque o front também tipa uma só: tudo que a OS recém-aberta não
  tem ainda é opcional no contrato. Quem monta a completa é `MontarOrdemServico`; a da
  abertura é `MontarOrdemServicoDaAbertura`.
- Sem R2 nesta rota: o tipo `OrdemServico` do front não tem campo de mídia — a foto do
  defeito é da **solicitação**, e é lá que o modal de detalhes vai buscá-la.
- Testado: `internal/service/ordemServicoIntegracao_test.go` (escopo dos 5 perfis, filtros
  combináveis, `finalizada` nos 4 estados, nulo como estado legítimo, **os dois relógios**
  — solicitada 8h atrás, iniciada 5h, 1h de pausa → trabalhadas ~4h e parada ~8h, o que
  tranca a migration `000002`), `internal/model/ordemServico_test.go` (serialização, campos
  omitidos, `custoTotal`, pausas) e `controller/ordemServicoController_test.go` (mapa
  erro→status, vírgula no `status`, filtros chegando no service, ator sempre do token).
  ⚠️ O teste do controller monta a query com `url.Values`, não string crua:
  `httptest.NewRequest` **panica** com o espaço de `"Em Andamento"` sem encoding.

## Indicadores de máquina (`GET /indicadores/maquinas/:id`, feito)

O Painel de Indicadores do Gestor (`DashboardGestor`, a ação rápida "Indicadores"):
Horas Parada, MTTR, MTBF e Custo Total da máquina, mais a rosca de paradas por tipo de
defeito e as barras de custo mensal dos últimos 12 meses. Tudo sai do histórico de OS
**encerradas** daquela máquina — `ListarHistoricoOsDaMaquina`, uma linha por OS.
Cada item de `porMes` repete as mesmas grandezas (`ResumoIndicadores`) calculadas só
com as OS encerradas naquele mês: o painel troca os cards e a rosca pelo mês clicado.

**Não responde mais só zeros** desde que o ciclo de vida da OS ficou completo
(`/encerrar` grava `os_encerramento`/`os_custo`, `/custo` corrige o segundo depois) — os
números aparecem sozinhos a partir da primeira OS encerrada de cada máquina, sem tocar
aqui. Ficou pronto e testado contra linhas inseridas na mão ANTES disso existir, e
continua valendo: nada nesta seção mudou com o ciclo de vida, só passou a ter dado de
verdade por trás.

**Por loja (`GET /indicadores/lojas/:id`, feito):** a tela da loja no mesmo painel —
o total, um item por setor e um por máquina (`IndicadoresLoja`), cada um com o próprio
`porMes`, porque o mês clicado no gráfico da loja filtra todos os cards da tela. Sai de
`ListarHistoricoOsDaLoja` (a mesma linha por OS, com `maquina_id`/`setor_id`, e o EXISTS
de escopo de `ListarMaquinas`) e de `ListarMaquinas` (para toda máquina/setor ganhar
card, zerado quando não tem OS). Os dois recortes usam o MESMO `resumir`
(`MontarIndicadoresLoja`): o total da loja tem que bater com a soma do que a tela mostra.
- ⚠️ **A OS de preventiva fica FORA do MTBF e do MTTR** (`origem = 'preventiva'`, corrigido em 15/10/2026).
  Os dois medem falha e conserto, e a revisão programada não é nenhum dos dois: contada, o MTBF encolhia a
  cada revisão (a máquina "falhava" mais só por receber manutenção em dia) e o MTTR era puxado pelo tempo de
  uma revisão curta -- o indicador punia quem fazia a manutenção em dia. **Horas, custo e a contagem
  (`quantidadeOs`) continuam contando a preventiva**: o dinheiro gasto na revisão é real. O corte está em
  `resumir`/`mtbf` (`model/indicadorMaquina.go`), alimentado por `s.origem` nas duas queries de histórico;
  vale no total, no recorte por mês, por setor e por loja (mesmo `resumir`). A OS direta continua entrando:
  é o Gestor abrindo conserto sem fila. Pequeno reparo (sem máquina) segue fora dos indicadores de máquina.
  Testes: `indicadorMaquina_test.go` e `indicadorPreventivaIntegracao_test.go`.
- **Pequenos reparos têm custo próprio, SEPARADO do maquinário** (15/10/2026). O reparo não tem máquina, então
  continua fora dos indicadores por máquina, do MTBF, do MTTR e das horas; mas o custo dele aparece na tela da
  loja (`GET /indicadores/lojas/:id`) em campos próprios, `quantidadeReparos` e `custoReparos`, no total da loja,
  em cada setor e em cada mês (`porMes`). **`custoTotal` e `quantidadeOs` continuam sendo só do maquinário**:
  o Gestor vê os dois lado a lado e nunca somados num número só. Vêm de `ListarHistoricoReparosDaLoja`: reparo
  é a solicitação sem máquina (`maquina_id IS NULL`), então um reparo que o Técnico terceirizou continua
  reparo; o setor é o da solicitação; o escopo do Gestor vale (EXISTS sobre o setor). Um setor que só teve
  reparo (sem máquina cadastrada) ganha card, para explicar o custo que está no total. Nas máquinas os dois
  campos vêm zerados. Testes: `indicadorMaquina_test.go` e `indicadorReparosIntegracao_test.go`.
  **A lista por trás de cada card:** `GET /indicadores/lojas/:id/ordens?grupo=maquinario|reparos`
  (`ListarOrdensMaquinarioEncerradasDaLoja` / `ListarReparosEncerradosDaLoja`) devolve as OS encerradas que
  compõem o número, paginadas (12), mais novas primeiro, com item (a máquina ou o que o Solicitante digitou),
  patrimônio, origem, setor, técnico, data de encerramento e custo. O **maquinário inclui a preventiva** (o custo
  dela está no card; a lista a marca pela origem) e aceita `?maquinaId=` (a tela da máquina individual usa); o
  filtro de máquina é recusado em reparos (400). Os dois aceitam `?setorId=` e `?mes=MM/AAAA` (o rótulo do
  gráfico; o **mês de encerramento**, em São Paulo — não o da abertura, senão a lista e a barra discordariam).
  Mesma definição e mesmo escopo do painel (loja fora do escopo é 404; só máquina ativa; setor ATUAL da máquina),
  então **a soma dos itens é o `custoTotal` / `custoReparos` do card** (há teste dos dois). É endpoint próprio e
  não `GET /ordens-servico` porque o reparo terceirizado vira `tipo = 'terceiros'` e ficaria de fora, e porque
  aquela listagem filtra o mês pela abertura e define "finalizada" (encerrada **e com custo lançado**) de outro
  jeito que o painel (encerrada). A aba **OS Finalizadas** do Gestor continua sendo a ferramenta operacional
  (buscar, imprimir, filtrar por período e valor); a lista do painel é para explicar o número.
- **MTBF de grupo só mede entre OS da mesma máquina** (`mtbf` agrupa por `maquinaId`):
  entre OS consecutivas do setor ele mediria o espaçamento entre falhas de máquinas
  diferentes, que encolhe a cada máquina cadastrada sem nenhuma quebrar mais. Para uma
  máquina só, é a conta de antes. MTTR e custo/horas são médias e somas diretas.
- **Loja fora do escopo é 404** (checado contra `usuario_escopo` antes das listas), pelo
  mesmo motivo da rota de máquina; o gestor com só alguns setores da loja recebe a loja
  como a soma desses setores — o `WHERE` recorta, não o front.
- `quantidadeOs` faz parte de `ResumoIndicadores` (total e cada mês): é o "em N OS" dos
  cards da loja.

- **A agregação é em Go, não no `SELECT`** (`MontarIndicadoresMaquina`, em
  `internal/model/indicadorMaquina.go`). Seis grandezas seriam três `GROUP BY` numa
  query só, e o MTBF (média do intervalo entre aberturas) ainda pediria `LAG`. Em Go são
  três laços sobre uma lista que cabe na memória, e a matemática fica testável **sem
  Postgres** (`internal/model/indicadorMaquina_test.go`). Mesmo espírito do `custoTotal`
  de `ListarOrdensServico`, que também é somado no model.
- **O `JOIN os_encerramento` É o filtro de "concluída"** — a linha só existe depois que o
  Técnico encerrou (`uq_encerramento_os`) e não há reabertura no modelo. `status =
  'Concluída'` seria o espelho denormalizado da mesma coisa; se um dia divergirem, manda
  a linha de encerramento.
- ⚠️ **Nulo conta como ZERO aqui, o oposto do que a listagem faz.** Em
  `GET /ordens-servico` `horas_parada` nula vira `null` e a tela escreve "Não se aplica";
  num gráfico não dá pra desenhar ausência. **Menos no MTTR**: OS sem `horas_trabalhadas`
  fica fora do divisor, senão a média ganharia um conserto instantâneo que não aconteceu.
- ⚠️ **O mês vem pronto do banco, em `America/Sao_Paulo`** (`to_char(... AT TIME ZONE
  ...)`, mesmo tratamento de `vencida` em `preventiva.sql`): OS encerrada às 22h de 31/08
  é agosto pro Gestor e setembro pro UTC. Formato `YYYY-MM` porque **ordena como texto** —
  o `MM/YYYY` do contrato é montado só na hora de responder. Há teste trancando isso.
- **Máquina fora do escopo é 404, não 200 com zeros.** O escopo entrou como `narg`
  opcional em **`ObterMaquinaPorID`** (NULL não filtra — é o administrador e todos os
  chamadores de escrita, que por isso não mudaram uma linha), e o service o chama **antes**
  da query de histórico. As duas coisas são indistinguíveis pela query sozinha: máquina de
  outra loja e máquina sem OS encerrada devolvem zero linhas. Mesma lacuna que
  `ObterSolicitacaoPorID` fechou na fase 1.
- **Máquina existente e sem histórico devolve zeros com as duas listas montadas** —
  `porTipoDefeito` sempre com os dois tipos (a rosca tem legenda fixa; fatia que some é
  lida como "não existe esse defeito") e `porMes` como array vazio, nunca `null`.
- Mora no `OrdemServicoService`/`OrdemServicoController`, não num service próprio: tudo
  que lê é histórico de OS. A URL é `/indicadores/...` e não `/maquinas/:id/indicadores`
  porque é a que o front já chama.
- Testado: `internal/model/indicadorMaquina_test.go` (a matemática, sem banco: MTTR
  ignorando nulo, MTBF precisando de duas OS, corte de 12 meses com meses fora de ordem) e
  `internal/service/indicadorIntegracao_test.go` (a query contra Postgres: OS aberta não
  entra, os dois relógios, o mês em BRT, escopo → 404, administrador sem escopo).

## Linha do tempo da OS (`os_evento`, migration `000021`, feito)

Responde "quem fez, e quando" para cada passo da OS — e preserva o que a correção de custo
apagaria. `GET /ordens-servico/:id/historico` devolve os eventos do mais antigo ao mais novo.

- **Um evento por passo, na transação do passo** (`registrarEvento`, `ordemServicoEvento.go`):
  `aberta` (em `criarOrdemServico`, então vale para abrir-os e direta, e no job de preventiva),
  `iniciada`, `pausada` (`detalhes.motivo`), `retomada`, `terceiro_acionado` (id **e nome** da
  empresa, que pode ser renomeada depois), `encerrada` e `custo_corrigido`. Passo recusado não
  deixa evento: ele commita junto com a mudança de estado ou nenhum dos dois.
- **Dinheiro: guarda o estado DEPOIS do passo.** `encerrada` e `custo_corrigido` levam em
  `detalhes` o custo inteiro (itens, notas, totais, `temNotaFiscal`). A versão anterior de uma
  correção é o evento anterior, então o lançamento original do Técnico — que `CorrigirCusto`
  apaga de `os_custo_item`/`os_nota_fiscal` — fica no `encerrada`, sem um evento extra "de
  lançamento". A tela calcula o "de → para" comparando snapshots consecutivos.
- **Append-only de verdade:** `trg_os_evento_imutavel` recusa `UPDATE`. `DELETE` fica livre só
  porque `ExcluirMaquinaDefinitivo` apaga a máquina com todo o histórico (`os_evento` entrou na
  CTE, antes de `os_custo_item`).
- **`usuario_id` nulo só em `aberta`** da OS de preventiva — ninguém abriu, foi o calendário
  (`ck_os_evento_usuario`, mesmo critério de `aberta_por_id`).
- **Quem lê:** gestor (escopo no `WHERE`, sobre o setor da solicitação), administrador e o
  técnico dono. Fora do alcance é 404, como OS inexistente. **Solicitante não lê**: acompanha o
  pedido, não a execução. A checagem é `ObterOrdemServicoNoEscopo`, query à parte.
- **Retroativo:** a migration reconstituiu eventos das OS existentes e os marcou
  `detalhes.retroativo = true`. Abertura e encerramento têm autor gravado; iniciada, pausa,
  retomada e terceiro usam o técnico da OS — **autoria presumida**, e a tela escreve isso.
  Correções de custo antigas não foram inventadas.
- **Não substitui `os_pausa`:** as horas continuam saindo de `vw_os_horas`. Se um dia a
  reatribuição de técnico existir (ponto 7 da modelagem), é um tipo novo de evento.
- Testado: `ordemServicoEventoIntegracao_test.go` (ordem e autor de cada passo, o lançamento
  do Técnico sobrevivendo à correção, motivo e nome da empresa, escopo dos 6 perfis, recusa sem
  evento, append-only, abertura sem autor), mais a checagem da abertura em
  `preventivaJobIntegracao_test.go` e `solicitacaoDiretaIntegracao_test.go`.

## Notificações (central no app + Web Push)

**O WhatsApp saiu em 13/10/2026.** A notificação existiu via Evolution API self-hosted
(WhatsApp Web por baixo, número dedicado banível, nunca chegou a ter o chip pareado em
produção). Saiu por não escalar: um número só pra todos os tenants, risco de ban
derrubando todo mundo junto, mensagem sem ação (o destinatário ainda tinha que abrir o
sistema e achar a OS) e nenhum registro do que foi visto. O substituto é o próprio app
como **PWA**: central de notificações dentro do sistema + Web Push no celular/desktop.

**Central de notificações (feita, migration `000018`).** Notificar é gravar **uma linha
por destinatário** em `notificacao` — "lida" é de cada um, e é o que o sininho e o badge
do ícone contam. `titulo`/`corpo` são o texto pronto do momento do evento (não remontados
na leitura) e `url` é o destino do clique (o painel abre o modal daquele item). Rotas em
`docs/api-e-rotas.md`.

| evento | onde dispara | quem recebe | `tipo` | `url` |
|---|---|---|---|---|
| solicitação nova (do Solicitante) | `CadastrarSolicitacaoMaquinario`/`Reparo` | gestores do setor¹ | `solicitacao_nova` | `/painel-gestor?solicitacao=N` |
| gestor aceitou (abriu a OS da fila) | `AbrirOS` | o Solicitante | `solicitacao_aceita` | `/minhas-solicitacoes?solicitacao=N` |
| gestor rejeitou | `Rejeitar` | o Solicitante (com o motivo) | `solicitacao_rejeitada` | `/minhas-solicitacoes?solicitacao=N` |
| gestor abriu OS (da fila ou direta) | `AbrirOS`, `CadastrarSolicitacaoDireta` | o Técnico designado | `os_aberta` | `/painel-tecnico?os=N` |
| preventiva vencida (job) | `abrirSolicitacaoDaPreventiva` | o Técnico da preventiva | `os_preventiva` | `/painel-tecnico?os=N` |
| técnico encerrou a OS (qualquer origem) | `OrdemServicoService.Encerrar` | gestores do setor¹ | `os_concluida` | `/painel-gestor?os=N` |

¹ todo gestor ativo cujo escopo alcança o setor (`ObterGestoresDoSetor`). Administrador
não recebe — não tem escopo, e o painel dele não é de acompanhamento do dia a dia.

**Não notificam, de propósito:** iniciar, pausar, retomar e acionar terceiro (aviso a cada
passo da execução vira ruído e ninguém lê mais nenhum); a direta não avisa gestor nem
"aceita" (quem pediu é o próprio gestor); o `AbrirOS` avisa o Técnico **antes** do
Solicitante, na mesma goroutine — o técnico é quem tem trabalho a fazer.

**Como está montado:**
- `NotificadorInterface` (um método por evento) + `DadosNotificacao` é o que
  `SolicitacaoService`/`PreventivaService`/`OrdemServicoService` conhecem; `NotificacaoService` (`notificacaoService.go`) é a
  implementação e também o service das rotas. Todo envio passa por `entregar` — é ali
  que o Web Push entra, **depois** da gravação: o push é só o aviso, a notificação de
  verdade é a linha, que o sininho mostra mesmo se o push nunca chegar.
- O campo público `Notificador` nos três services (não parâmetro de construtor — mudar
  a assinatura quebraria todo teste que chama `NewRepoX(pool)` direto; `nil` = não
  notifica), chamado sempre **depois** do commit via `emSegundoPlano` (goroutine com
  `context.Background()` + timeout de 15s — nunca o `ctx` da request, que morre quando a
  resposta é escrita; falha só vai pro log). `Alvo` sai de `alvoDaSolicitacao`/`alvoDaOS`;
  nos eventos de OS, `dadosDaOS` monta tudo da própria resposta da rota, sem query a
  mais (por isso `criarOrdemServico` passou a preencher `tecnicoNome` na resposta do
  abrir-os — o técnico já era lido pra validar).
- `ObterGestoresDoSetor` não filtra mais por telefone (era critério do WhatsApp).

**Web Push (migration `000019`).** O aparelho se inscreve
(`POST /push/inscricoes`, a `PushSubscription` do navegador) e vira uma linha em
`dispositivo_push`. `entregar` grava as notificações e, **depois**, manda um push cifrado pra
cada aparelho dos destinatários (`enviarPush`), com `MensagemPush` = `{ id, tipo, titulo,
corpo, url, naoLidas }` — o `id` é da notificação daquele usuário (o service worker marca como
lida no toque) e `naoLidas` já conta esta (vira o número no ícone). O lado do navegador
(botão "Ativar" no sininho, `sw.js`, logout desinscrevendo) está em
`../sistema-OSm--Front-end/CLAUDE.md`, "Web Push".
- **Urgência**: `solicitacao_nova`, `os_aberta` e `os_preventiva` vão `Urgency: high` (pedem
  ação; é o que faz o Android entregar em economia de bateria); aceita/rejeitada/concluída,
  `normal`. **TTL de 24h**: aparelho desligado mais que isso perde o push, não a notificação.
- **Inscrição morta** (serviço de push responde 404/410: desinstalou, revogou permissão,
  limpou o navegador) é apagada na hora. Outras falhas vão pro log e não param os outros
  aparelhos.
- ⚠️ **SSRF**: o endpoint é uma URL que o cliente manda e o servidor chama. Só passa host dos
  serviços de push dos navegadores (`hostsPush` em `pushService.go`: FCM, Mozilla, Apple,
  Windows), sem porta, sem userinfo, `https`; checado ao salvar **e** ao enviar, e o client
  HTTP **não segue redirect**. Navegador com outro serviço de push entra na lista.
- **Chaves VAPID**: `VAPID_PUBLIC_KEY`/`_PRIVATE_KEY`/`_SUBJECT` (`make chaves-vapid`). Sem
  elas `NewWebPush` devolve nil, o router loga "Web Push desligado" e o sininho segue
  funcionando sozinho. Trocar o par invalida toda inscrição existente.
- ⚠️ A tabela só cresce. Não há limpeza ainda; se pesar, um job apagando lidas com mais
  de N dias resolve (a paginação e o índice parcial de não lidas já seguram a leitura).

## OS direta — Gestor/Administrador abre sem fila (`POST /solicitacoes/direta`, feito)

Pedido dos gestores: quando o Solicitante não está disponível, quem aprova abre a OS
pelo próprio perfil. Gestor e Administrador, mesmos requisitos. Mesmo desenho da
preventiva: **solicitação e OS nascem juntas** numa transação (`CadastrarSolicitacaoDireta`),
a solicitação com `origem = 'direta'` e status `Convertida`, `solicitante_id` = quem abriu.

- **Origem própria (migration `000014`) por causa da foto.** `fn_check_solicitacao_tem_foto`
  cobra anexo de `origem = 'solicitante'`; a foto existe para o Gestor avaliar antes de
  aprovar, e aqui quem abre é quem aprovaria. Sem foto, corpo JSON (sem multipart).
- **Avisa só o Técnico**: a direta não chama `notificar` nem avisa "aceita" — avisar o
  gestor do que ele mesmo abriu é ruído. O Técnico recebe `os_aberta`, igual ao abrir-os
  da fila (ver "Notificações").
- **Corpo**: `tipo` (`maquinario`/`reparo`), `maquinaId` (maquinário — o setor sai da
  máquina) ou `item` + `setorId` (reparo), `descricao`, `impactos` (só maquinário),
  `urgencia`, `tecnicoId`. Técnico é obrigatório. Resposta é a `OrdemServico`, igual ao
  abrir-os.
- **Escopo**: o service insere e relê com `ObterSolicitacaoPorID` + `escopoDe(ator)`; fora do
  escopo a releitura não acha a linha, volta 400 e o rollback leva o INSERT junto. Zero
  query nova.
- **Técnico tem que atender a loja da solicitação** (`TecnicoAtendeLoja`) — vale para a
  direta **e para o abrir-os da fila**, porque os dois passam por `criarOrdemServico`
  (`solicitacaoOsHelpers.go`). Antes o abrir-os aceitava técnico de qualquer loja; agora
  é 400. O select do front já filtra por `GET /tecnicos?lojaId=`.
- Não checa setor desativado nem pede motivo ("solicitante indisponível") — ninguém pediu.
- Testado: `internal/service/solicitacaoDiretaIntegracao_test.go`.

## Abertura automática de OS por preventiva (feito; falta o Cron no Railway)
Ao vencer a `proxima_data` de uma preventiva **ativa**, o sistema abre a **Solicitação e
a Ordem de Serviço juntas**, na mesma transação. A solicitação nasce com
`origem = 'preventiva'`, `preventiva_id` preenchido, `solicitante_id` **nulo** (não houve
pessoa) e status **`Convertida`**; a OS nasce `Aberta` no nome do técnico que a própria
preventiva carrega.

⚠️ **Preventiva não passa pela fila do Gestor (mudou na migration `000008`).** Até então
a solicitação nascia `Pendente` e esperava aprovação. O trabalho, porém, já foi aprovado
quando a máquina foi cadastrada — procedimento, intervalo e data saíram de lá —, então a
segunda aprovação a cada ciclo não decidia nada, só atrasava. O Gestor continua vendo
tudo pelas abas de **OS em Andamento** e **Manutenção Prev.**; ele só perdeu o passo de
clicar em "Abrir OS".

- **`GET /ordens-servico` devolve `origem`** (`solicitante`/`direta`/`preventiva`, de `solicitacao_os.origem`): é o que as telas usam para marcar a OS de preventiva em âmbar, em vez de deduzir pela ausência de solicitante. A resposta do abrir-os e da OS direta também a traz.
- **A solicitação continua existindo, e isso não é cerimônia.** `horas_parada` é medida
  desde `solicitacao_os.criado_em` (`vw_os_horas`), `uq_os_solicitacao` exige uma
  solicitação por OS, e o card do Técnico busca a origem para mostrar o problema. Tirá-la
  quebraria as três coisas de uma vez.
- **Quem escolhe o técnico é o Administrador, no cadastro** (`preventiva.tecnico_id`). O
  job não sorteia: preventiva é trabalho recorrente e previsível, e sortear por área e
  loja exigiria uma regra de desempate que ninguém pediu, além de poder cair em alguém
  afastado. O service recusa técnico que não existe, que não é mais técnico ou que está
  desativado — a FK garante só que é usuário do tenant.
- **`urgencia` é sempre `Baixa` e `aberta_por_id` é `NULL`**, os dois literais na query
  (`CriarOrdemServicoDePreventiva`). Preventiva é trabalho planejado com data marcada, e
  ninguém abriu a OS — foi o calendário. Inventar um ator gravaria autoria falsa na
  coluna que existe para responder "quem abriu"; por isso a `000008` tirou o `NOT NULL`.
- **`afeta_producao` é `false`**, então a OS de preventiva não acumula horas de máquina
  parada e a tela escreve "Não se aplica". Não é decisão nova: a solicitação de preventiva
  nunca teve linha em `solicitacao_impacto` (não há Solicitante para marcar). Se um dia a
  preventiva precisar parar a máquina de propósito, isso vira um campo do cadastro.
- ⚠️ **Consequência assumida: sumiu o freio humano.** Antes, uma preventiva vencida ficava
  parada esperando o Gestor e não disparava de novo. Agora o ciclo avança sempre, então
  preventiva de intervalo curto com técnico lento acumula OS abertas em cima do mesmo
  técnico. Há subteste cobrindo isso (`ciclo seguinte abre outra OS`) — se virar problema,
  o lugar de resolver é o cadastro (intervalo maior) ou uma regra nova de "não abre com a
  anterior ainda em aberto", que hoje não existe de propósito.

**Implementado e testado (28/08/2026, revisto em 04/09/2026)**, ponta a ponta contra
Postgres de verdade: `ListarPreventivasVencidas` e `ObterPreventivaVencidaParaAbertura`
(`preventiva.sql`), `CriarSolicitacaoPreventiva` e `CriarOrdemServicoDePreventiva`
(`solicitacao_os.sql`), `AbrirSolicitacoesDePreventivasVencidas` +
`abrirSolicitacaoDaPreventiva` (`preventivaService.go`), `cli_preventivas_vencidas.go`
e `make preventivas-vencidas`. Falta **só** criar o Cron Job no Railway.

Desde a fase de notificação (ver seção própria acima), `abrirSolicitacaoDaPreventiva`
também chama `notificarPreventivaVencida` no fim — mesmo `Notificador` opcional de
`SolicitacaoService`, mesmo motivo de rodar em goroutine (uma preventiva com envio
lento não pode atrasar as outras 200 no mesmo laço). ⚠️ **Quem recebe é o técnico
designado, não os gestores do setor** (`NotificarOSPreventiva`): a OS já nasce atribuída,
então o Gestor não tem ação pendente e a mensagem seria ruído diário — o caminho mais
curto para ninguém mais ler a notificação de solicitação de verdade.

- **A migration `000005` destravou isso.** `fn_check_solicitacao_tem_foto` exigia foto em
  *toda* solicitação, e a de preventiva não tem nem como ter — ninguém fotografou nada.
  O `INSERT` do job falhava no commit com "precisa de ao menos um anexo do tipo foto".
  Agora a exigência vale só para `origem = 'solicitante'`. O corte usa `origem` e não
  `solicitante_id` porque `ck_origem` já amarra os dois.
- **Onde o job mora: não no banco.** `pg_cron` precisa de `shared_preload_libraries`, que
  o Postgres gerenciado do Railway não deixa configurar no Hobby — e regra de negócio em
  job de banco fica fora do teste e do CI. Ficou **subcomando de CLI**, como
  `provisionar-admin` e `backup-banco`. Consequência boa: **não há nada do Railway dentro
  do código** — sair de lá é reapontar quem dispara (cron de VPS, `CronJob` de k8s,
  GitHub Actions com as `DB_*` nos secrets, que funciona porque o Supabase é alcançável
  pela internet). Ticker em goroutine dentro da API continua sendo a saída de emergência:
  acopla o job ao uptime, mas duplicar execução aqui é inofensivo por construção (ver o
  índice abaixo) — o aviso vale mesmo é pro `backup-banco`.
- **Uma transação por preventiva**, não uma para o lote: uma linha ruim não pode derrubar
  as outras 200. Dentro dela, o `INSERT` e o `AvancarProximaData` são atômicos **entre
  si** — separados, avançar a data com o INSERT falhando pularia o ciclo em silêncio, e
  inserir sem avançar faria a preventiva disparar de novo no instante em que o Gestor
  convertesse a solicitação. As falhas voltam juntas num `errors.Join`: erro não-nil do
  job é resultado **parcial**, não fracasso.
- ⚠️ **A proteção contra ciclo duplicado mudou de lugar na `000008`.** Era o índice único
  parcial `uq_preventiva_pendente` (`solicitacao_os (preventiva_id) WHERE status =
  'Pendente'`), e ele parou de valer no instante em que a solicitação passou a nascer
  `Convertida` — o filtro nunca mais casa. Ampliar o filtro também não serve: uma
  preventiva **pode** ter várias solicitações ao longo do tempo, uma por ciclo, então
  nenhum `UNIQUE` sobre `preventiva_id` sozinho resolve. Quem protege agora é o
  `SELECT ... FOR UPDATE` de `ObterPreventivaVencidaParaAbertura`, dentro da transação de
  cada preventiva: a segunda réplica fica bloqueada até o commit da primeira, lê a data já
  avançada e sai sem escrever. É mais forte que o índice, porque cobre o INSERT **e** o
  `AvancarProximaData`, não só o INSERT. O `NOT EXISTS` da query de varredura saiu junto,
  pelo mesmo motivo.
- ⚠️ **`m.ativa` na query é obrigatório e esse SIM está trancado por teste.**
  `DesativarMaquina` não desativa as preventivas da máquina, então sem esse filtro máquina
  desativada abriria solicitação a cada ciclo, para sempre, sem jeito de parar pela API
  (não existe reativação nem `DELETE` de linha).
- **Configurar o Cron Job no Railway** (o que falta): mesmo repo, **Dockerfile Path
  `dockerfile`** (produção, não o `.dev`), **Custom Start Command `./main
  preventivas-vencidas`** — o comando inteiro, mesma armadilha do Cron-BACKUP (ver "Backup
  do banco"). Variáveis: as `DB_*` + `DB_SSLMODE`; **não precisa das `R2_*`**. **Mais `VAPID_PUBLIC_KEY`/`_PRIVATE_KEY`/`_SUBJECT`, as mesmas da API**, se quiser que o aviso
  chegue também como push no celular: sem elas o job grava a notificação (o sininho) e só não manda o push.
  ⚠️ O job é um processo à parte da API e **monta o próprio notificador**
  (`NewNotificacaoDoAmbiente`, também usado pelo router) e espera os avisos terminarem
  (`AguardarAvisos`) antes de sair. Antes de 15/10/2026 ele não ligava o `Notificador` e abria a OS sem
  avisar o técnico, e sem a espera o processo morria antes de a goroutine gravar a notificação. Frequência: 1×/dia de manhã (`0 9 * * *` UTC = 06:00 BRT) — preventiva
  vence no dia, não na hora, e rodar mais vezes não duplica nada, só não adianta.
- O job **não roda migrations**: quem faz isso é o boot da API. O container do cron sobe o
  mesmo binário contra o mesmo banco já migrado.
