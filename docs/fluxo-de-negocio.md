# Fluxo de negócio: o que está pronto e o que falta

O estado de cada fase e o porquê das decisões de negócio já tomadas.
Leia antes de implementar endpoint do miolo do fluxo (solicitação, OS, indicadores).

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

## O que falta no back (retomar aqui)

Cadastros: **completos**. Solicitações (fase 1): **completo** — ver seção própria
"Solicitações" abaixo. `GET /ordens-servico`: **completo** — com ele o Painel do Gestor
fica inteiro (ver "Ordem de serviço — listagem" abaixo). Falta o resto do miolo do fluxo,
na ordem em que o front precisa:

1. **Ciclo de vida da Ordem de serviço** — `iniciar`/`pausar`/`retomar`/
   `acionar-terceiro`/`encerrar`/`custo`. É tudo ESCRITA, e nenhuma delas é do Gestor:
   as cinco primeiras são do **Técnico** (`PainelTecnico`) e o `custo` é do
   **Administrador** (`AdministradorCustosPendentes`). Destrava os dois cards mortos do
   painel do Administrador.
   O que já existe e NÃO precisa ser refeito: a criação da OS (`AbrirOS`, fase 1 — a OS
   nasce da aprovação do Gestor, nunca de um `POST /ordens-servico`) e a **leitura**
   (`GET /ordens-servico`, ver "Ordem de serviço — listagem" abaixo), que já projeta
   encerramento, custo, horas e pausas para quando essas linhas existirem.
2. **Indicadores** (`GET /indicadores/maquinas/:id`). O **job de preventiva vencida**
   saiu desta lista — está pronto e testado, falta só o Cron Job no Railway (ver seção
   abaixo). A **notificação por WhatsApp** também saiu — código pronto e testado, falta
   só o chip dedicado (ver "Notificação de solicitação por WhatsApp" abaixo).

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
Finalizadas). Array simples, sem paginação — `?pagina=` é aceito e **ignorado**, o front
pagina no cliente (mesmo padrão de `/solicitacoes`, `/maquinas`, `/preventivas`).

**Não existe `POST /ordens-servico`, e não é esquecimento**: a OS nasce de
`POST /solicitacoes/:id/abrir-os` (a aprovação do Gestor). `uq_os_solicitacao` garante
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
  `custo_hora_tecnico` é nulo por regra em reparo e terceiros (`ck_custo_por_tipo`).
- ⚠️ **O override de `numeric` no `sqlc.yaml` nunca casou nada** — o `db_type` correto é
  `pg_catalog.numeric`, não `numeric`. É por isso que `shopspring/decimal` não está no
  `go.mod` e `models.go` seguia com `pgtype.Numeric`. Nunca doeu porque nenhuma query
  tocava coluna `numeric` antes desta. Deixado como está de propósito: consertá-lo traria
  `decimal.Decimal`, que serializa como **string** em JSON contra um front que tipa
  `number`. Quem vai precisar dele direito é a **escrita** do custo (fase 2) — e aí são
  DUAS entradas, uma com `nullable: true`, senão só a coluna NOT NULL vira decimal.
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

## Notificação de solicitação por WhatsApp (feito — infra, código e wiring; falta o chip)

Gestor não fica com o app aberto o tempo todo — o sistema avisa por WhatsApp sempre que
uma Solicitação nasce `Pendente` (as duas criações humanas e o job de preventiva
vencida), pro Gestor saber sem precisar checar o painel. Mesmo raciocínio serve o
Técnico mais adiante (aviso de OS atribuída), quando a fase 2 existir — hoje é só
Gestor.

**Decisão: Evolution API self-hosted, não a Cloud API oficial da Meta.** Não é o caminho
"correto" — é WhatsApp Web por baixo (lib Baileys, engenharia reversa), viola os termos
do WhatsApp e o número pode ser banido. Mas pro volume daqui (poucas mensagens por dia,
pra 2-3 destinatários fixos que reconhecem o remetente) o risco na prática é baixo — é o
oposto do padrão que costuma levar a ban (rajada, destinatário que não reconhece,
conteúdo de marketing). Ganho real: zero aprovação de template pela Meta (a oficial
exige, e leva dias, pra toda mensagem business-initiated fora da janela de 24h), texto
livre, e roda no mesmo Docker Compose que já existe — custo marginal zero, em vez de
mensalidade de um provedor gerenciado (Z-API e primos, ~R$60-100/mês fixos) ou do
por-mensagem da oficial (~R$0,035/mensagem, categoria *utility*).

**Requer número dedicado, nunca o do Gestor.** Quem fica banível é o número que
autentica no Evolution API via QR (como um WhatsApp Web comum) — um chip pré-pago
qualquer, comprado só pra isso, com WhatsApp Business instalado. O número do Gestor é só
destinatário, nunca entra em risco.

**Infra** (`../docker-compose.yml`, repo `sistema-os-infra`, PR #1): três serviços —
`evolution-postgres` + `evolution-redis` (estado da sessão do WhatsApp, banco e cache
PRÓPRIOS, separados do banco da aplicação) e `evolution-api`
(`evoapicloud/evolution-api:v2.3.7`, porta `8092` no host). Testada de ponta a ponta:
instância criada, QR de pareamento obtido (PNG em base64 real), estado `connecting`
sobrevivendo a um restart do container (prova que é o Postgres persistindo, não
memória).

**Código** (`radaptech/sistema-OSm--Back-end`, PRs #11/#14):
- `ObterGestoresDoSetor` (`usuario.sql`) — dado um `setor_id`, todo gestor cujo escopo
  alcança ele (mesmo `EXISTS` de `ListarSolicitacoes`), ativo, com telefone. Sem
  telefone/desativado/administrador não aparece — não é erro, é degrade silencioso.
- `NotificacaoService` (`notificacaoService.go`) — cliente HTTP da Evolution API
  (`POST /message/sendText/{instancia}`, **path confirmado testando contra a instância
  real** — a documentação pública erra, descreve o inverso). `NotificadorInterface` é o
  que `SolicitacaoService`/`PreventivaService` dependem, nunca a struct concreta.
  `normalizarTelefone` existe porque `usuario.telefone` é texto livre sem máscara em
  lugar nenhum do sistema. Falha em um gestor não impede os outros (`errors.Join`, mesmo
  espírito do job de preventiva vencida).
- `SolicitacaoService`/`PreventivaService` ganharam um campo público
  `Notificador NotificadorInterface` (não parâmetro de construtor — mudar a assinatura
  quebraria todo teste que já chama `NewRepoX(pool)` direto). `nil` (o zero value, o que
  todo teste existente continua recebendo) significa "não notifica". Plugado nos 3
  pontos que criam uma solicitação `Pendente`, sempre em goroutine com
  `context.Background()` + timeout de 15s (nunca o `ctx` da request, que morre quando a
  resposta é escrita): `CadastrarSolicitacaoMaquinario`, `CadastrarSolicitacaoReparo`
  (`Alvo` sai de `alvoDaSolicitacao`) e `abrirSolicitacaoDaPreventiva` (relê a máquina
  via `ObterMaquinaPorID` depois do commit, porque `ListarPreventivasVencidasRow` não
  carrega os nomes).
- `router.go` conecta o `NotificacaoService` real; lê `EVOLUTION_API_URL`/`_API_KEY`/
  `_INSTANCE_NAME` do `.env` (ver `.env-example`).

**Testado em 3 camadas, a última contra a Evolution API real**: puros
(`montarTexto`/`normalizarTelefone`), `NotificacaoService` de integração contra Postgres
+ Evolution API real, e o wiring com um fake que grava o que recebeu
(`notificacaoWiringIntegracao_test.go`) — mais um smoke real+real (rodado e removido,
não ficou no repo) que confirmou `CadastrarSolicitacaoMaquinario` voltando em ~12ms
(assíncrono de verdade) com o log da falha esperada (WhatsApp não pareado) aparecendo
~3s depois.

**O que falta**: só o chip — comprar, instalar WhatsApp Business, escanear o QR
(`POST /instance/connect/sistema-os-notificacoes` contra a Evolution API). Nenhum código
pendente.

## Abertura automática de solicitação por preventiva (feito; falta o Cron no Railway)
Ao vencer a `proxima_data` de uma preventiva **ativa**, o sistema abre uma **Solicitação**
(não uma OS) que cai na fila do Gestor. Ela nasce com `origem = 'preventiva'`,
`preventiva_id` preenchido e `solicitante_id` **nulo** — não houve pessoa. A OS só nasce
depois, quando o Gestor aprova com técnico + urgência: criar OS direto pularia a aprovação.

**Implementado e testado (28/08/2026)**, ponta a ponta contra Postgres de verdade:
`ListarPreventivasVencidas` (`preventiva.sql`), `CriarSolicitacaoPreventiva`
(`solicitacao_os.sql`, arquivo novo), `AbrirSolicitacoesDePreventivasVencidas` +
`abrirSolicitacaoDaPreventiva` (`preventivaService.go`), `cli_preventivas_vencidas.go`
e `make preventivas-vencidas`. Falta **só** criar o Cron Job no Railway.

Desde a fase de notificação (ver seção própria acima), `abrirSolicitacaoDaPreventiva`
também chama `notificarPreventivaVencida` no fim — mesmo `Notificador` opcional de
`SolicitacaoService`, mesmo motivo de rodar em goroutine (uma preventiva com WhatsApp
lento não pode atrasar as outras 200 no mesmo laço).

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
- ⚠️ **`uq_preventiva_pendente` × `NOT EXISTS` fazem coisas diferentes**, e a versão
  anterior desta seção confundia as duas. O **índice** (único parcial em
  `solicitacao_os (preventiva_id) WHERE status = 'Pendente'`) é quem **garante a regra** —
  uma preventiva não tem duas solicitações pendentes ao mesmo tempo (pode ter várias ao
  longo do tempo, uma por ciclo) — inclusive contra duas réplicas do cron rodando juntas,
  que é justamente o que a query sozinha nunca pegaria. O **`NOT EXISTS`** na query só
  evita trabalho condenado: sem ele, toda preventiva parada na fila do Gestor volta a cada
  execução para tomar 23505 no INSERT, uma transação inútil por dia por preventiva.
  Tirar o `NOT EXISTS` **não corrompe nada** (o service trata 23505 como benigno e a
  transação volta inteira) — por isso **nenhum teste falha se ele sumir**. Tirar o índice
  é que quebra.
- ⚠️ **`m.ativa` na query é obrigatório e esse SIM está trancado por teste.**
  `DesativarMaquina` não desativa as preventivas da máquina, então sem esse filtro máquina
  desativada abriria solicitação a cada ciclo, para sempre, sem jeito de parar pela API
  (não existe reativação nem `DELETE` de linha).
- **Configurar o Cron Job no Railway** (o que falta): mesmo repo, **Dockerfile Path
  `dockerfile`** (produção, não o `.dev`), **Custom Start Command `./main
  preventivas-vencidas`** — o comando inteiro, mesma armadilha do Cron-BACKUP (ver "Backup
  do banco"). Variáveis: só as `DB_*` + `DB_SSLMODE`; **não precisa das `R2_*` nem da
  `JWT_SECRET`**. Frequência: 1×/dia de manhã (`0 9 * * *` UTC = 06:00 BRT) — preventiva
  vence no dia, não na hora, e rodar mais vezes não duplica nada, só não adianta.
- O job **não roda migrations**: quem faz isso é o boot da API. O container do cron sobe o
  mesmo binário contra o mesmo banco já migrado.
