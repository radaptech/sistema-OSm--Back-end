# Testes e CI

Leia antes de escrever teste novo ou quando `go test ./...` passar verde
suspeitosamente rápido (provavelmente é `t.Skip` por falta de Postgres).

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

## Testes
- `go test ./...`. Em `controller/` e `middleware/`, handlers e middlewares são testados
  direto com `gin.CreateTestContext` + `httptest` — sem servidor, sem banco:
  - `controller/loginController_test.go` — `serviceFake` implementa
    `LoginServiceInterface` variando só o erro; as tabelas cobrem erro → status, o
    cookie de sessão (nome, `HttpOnly`/`Secure`/`Max-Age`) e o corpo. O caso "erro não
    emite cookie" existe porque o sucesso do login já esteve dentro do `if err != nil`:
    respondia 200 vazio, sem cookie, e o erro escrevia dois corpos. O fake também **grava
    o que recebeu** (filtros de `ListarUsuarios`, `{alvo, ator}` de `Desativar`), porque
    parâmetro vindo do lugar errado não muda o status: se o ator viesse da rota em vez do
    token, a trava de auto-desativação viraria decoração e o teste de status passaria.
  - `controller/lojaController_test.go` e `setorController_test.go` — mesmo molde. O de
    setor cobre `?lojaId=` separado (ausente/vazio → `nil` no service, inválido → 400 sem
    tocar no banco): é esse filtro que faz o select em cascata mostrar só a loja escolhida.
  - `controller/maquinasController_test.go` — além do mapa erro → status, cobre o que só
    existe aqui: o corpo **multipart** virando struct (`serie` → `NumeroSerie` é o campo que
    sumiria calado, porque o nome do front não bate), os `json:"-"` barrando derivado vindo
    do cliente, os 8 corpos que o `ValidateStruct` recusa **sem chegar no service**, e a
    foto quando o R2 não está configurado — upload que falha é 500 e a máquina não é criada,
    assinatura que falha é 200 sem foto, e a chave crua **não aparece na resposta**.
  - `controller/empresaTerceirizadaController_test.go` — mapa erro → status e o corpo:
    os campos opcionais podem vir vazios, nulos ou ausentes e **nenhum dos três é erro de
    binding** (quem normaliza é o `textoOuNil` do service). Nome só de espaços é 400.
  - `controller/preventivaController_test.go` — o mapa erro → status e a assimetria do
    `maquinaId`: obrigatório no POST (cheque do controller, não tag — a struct é
    compartilhada com o corpo da máquina), ignorado no PUT.
  - `controller/ordemServicoController_test.go` — o mapa erro → status e o que só existe
    nesta rota: `?status=` separado por **vírgula** (é assim que `montarQuery` serializa
    array no front — com `ctx.QueryArray` o filtro chegaria com a vírgula dentro e viraria
    500), `?finalizada=` que é 400 quando não é booleano (ignorar devolveria a lista
    inteira pra tela de OS Finalizadas) e `?finalizada=false` chegando como ponteiro, não
    como nil. ⚠️ Monta a query com `url.Values`, não string crua: `httptest.NewRequest`
    **panica** com o espaço de `"Em Andamento"` sem encoding.
  - `controller/solicitacaoController_test.go` — o mapa erro → status, foto obrigatória
    nas duas criações (sem tocar no service nem no R2), content-type de arquivo recusado
    antes do R2 (`chaveDoUpload`), upload sem R2 configurado → 500 sem criar nada,
    validação de payload multipart e JSON, `?lojaId=`/`?status=`/`?tipo=`, e a chave crua
    do R2 nunca vazando na resposta (mesmo teste de `maquinasController_test.go`, agora
    pra anexo).
  - **Todos têm o teste "ator vem do token"**: `usuario.id`/`perfil` chegando da query em
    vez do JWT não muda status nenhum — a listagem responde 200 com dados demais.
  - `middleware/perfil_test.go` — `Permitir` com um perfil, vários, nenhum, e o caso de
    falha fechada (contexto sem perfil **nega**, protege contra montar o middleware na
    ordem errada).
  - `middleware/tenantId_test.go` — só os ramos que abortam antes de tocar no banco
    (header ausente/em branco → 400, `www`/`api` → 403), por isso passa `nil` como
    `*repository.Queries`. O `TestGetTenantID` existe porque o cast já esteve em `int32`
    e nunca casava com o `bigint` de `empresa.id` — falhava calado devolvendo `false`.
- `main_test.go` (`package main`) — `despacharSubcomando`. Substitui o mapa `subcomandos`
  por fakes e testa **o roteamento, nunca a execução** (os subcomandos reais conectam no
  banco e chamam `log.Fatal`). Cobre os nomes exatos continuarem existindo (o Railway Cron
  chama por string, não compila junto), as flags do `provisionar-admin` chegarem sem o nome
  do subcomando, e os dois erros humanos que o painel do Railway aceita calado: typo e
  `./main` no lugar do subcomando.
- Dois níveis em `internal/service/`:
  - `loginService_test.go` — unitário, sem banco: tabela cobrindo `validarEscopo` +
    `escopoDoPerfil` nos 4 perfis.
  - `loginIntegracao_test.go`, `lojaIntegracao_test.go`, `setorIntegracao_test.go`,
    `maquinarioIntegracao_test.go`, `preventivaIntegracao_test.go`,
    `escopoListagemIntegracao_test.go`, `solicitacaoOsIntegracao_test.go` —
    integração de verdade contra Postgres. `bancoDeTeste` (em `loginIntegracao_test.go`,
    compartilhado) cria um banco descartável (`teste_<nome do teste>_<pid>`), aplica as
    migrations nele e dropa no fim; o seed é criado pelos próprios services, então o
    caminho de escrita entra junto. **Sem Postgres alcançável ele dá `t.Skip`**, não falha.
    O que só aparece aqui: transação voltando inteira quando a validação recusa, gatilhos
    `DEFERRABLE` que só disparam no commit (gestor virando administrador precisa perder o
    escopo junto), unicidade por tenant/loja, e `ErrNoRows` virando `ErrNaoEncontrado` em
    vez de 500.
  - `TestListarTecnicos` (no mesmo `escopoListagemIntegracao_test.go`) cobre o que só o
    banco prova em `/tecnicos`: `area` vindo do JOIN, `lojasIds` do `array_agg`, o
    `?lojaId=`, o gestor não enxergando técnico de loja fora do escopo e o técnico
    desativado sumindo.
  - **`escopoListagemIntegracao_test.go` é o que prova o `EXISTS` do escopo**: monta um
    tenant com duas lojas, três setores e uma máquina em cada, e confere o recorte dos
    cinco perfis mais os dois casos de contorno (`?lojaId=`/`?setorId=` fora do escopo
    devolvem vazio). Falha aqui é silenciosa em produção — responde 200 com máquina demais
    e nenhuma tela reclama.
  - `preventivaJobIntegracao_test.go` — o job de preventiva vencida, em 6 subtestes que
    compartilham estado e rodam em ordem ("não duplica" só faz sentido depois de "abre").
    Cobre a forma da solicitação automática (`ck_origem`/`ck_solicitacao_alvo`, o trigger
    DEFERRABLE da foto, zero anexos), o `proxima_data` indo pra hoje+25 e não hoje+30, e o
    ciclo reabrindo depois que o Gestor converte a pendente.
    **Mutação conferida**: tirar `m.ativa` da query quebra 4 subtestes; tirar o
    `NOT EXISTS` **não quebra nenhum**, e isso é esperado — ver a seção do job.
  - `ordemServicoIntegracao_test.go` — `GET /ordens-servico`, query e service no mesmo
    teste (o setup de tenant/lojas/OS é caro pra repetir). Os subtestes rodam **em ordem**
    e compartilham estado: os de filtro contam com todas as OS `Aberta`, e só depois o
    "prepara o ciclo de vida" as encerra/pausa/lança custo. Esses INSERTs são na mão,
    diferente das OS (que vão por `AbrirOS`), porque não existe caminho de escrita para
    `os_encerramento`/`os_custo`/`os_pausa` ainda — quando existir, viram chamada de
    service. O último subteste promove um técnico a gestor, então fica por último de
    propósito.
  - `solicitacaoOsIntegracao_test.go` — as duas criações persistindo de verdade (não só
    o retorno, mesmo critério do próximo bullet), as 4 recusas (setor errado, máquina
    desativada/inexistente, marcador de impacto desconhecido), escopo nas duas
    listagens e no obter-por-id, resumo, `AbrirOS` (feliz + técnico inválido + duplo) e
    `Rejeitar` (feliz + duplo + motivo vazio).
  - `notificacaoService_test.go` — `montarTexto`/`normalizarTelefone` puros, e
    `TestNotificarNovaSolicitacao` de integração contra Postgres **e a Evolution API
    real** (`evolutionDeTeste`, mesmo critério de `bancoDeTeste`: sem ela alcançável,
    `t.Skip`). `notificacaoWiringIntegracao_test.go` prova o outro lado com um fake
    (`notificadorFake`, grava o que recebeu e sincroniza via channel com a goroutine):
    `SolicitacaoService`/`PreventivaService` chamando o `Notificador` com tenant/setor
    certos, `Alvo` formatado certo nos dois tipos, `SolicitanteNome` presente/ausente
    conforme a origem, e `Notificador == nil` não quebrando nada.
  - **Teste de escrita transacional confere o banco, não o retorno.** Em
    `maquinarioIntegracao_test.go` a máquina criada é localizada pela *listagem*, não pelo
    struct devolvido: um `CadastrarMaquina` sem `tx.Commit` devolvia a linha com id
    preenchido e o rollback do `defer` apagava tudo — o teste passava olhando só o retorno.
    Mesmo motivo do subteste "preventiva inválida desfaz a máquina junto", que confirma
    que a máquina **não** ficou no banco.
- O DSN vem de `TEST_DB_DSN`. O default (`localhost:5431`) **não conecta hoje** — a porta
  do host está certa (a 5432 é do projeto vizinho), mas o compose mapeia pra 5431 dentro
  do container, onde o Postgres não escuta; ver "Ambiente local". Use o IP do container:
  `TEST_DB_DSN='postgres://postgres:postgres@172.29.0.3:5432/postgres?sslmode=disable' go test -race ./...`.
- **A ordem dos dois `t.Cleanup` em `bancoDeTeste` é proposital** (`t.Cleanup` roda em
  LIFO: o `migrate` fecha antes do `pool`). Invertida, `pool.Close()` espera para sempre
  pela conexão que o `migrate` ainda segura e o teste **pendura em vez de falhar** — o
  banco descartável também fica órfão. Se um teste travar em ~nada de saída, é isso.
- Não há mock do `repository`: o service guarda `*pgxpool.Pool` concreto. Regra de
  negócio pura vai em função livre (como `validarEscopo` ou `montarUsuario`) justamente
  pra ser testável sem banco.
- Teste que confere mensagem de erro existe onde o **texto** é o produto: o 422 de
  `DesativarLoja` precisa carregar a contagem de setores ("ainda tem 3 setor(es)"), senão
  o admin lê um toast que não diz o que fazer.

## CI
- `.github/workflows/ci.yml` — push em `master`/`dev` e todo PR: `gofmt` (falha se algum
  arquivo estiver fora de formato), `go vet`, `go test -race ./...`.
- **O job sobe um service container de Postgres 16 e exporta `TEST_DB_DSN`** apontando
  pra `localhost:5432`. Sem ele o `loginIntegracao_test.go` daria `t.Skip` e o CI
  passaria verde tendo rodado só os unitários — pior que não ter CI. `JWT_SECRET`
  também vai no env do job: `AutenticacaoJwt` dá `panic` sem ela.
- **Não há job de deploy de propósito**: o Railway redeploya sozinho no push do branch
  conectado. Um job aqui seria uma segunda fonte da verdade + um `RAILWAY_TOKEN` pra
  guardar. Migrations rodam no boot (`main.go`), então também não precisam de passo.
  Qual branch o Railway escuta é o que separa "salvei um arquivo" de "publiquei" — ver
  "Deploy e produção".
