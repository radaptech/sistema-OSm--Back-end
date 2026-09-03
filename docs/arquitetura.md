# Arquitetura e estrutura do código

Stack, o mapa de cada pacote e as convenções que valem em todo o projeto.
Leia antes de criar arquivo novo, mexer em camada que não conhece, ou quando
estiver em dúvida sobre onde uma regra deve morar.

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

## Stack
- **Go 1.26**, módulo `github.com/radaptech/sistema-OSm--Back-end`.
- **Gin** (HTTP), **pgx/v5** (Postgres, via `pgxpool`), **golang-migrate** (migrations),
  **sqlc** (`sqlc.yaml` → gera `database/repository/` a partir de `database/queries/`).
- **JWT** (`golang-jwt/jwt/v5`, HMAC HS256) para sessão, cookie HttpOnly — ver
  "Autenticação" abaixo e front-end `CLAUDE.md`.
- **`golang.org/x/time/rate`** (token bucket) para o rate limit por IP — ver
  "Rotas e rate limit" abaixo.
- **Hash de senha: argon2id** (`alexedwards/argon2id`, `auth/passHash.go` —
  `HashPassword`/`HashCompare`, `argon2id.DefaultParams`). `bcrypt` foi removido —
  `provisionamento.go` também passou a usar `auth.HashPassword`; não reintroduza bcrypt
  em nenhum caminho de senha nova.
- Infra alvo: **Railway** (app, plano Hobby), **Supabase** (Postgres) e **Cloudflare R2**
  (bucket de fotos/vídeos, S3-compatível). O Postgres **não** é do plugin do Railway e
  **não** é conexão direta: é o Supabase atrás do **Session Pooler** (Supavisor), host
  `aws-0-<região>.pooler.supabase.com`, usuário no formato `postgres.<project-ref>`.
  Não existe host interno `*.railway.internal` nesse caminho — o tráfego sai do Railway
  pra internet.
  ⚠️ **Session mode, não transaction mode.** É o que deixa `pgx`/`sqlc` funcionarem sem
  ajuste: o pool do pgx usa **prepared statements** por padrão, e o transaction mode
  (porta `6543`) devolve a conexão ao pooler a cada transação, então o statement
  preparado some e volta `prepared statement "stmtcache_..." does not exist`,
  intermitente e sob carga. Session pooler = porta **`5432`**. Se um dia for preciso
  mudar pra `6543`, o ajuste é desligar o cache de statements
  (`config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol`), não
  "tentar de novo".
  ⚠️ Pooler no meio também é o motivo do `MinConns = 5` em `config/conn.go`: conexão
  fria negocia com o Supavisor antes de chegar no Postgres e mediu até 1.3s a mais por
  request. Ver o comentário lá, que é a fonte da verdade dos números.
  ⚠️ **Sem PITR**: backup é `pg_dump` agendado mandando pro R2 (subcomando
  `backup-banco`, ver "Backup do banco" abaixo), com restore ensaiado — não existe rede
  de proteção gerenciada aqui. Local: Docker Compose
  (`postgres_container-sistema-OS`, `api_sistema-OS` com hot-reload via CompileDaemon,
  `pgadmin_container-sistema-OS`).

## Documentos de referência (leia antes de implementar endpoint novo)
- `docs/modelagem-banco-dados.md` — modelo de dados completo, revisão 4.1, com todo o
  raciocínio de cada constraint/trigger/view.
- `docs/der-banco-dados.mmd` (+ `.svg`/`.png` gerados) — diagrama ER.
- Artefatos publicados (fonte de verdade do contrato HTTP — pedir link se precisar):
  DER revisado, Contrato de API v1.2 (53 endpoints), RBAC, PRD, Roteiro do Back-end
  (16 fases). Ver `../sistema-OSm--Front-end/CLAUDE.md` para a lógica de negócio do ponto de vista
  do front (é o documento mais detalhado de regras de negócio do projeto).

## Regra de ouro: banco e API não divergem da modelagem
Toda constraint, ENUM, trigger e view documentados em `modelagem-banco-dados.md` existem
por um motivo explicado ali — não simplifique/pule sem entender o porquê (normalmente é
uma regra de negócio que já mordeu alguém, ex: "terceirizar é decisão do Técnico, não do
Solicitante", "horas_parada só existe se afeta_producao e corre desde
`solicitacao_os.criado_em`, não desde `aberta_em`", "tipo da OS só promove pra
terceiros, nunca volta"). Peça o artefato do Contrato de API antes de implementar um
endpoint pra bater o JSON de envio/resposta campo a campo — os nomes já são exatamente o
que o front espera (camelCase, datas `dd/mm/yyyy HH:MM:SS`, ver `config/dataBr.go`).

## Estrutura atual
- `main.go` — entrypoint. Roda migrations automaticamente no boot
  (`RunMigrationPostgress`), aplica `middleware.CorsConfig()` global, monta o
  `router.Container` e registra as rotas (`router.ConfigurarRotas`), e sobe o Gin na
  porta 8081. Também despacha subcomandos de CLI antes de subir o servidor:
  `subcomandos` (mapa nome → função) + `despacharSubcomando(args) bool`, que roda o
  comando pedido e diz se rodou. Os três hoje: `provisionar-admin`, `backup-banco`,
  `preventivas-vencidas` — cada um no seu `cli_*.go` na raiz.
  ⚠️ **Argumento desconhecido cai fora do mapa e a API sobe**, sem erro. É o
  comportamento de sempre e é a armadilha do Custom Start Command do Railway (typo ou
  comando pela metade = container subindo o Gin no lugar do job, calado — já aconteceu
  com o Cron-BACKUP). `main_test.go` tranca o roteamento e os nomes, justamente porque o
  Railway chama por string e não compila junto com este repo.
  `proxiesConfiaveis()` (mesmo arquivo) alimenta o `SetTrustedProxies` — ver "Rotas e
  rate limit".
- `config/` — `VariaveisDeAmbiente` (lê `.env`), `ConnPostgresql` (pool + migrations),
  `DataBr` (tipo de data custom, layout `02/01/2006 15:04:05`, nunca RFC3339). O
  `UnmarshalJSON` aceita **as duas formas do contrato**: com hora e `02/01/2006` sozinho —
  coluna `date` chega assim (`preventiva.proxima_data` vem de um `<input type="date">` que
  o front converte pra `dd/mm/yyyy`), e sem o fallback o cadastro falhava no binding,
  antes de chegar no service. Marshal sempre emite com hora; o front lê os dois
  (`converterDataBackend`).
- `auth/` — `jwt.go` (`GerarJwt`, claims `sub`/`tenantId`/`perfil`/`exp`/`iat`,
  HS256), `passHash.go` (`HashPassword`/`HashCompare`, argon2id).
- `middleware/` — `middJwt.go` (`AutenticacaoJwt`, lê cookie `token` ou
  `Authorization: Bearer`, valida e injeta `userId`/`user_perfil`/`user_TenantId` no
  contexto do Gin — falha fechada: claim ausente/malformada aborta com 401; as chaves
  são as consts `UserId`/`UserPerfil`/`UserTenantId`, e
  `GetUserID`/`GetUserPerfil`/`GetTenantIDToken`
  leem tipado), `tenantId.go` (`TenantMiddleware` resolve o header `X-tenant-ID` em
  `empresa.id` via `ObterEmpresaPorSubdominio` e guarda na chave `TenantId`;
  `GetTenantID` lê — **só o login usa**, ver "Autenticação"), `perfil.go`
  (`Permitir(perfis ...string)`, o RBAC — 403, nunca 401), `cors.go` (`CorsConfig`,
  libera `localhost`/`*.localhost` e `radaptech.com.br`/subdomínios,
  `AllowCredentials: true` pro cookie ir junto), `rateLimit.go` (`LimitarPorIP` —
  ver "Rotas e rate limit").
- `internal/model/` — structs de request/response, com `json` (camelCase, espelhando os
  tipos do front) e `binding` (validação do `go-playground/validator` via Gin) nas tags.
  `login.go` (`Login`, `SessaoUsuario`, `EscopoAcessoGestor`), `usuarios.go`
  (`NovoUsuarioPayload`, `AtualizarUsuarioPayload`, `Usuario`), `loja.go` (`Loja`,
  `Empresa`, `NovaLojaPayload`), `setor.go` (`Setor`, `NovoSetorPayload`),
  `maquinario.go` (`MaquinarioInsert`, `AtualizarMaquina`, `Maquinario` +
  `MontarListaMaquinarios`), `preventiva.go` (`PreventivaPayload`, `Preventiva` +
  `MontarPreventiva`), `paginacao.go` (`RespostaPaginada[T]`, genérico — espelha
  `RespostaPaginada<T>` do front; `GET /usuarios` e `GET /solicitacoes/minhas` usam),
  `solicitacao.go` (as duas criações — `NovaSolicitacaoMaquinarioPayload`,
  `NovaSolicitacaoReparoPayload` —, `AberturaOrdemServicoPayload`,
  `RejeicaoSolicitacaoPayload`, `SolicitacaoOS` + `MontarSolicitacao`,
  `AnexoSolicitacao`, `ResumoSolicitacoes`, `OrdemServico` (parcial, ver seção
  "Solicitações" abaixo) + `MontarOrdemServico`).
  **Toda struct de resposta precisa do `id` e das tags camelCase**: sem tag o Go
  serializa `Nome` e o front lê `undefined`, e sem `id` a listagem não serve pra nada
  (é o `value` do select, o `/:id` do botão editar e o que vai pro escopo). Já
  aconteceu com `Loja` e `Setor`.
  ⚠️ **Campo de data em struct de resposta tem que ser `*config.DataBr`, nunca o valor.**
  O `MarshalJSON` do `DataBr` tem receiver ponteiro, então num campo não-ponteiro o
  `encoding/json` ignora o método e serializa `{}` — a data some da resposta sem erro
  nenhum. Use `config.NewDataBrPtr(...)`, que existe pra isso.
- `internal/helper/Errors.go` — `TraduzErroPostgres`: converte código de erro do
  Postgres (`23505`, `23503`, `23502`, ...) em erro de negócio (`ErrDadoDuplicado`,
  `ErrConflitoIntegridade`, ...) pros controllers não fazerem `switch` em `pgErr.Code`
  espalhado pelo código. `ErrCredenciaisInvalidas` é o erro único do login — ver
  "Autenticação" abaixo. `ErrValidacao` é o sentinela que os erros de regra de negócio
  do service embrulham com `%w` (`validarEscopo`, `setoresPorLoja`) pro controller
  responder 400 sem olhar o texto do erro.
  **Sentinela sempre na frente do `%w`**: `fmt.Errorf("%w: detalhe", helper.ErrX)`, nunca
  `"detalhe: %w"`. A mensagem inteira vai pro toast do front, e no formato antigo o texto
  genérico do sentinela ficava pendurado no rabo ("a loja ainda tem 2 setores: ID nao
  existe no sistema"). Formato atual: `categoria: detalhe`.
  ⚠️ **`TraduzErroPostgres` não entende `pgx.ErrNoRows`** — ele só olha código do
  `pgconn` e cai num `fmt.Errorf("%v", err)` final, com `%v` e não `%w`, que **quebra o
  `errors.Is`**. Todo `:one` precisa de `if errors.Is(err, pgx.ErrNoRows)` **antes** de
  chamar o helper, senão um id inexistente vira 500 em vez de 404.
- `internal/service/` — regras de negócio. Todo service guarda o `*pgxpool.Pool`
  (não um `Querier`): as escritas precisam de transação e `Querier` não expõe `WithTx`,
  então cada método monta o seu `repository.New(tx)`. Um `montarX` por entidade
  (`montarUsuario`, `montarLoja`, `montarSetor`) é a **única** tradução de linha do banco
  pra resposta — espalhar isso em cada método já fez o `id` sumir de metade delas.
  Listagem devolve slice **não-nil** (`make(..., 0, n)`): o front tipa `T[]` e `null`
  quebra o `.map`.
  - `loginService.go` — CRUD de usuário (`CadastrarUsuario`, `ObterUsuario`,
    `ListarUsuarios`, `AtualizarUsuario`, `DesativarUsuario`), `Login` e `ObterSessao`.
    - `ObterSessao` (`GET /autenticacao/sessao`) confere **empresa e usuário**:
      `ObterUsuarioPorID` **não** filtra `ativo` como o `ObterUsuarioPorEmail`, então o
      `!user.Ativo` está explícito ali, e antes dele vem `EmpresaAtiva` — sem esse cheque
      desativar um tenant só barrava login novo e quem já estava dentro seguia até o
      `exp`. Os três casos (empresa inativa, usuário sumido, usuário inativo) devolvem
      `ErrSessaoExpirada`.
    - `ListarUsuarios` monta o escopo da página inteira com **uma** query
      (`ObterEscoposSessaoPorUsuarios`), não uma por usuário.
    - `AtualizarUsuario` substitui o escopo inteiro na mesma transação (setores antes dos
      escopos, sem `ON DELETE CASCADE`) e zera `area_tecnico_id` fora do perfil técnico —
      `ck_usuario_area_tecnico` exige a coluna NOT NULL exatamente para `tecnico`.
    - `DesativarUsuario` recusa `id == atorId`. Não é vaidade: só administrador chega na
      rota, então o último deles se desativando trancaria o tenant inteiro pra fora, com
      saída só pela CLI de provisionamento.
  - `EscopoPerfilService.go` — o que é por perfil e não por endpoint: `validarEscopo`
    (cardinalidade de 3.8, que as tags de `binding` não alcançam), `escopoDoPerfil`
    (normaliza o payload plano do front), `setoresPorLoja` (distribui cada setor no
    escopo da loja certa — ver "Queries e repository") e `montarSessao` (o corpo de
    `SessaoUsuario`, compartilhado entre `POST /autenticacao/login` e
    `GET /autenticacao/sessao`). Também `gravarEscopo` e `resolverAreaTecnico`,
    compartilhados por `CadastrarUsuario`/`AtualizarUsuario`, e `montarUsuario` (o
    caminho inverso de `escopoDoPerfil`: achata o escopo do banco no formato plano do
    front — `acessoTotalSetores` só é `true` quando **todos** os escopos são totais,
    senão o front marcaria o alternador e apagaria os setores da loja parcial no próximo
    save).
  - `empresaTerceirizadaService.go` — CRUD da prestadora externa que o Técnico aciona.
    O mais raso do pacote: sem transação (uma tabela, sem filho pra gravar junto), sem
    escopo de acesso (não pende de loja/setor) e sem recusa por dependente no
    `Desativar` (não tem filho; a OS que já a acionou continua apontando pra linha,
    porque o delete é soft).
    ⚠️ **Especialidade e telefone passam por `textoOuNil`, não por `nomeValido`**: os dois
    são opcionais, e o formulário do front manda string vazia no campo que ninguém tocou.
    Com `nomeValido` o cadastro inteiro passaria a exigir especialidade, com a mensagem
    falando em "nome"; sem normalizar, o banco guarda `""` numa coluna nullable e o campo
    volta presente e vazio (o `omitempty` não pega ponteiro para string vazia).
  - `lojaService.go` — CRUD de loja + `ListarEmpresas`. `nomeValido` (apara e recusa
    vazio; `binding:"required"` passa numa string de espaços e não há CHECK no banco) é
    compartilhado com setor. **`DesativarLoja` recusa enquanto houver setor ativo**, e a
    contagem roda na mesma transação do UPDATE — separadas, alguém cria um setor no meio
    e a loja fica inativa com setor ativo pendurado no escopo de um gestor. Recusa em vez
    de cascatear porque o soft delete não tem volta pela API.
  - `setorService.go` — CRUD de setor. **`CadastrarSetor` é transacional** e recusa loja
    inexistente, de outro tenant ou **desativada**: a FK composta garante o tenant, não o
    `ativa`, e sem isso dava pra desativar a loja (permitido com zero setores) e pendurar
    um setor novo nela depois, contornando a regra pelo outro lado. O
    `ObterLojaParaEscrita` usa `FOR SHARE` pra loja não ser desativada entre o cheque e o
    INSERT. `AtualizarSetor` **não** muda `loja_id` (ver "Queries e repository").
  - `maquinario.go` — CRUD de máquina. `CadastrarMaquina`/`AtualizarMaquina` são
    transacionais **por causa das preventivas**: máquina sem preventiva não pode chegar a
    existir (ver `preventivaService.go` abaixo), então as duas gravam juntas ou nenhuma
    grava. As duas releem por `ObterMaquinaPorID` antes do commit — `RETURNING` não
    enxerga tabela juntada, e a resposta precisa sair com `setorNome`/`lojaId`/`lojaNome`
    na mesma forma do `GET`, porque o front consome POST, PUT e GET pelo mesmo tipo
    `Maquina`. `AtualizarMaquina` **muda** `setor_id` (diferente de `AtualizarSetor`, que
    ignora `loja_id`): mover máquina de setor não arrasta o histórico de mais ninguém.
    `ListarMaquinario` recebe `usuarioId`/`perfil` além do tenant e recorta pelo escopo
    de quem chama — ver "Escopo no `WHERE`" em "Queries e repository".
  - `preventivaService.go` — CRUD de preventiva **mais** `gravarPreventivas`, função livre
    que recebe `*repository.Queries` (não o Pool) exatamente para
    `CadastrarMaquina`/`AtualizarMaquina` a chamarem de dentro da transação que já
    abriram — um método de `PreventivaService` abriria transação própria e quebraria a
    atomicidade. Mesmo padrão de `gravarEscopo`. **A regra "máquina exige ao menos uma
    preventiva" é validada aqui, no servidor**: o `min(1)` do Zod é só do navegador, e sem
    o cheque um POST direto criaria máquina sem preventiva nenhuma.
    `AtualizarMaquina` substitui o conjunto inteiro (`DesativarPreventivasDaMaquina` antes
    de `gravarPreventivas`, sem merge incremental) — mesmo padrão do escopo em
    `AtualizarUsuario`.
    Mora aqui também o **job de preventiva vencida**:
    `AbrirSolicitacoesDePreventivasVencidas` (percorre as vencidas, devolve quantas abriu
    e as falhas num `errors.Join`) e `abrirSolicitacaoDaPreventiva` (a transação de uma
    preventiva só). É o único método do pacote **sem `tenantID` no parâmetro** — não há
    request nem token, o job varre todos os tenants. Ver a seção do job.
    Também `notificarPreventivaVencida`, chamada no fim de `abrirSolicitacaoDaPreventiva`
    — ver "Solicitações" abaixo pro papel do `Notificador`.
  - `solicitacaoOs.go` + `solicitacaoOsHelpers.go` — `SolicitacaoService`: as duas
    criações humanas, as duas listagens (fila do gestor com escopo, "minhas" paginada),
    obter por id, resumo, `AbrirOS` e `Rejeitar`. Ver a seção "Solicitações" abaixo pro
    detalhe de cada método — aqui vale só o que é convenção nova em relação ao resto do
    pacote: `montarSolicitacoesEmLote` é o `ObterEscoposSessaoPorUsuarios` da vez (busca
    impacto/anexo de uma página inteira numa ida só, sem N+1) e `concluirSolicitacao` é a
    cauda comum às duas criações + `Rejeitar` (relê, comita, monta — mesmo motivo de
    `CadastrarMaquina` relendo por `ObterMaquinaPorID`).
  - `ordemServico.go` — `OrdemServicoService`, só leitura por enquanto
    (`ListarOrdensServico` + `montarOrdensServicoEmLote`). A OS não nasce aqui: quem a
    cria é `SolicitacaoService.AbrirOS`. Único service do pacote que recebe os filtros
    numa **struct** (`FiltrosOrdemServico`) em vez de parâmetros soltos — são seis, e
    dois deles são `*int64` vizinhos (`LojaId`/`TecnicoId`): trocá-los de lugar numa
    assinatura posicional compila e devolve a lista errada, calado.
  - `notificacaoService.go` — `NotificacaoService`, o cliente da Evolution API (WhatsApp).
    Ver "Solicitações" abaixo, tem seção própria.
- `controller/` — `loginController.go`: `LoginController` recebe um
  `LoginServiceInterface` (a interface existe pro teste do handler poder trocar o
  service — `UsuarioService` guarda `*pgxpool.Pool` concreto, não dá pra mockar de
  outro jeito). Handlers: `Registrar` (`POST /usuarios`), `Login`, `Logout`, `Sessao`.
  O cookie de sessão sai só de `cookieSessao(ctx, token, maxAge)` — login e logout
  passam pela mesma função porque o `Set-Cookie` de remoção só apaga se casar com o de
  criação (nome, path, `Secure`, `SameSite`). Mais `Obter`/`ListarUsuarios`/
  `Atualizar`/`Desativar`.
  `lojaController.go`, `setorController.go`, `maquinasController.go` e
  `preventivaController.go` seguem o mesmo molde (uma interface própria por service,
  pro fake do teste). `solicitacaoController.go` também, mais 3 buckets do R2 (anexo de
  maquinário, de pequeno reparo, e a foto de cadastro da máquina pra resolver
  `maquinaFotoUrl`) — ver "Solicitações" abaixo.
  **Mapa de erro → status**: `ErrValidacao` 400, `ErrDadoDuplicado` 409,
  `ErrConflitoIntegridade` 422, resto 500 **com o erro cru só no `log`**.
  `ErrNaoEncontrado` é **404 quando o `:id` da rota é a única coisa que pode faltar**
  (loja, setor, `GET`/`DELETE /usuarios/:id`) e **422 em `POST`/`PUT /usuarios`**, onde o
  mesmo sentinela também cobre "área técnica citada no corpo não existe" e `errors.Is`
  não distingue os dois.
- `internal/service/helpers.go` — o que mais de um service usa e não é de nenhuma
  entidade: `nomeValido` (apara e recusa vazio), `textoOuNil` (o irmão dele para campo
  opcional: apara e devolve `nil`) e `escopoDe` (quem chama → filtro de escopo das
  listagens). Não moram lá, de propósito: os `montarX` (cada um é a tradução única da sua
  entidade), o que é por perfil (`EscopoPerfilService.go` já é o arquivo compartilhado
  desse assunto) e `gravarPreventivas` (só se entende junto do resto de preventiva).
- `controller/helpers.go` — o que mais de um controller faz antes de chamar o service.
  Todos devolvem `(valor, false)` **depois de já terem escrito a resposta de erro**, então
  o handler é sempre `x, ok := helper(ctx); if !ok { return }` e nunca escreve dois corpos.
  - `idDaRota` — `:id` malformado é **400**, não 404 (`/abc` não é um id que não existe, é
    bug de cliente). `idDeQuery(ctx, nome)` é o mesmo critério para `?lojaId=`/`?setorId=`/
    `?maquinaId=`: ausente ou vazio vira `nil` sem erro, inválido é 400.
  - `tenantDaRota` — tenant do **token**, nunca do header (ver "Autenticação").
  - `atorDaRota` — `usuario.id` + `perfil` do token, para as listagens que filtram por
    escopo. Mesmo motivo do anterior: aceitar do cliente deixaria um solicitante listar a
    loja inteira mandando outro id. Claim faltando é 500 (bug de wiring), não 401.
  - `corpoJSON[T]` — `ShouldBindJSON` das rotas sem arquivo. Campo extra é ignorado pelo
    binding. Substituiu `corpoLoja`/`corpoSetor`, que eram a mesma função com tipos
    diferentes; as notas de cada payload vivem na struct dele, em `internal/model`.
  - `corpoMultipart[T](ctx, limiteCorpo)` — rotas **com** arquivo (`POST`/`PUT /maquinas`,
    as duas criações de solicitação): JSON na parte `dados`, arquivos nas partes
    `foto`/`video`. ⚠️ Como o corpo entra por `json.Unmarshal`, **as tags `binding` não
    rodam sozinhas** — é por isso que a função chama `binding.Validator.ValidateStruct`
    explicitamente. Sem essa linha, `required`/`oneof`/`min=1`/`dive` viram decoração.
    Corpo maior que o limite responde **413**, não 400: não está malformado, está grande.
    `limiteCorpo` é o teto do corpo (`bucketr2.TamanhoMaximoFoto` nas rotas só-foto,
    `bucketr2.TamanhoMaximoComVideo` em `POST /solicitacoes/maquinario`, a única que
    aceita vídeo) — o teto de MEMÓRIA passado a `ParseMultipartForm` continua sempre
    `TamanhoMaximoFoto`: o excedente do vídeo escorre pro disco, não infla a heap.
  - Ficaram de fora de propósito: `cookieSessao` (regras do cookie de sessão, colado no
    Login/Logout) e `resolverFoto`/`chaveDaFoto` (métodos — dependem do bucket que o
    `MaquinaController` guarda).
- `internal/router/router.go` — `Container` (injeção: guarda os controllers montados +
  o `*repository.Queries` que o `TenantMiddleware` precisa) e `ConfigurarRotas`. Ver
  "Rotas e rate limit".
- `database/migrate/` — migrations SQL puro (`golang-migrate`), numeradas e sequenciais.
- `database/queries/` + `database/repository/` — ver "Queries e repository (sqlc)"
  abaixo.
- `bucketR2/` — cliente do Cloudflare R2 (`s3.Client` via SDK da AWS, `BaseEndpoint`
  apontado pro R2). `InitR2_cloudflare` monta `s3Client` e `presignClient` (vars de
  pacote, um de cada, montados uma vez no boot — não recriar por request). `UploadFoto(
  fotoUrl, bucket string) gin.HandlerFunc` faz upload multipart com `MaxBytesReader` +
  `ParseMultipartForm` (10MB, `tamanhoMaximoFoto`), key prefixada por tenant
  (`tenant/{id}/...`, lida de `middleware.GetTenantIDToken` — **500**, não 401, se a claim
  `UploadFoto(ctx, tenantID, bucket, header) (string, error)` sobe o arquivo e devolve a
  **key** — recebe o `*multipart.FileHeader` e **não** um `gin.Context`: quem decide status
  HTTP, se a foto é obrigatória e o que fazer quando o resto falha é o handler do domínio.
  Abre e fecha o arquivo por dentro. Key prefixada por tenant (`tenant/{id}/...`) e
  `ContentType` do header do arquivo (senão o R2 serve como `application/octet-stream` e o
  browser força download em vez de exibir).
  `URLLeitura(ctx, bucket, key string, ttl time.Duration) (string, error)` gera a URL
  assinada de leitura via `presignClient.PresignGetObject` — é o que resolve
  `maquina.foto_chave`/`solicitacao_anexo.chave` (guardados como key, não URL — ver
  "R2 — storage de anexos" abaixo) num `fotoUrl`/`url` de resposta.
  ⚠️ **As duas funções checam se o cliente é nil** (`s3Client`/`presignClient`, montados só
  por `InitR2_cloudflare`): sem a guarda, um boot sem as variáveis do R2 vira **panic** de
  nil pointer na primeira máquina com foto que alguém listar — e aí não some só a foto,
  some a resposta inteira.
  **Wireado em `/maquinas`** (`POST`/`PUT` sobem a foto, todas as leituras devolvem URL
  assinada) e em `/solicitacoes` (foto obrigatória + vídeo opcional nas duas criações,
  `solicitacaoController.resolverSolicitacao` resolve anexo e `maquinaFotoUrl` antes de
  responder — ver "Solicitações" abaixo).
