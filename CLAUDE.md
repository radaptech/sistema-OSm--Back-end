# Contexto do Projeto - Back-end (Solicitação OS)

API do sistema de manutenção/OS para redes de varejo, consumida pelo front-end em
`../sistema-OSm--Front-end` (React). Este back-end está em construção — ver `docs/` para o roteiro
completo e os contratos já fechados com o front.

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
  `RespostaPaginada<T>` do front; só `GET /usuarios` usa hoje).
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
  pro fake do teste).
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
  - `corpoMultipart[T]` — rotas **com** arquivo (`POST`/`PUT /maquinas` hoje, as três
    criações de solicitação depois): JSON na parte `dados`, arquivos nas partes
    `foto`/`video`. ⚠️ Como o corpo entra por `json.Unmarshal`, **as tags `binding` não
    rodam sozinhas** — é por isso que a função chama `binding.Validator.ValidateStruct`
    explicitamente. Sem essa linha, `required`/`oneof`/`min=1`/`dive` viram decoração.
    Corpo maior que o limite responde **413**, não 400: não está malformado, está grande.
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
  assinada); falta o mesmo para `solicitacao_anexo`.

## Migrations
- Criar novo par: `make migration nome_da_migration` (gera `NNNNNN_nome.up.sql` +
  `.down.sql` em `database/migrate/`).
- **`down.sql` sempre reverte exatamente na ordem inversa do `up.sql`** (views → triggers/
  funções → índices explícitos → tabelas filhas→raízes → tipos ENUM → extensões).
- Antes de considerar uma migration pronta, **teste contra Postgres de verdade**: suba um
  banco descartável (`docker exec postgres_container-sistema-OS psql -U postgres -d
  postgres -c "CREATE DATABASE teste_x"`), aplique `up.sql`, teste os casos de borda
  (triggers deferred, CHECKs compostos), aplique `down.sql`, confirme que voltou a zero,
  descarte o banco. Não valide só lendo o SQL.
- `main.go` roda migrations pendentes sozinho a cada boot — não precisa rodar `migrate`
  manualmente em dev, só em produção/CI antes do deploy do binário.
- Aplicadas até aqui: `000001` schema inicial, `000002` horas parada desde a solicitação,
  `000003` chave do R2, `000004` criticidade vira ENUM, `000005` foto só na solicitação
  humana, `000006` seed de `area_tecnico`.
- ⚠️ **Tabela e tipo dividem namespace no Postgres** — trocar uma tabela por um ENUM
  homônimo exige dropar a tabela **antes** de criar o tipo (foi o caso de `000004`).

## Provisionamento do primeiro Administrador
Não existe `POST /empresas` nem forma de criar o primeiro `administrador` via API —
`POST /usuarios` exige um administrador já autenticado. Resolvido fora da API, por CLI:

```
make provisionar-admin ARGS="-subdominio=cooprata -empresa='Cooprata' -nome='Davi' -email=admin@cooprata.com -senha=SENHA_FORTE"
```

Implementado em `provisionamento.go` na raiz (`package main`, `ProvisionarAdministrador`,
SQL cru — não passa pelo `repository`) + `cli_provisionar_admin.go`. Idempotente por
tenant: rodar de novo com o mesmo `--subdominio` não recria a empresa, só adiciona outro
administrador a ela.

## Backup do banco (`backup-banco`)
Mesmo padrão do `provisionar-admin`: subcomando de CLI, fora da API HTTP, despachado por
`main.go` antes do servidor subir. Despeja o banco inteiro (`pg_dump --format=custom
--no-owner --no-privileges`, num arquivo temporário — não em pipe direto, o SDK da AWS
lida melhor com `io.ReadSeeker` do que com um reader sem seek) e sobe pro R2 com
`bucketR2.UploadArquivo` (genérico, `io.Reader` + key prontos — diferente de `UploadFoto`,
que resolve o upload de dentro de uma requisição HTTP multipart).

```
make backup-banco
```

Implementado em `cli_backup_banco.go` na raiz. Requer `R2_BUCKET_NAME_BACKUPS` no `.env`
(reaproveita as outras três `R2_*` já usadas pelo upload de foto) — **o bucket precisa
existir no Cloudflare antes**, criado manualmente no dashboard: `PutObject` não cria
bucket, só grava dentro de um que já existe. **Testado local de ponta a ponta contra o R2
de verdade** (23/08/2026, bucket `backups-cooprata`): `make backup-banco` →
`backup salvo em backups-cooprata/backups/<timestamp>.dump`, sem erro — antes disso, contra
o mesmo bucket ainda não criado, o erro era um `NoSuchBucket` limpo (confirmando que só
faltava o bucket, o resto do pipeline já rodava certo).
`--no-owner --no-privileges` existem por causa do restore: o banco de teste ("restore
ensaiado") normalmente tem um owner diferente do de produção, e sem essas duas flags o
`pg_restore` para em `ALTER OWNER`/`GRANT` pra um role que não existe no destino —
**testado** localmente (dump → `CREATE DATABASE` descartável → `pg_restore --no-owner
--no-privileges` → contagem de linhas batendo em `empresa`/`maquina`/`preventiva` →
`DROP DATABASE`), sem erro nenhum do `pg_restore`.

Chave do objeto: `backups/<timestamp UTC>.dump` (`20060102-150405`) — sem prefixo de
tenant, porque é o banco inteiro, todos os tenants juntos (o Postgres é uma instância
só, sem um banco por tenant).

**Agendamento é Railway Cron, não `pg_cron`** — mesmo motivo do job de preventiva vencida
(ver "Abertura automática de solicitação por preventiva" nesse arquivo): Hobby não libera
`shared_preload_libraries`. Ao configurar o Cron Job no Railway:
- ⚠️ **A major do `postgresql*-client` no `dockerfile` tem que ser >= a do servidor.**
  `pg_dump` recusa dumpar um Postgres maior que ele: `aborting because of server version
  mismatch` (`server version: 17.6; pg_dump version: 16.15`) — foi a segunda falha do
  Cron-BACKUP (24/08/2026), já com o start command certo. Supabase roda **17.6**, então o
  pacote é `postgresql17-client` nos dois dockerfiles. Major nova no Supabase = subir o
  pacote junto, senão o backup morre calado até alguém olhar o log do cron.
- Aponte pro mesmo repo, com **Custom Start Command `./main backup-banco`** — o comando
  inteiro, não só o subcomando. O `dockerfile` de produção termina em **`CMD ["./main"]`**
  (era `ENTRYPOINT`, virou `CMD` em 24/08/2026), e o start command do Railway **substitui
  o `CMD` inteiro** em vez de virar argumento dele. Com só `backup-banco` ali, o Railway
  não aplica nada e o container sobe a **API** — foi exatamente o que aconteceu na
  primeira execução do Cron-BACKUP (24/08/2026): logs com o Gin registrando rotas e
  `panic: JWT_SECRET não configurada`, nenhum backup. Se um dia o `dockerfile` voltar pra
  `ENTRYPOINT`, `backup-banco` sozinho volta a funcionar — `./main backup-banco` funciona
  nos dois casos.
- **`dockerfile` (produção) e `dockerfile.dev` (Compose local) são arquivos diferentes
  desde 23/08/2026** — ver "Dois Dockerfiles" abaixo. Garanta que o Railway builda o
  `dockerfile` (produção, sem CompileDaemon): "Settings > Build > Dockerfile Path", se o
  nome sozinho não for pego por padrão.
- Variáveis do serviço: as mesmas `DB_*`/`DB_SSLMODE` da API (host do Session Pooler do
  Supabase, porta `5432` — ver "Stack") e as quatro `R2_*` (as três de sempre + `R2_BUCKET_NAME_BACKUPS`).
- Frequência: diária é o ponto de partida razoável — não existe ainda rotação/expiração
  de backup antigo (o bucket cresce um `.dump` por execução, sem limpeza automática); se
  o custo de armazenamento incomodar, é o próximo passo, não um bloqueio para começar.

## Job de preventiva vencida (`preventivas-vencidas`)
Terceiro subcomando de CLI, mesmo molde de `provisionar-admin` e `backup-banco`:
`cli_preventivas_vencidas.go` na raiz, `make preventivas-vencidas` em dev, Railway Cron em
produção. Abre uma Solicitação para cada preventiva vencida e avança o ciclo dela — o
porquê de cada decisão está em "Abertura automática de solicitação por preventiva".

Imprime o total **antes** do erro de propósito (falha parcial é normal: sem o número
primeiro, o log do cron mostraria só o que deu errado), e mesmo assim sai com código ≠ 0
quando houve falha — cron que falha metade e devolve sucesso não é notado por ninguém.

```
make preventivas-vencidas
```

## Dois Dockerfiles (produção × dev)
`dockerfile` (produção, o que o Railway builda) e `dockerfile.dev` (o que
`../docker-compose.yml` builda, `dockerfile: dockerfile.dev` no serviço `api`) — arquivos
separados desde 23/08/2026. Motivo: `dockerfile.dev` roda `CompileDaemon` (hot-reload,
pra reagir ao volume `./sistema-OSm--Back-end:/app` do Compose) com um `ENTRYPOINT` fixo — sem uma
imagem de produção própria, o Cron Job do `backup-banco` (acima) não tinha como injetar
o argumento certo, e a imagem final carregava o toolchain do Go inteiro à toa.
- `dockerfile` é multi-stage: `builder` compila (`CGO_ENABLED=0` — nada no projeto usa
  cgo, pgx/v5 é Go puro, então sai binário estático), o estágio final é só
  `alpine:3.24` (mesma versão por baixo de `golang:1.26.5-alpine`) + `ca-certificates`
  (senão toda chamada HTTPS pro R2 falha na validação do certificado) +
  `postgresql17-client` (só o `pg_dump`/`pg_restore`, não o servidor) + o binário +
  `database/migrate` (`config/conn.go` lê migrations por caminho relativo,
  `file://database/migrate`, resolvido a partir do `WORKDIR`). **Testado local:** builda
  limpo, sobe a API normal contra o Postgres do Compose (migrations rodando, rotas
  registradas) e roda `backup-banco` como argumento simples — 94.5MB contra 1.17GB da
  imagem de dev (~12x menor), sem o Go toolchain nem o `.git` embarcados.
- `dockerfile.dev` não muda: continua com `git`/`build-base`/`CompileDaemon` e o
  `postgresql17-client` (o `backup-banco` também precisa rodar localmente pra testar).
  Os dois ficam na **mesma major** de propósito: o dev é onde o `backup-banco` é testado
  antes de subir, e testar com um cliente diferente do de produção testa outra coisa.
  Cliente 17 dumpa o Postgres 16 do Compose sem reclamar (o problema é só o contrário —
  ver abaixo), então alinhar não custa nada em dev.
- **Nenhum dos dois copia `.env`** — nem precisa: `config.NewVariaveisAmbiente` tenta
  carregar `.env` e só avisa se não achar, seguindo com variável de ambiente do sistema
  (é o caminho de produção: Railway injeta as variáveis, não existe `.env` lá).

## Autenticação (JWT)
- `auth.GerarJwt(id, tenantId int64, perfil string)` assina um HS256 com `JWT_SECRET`
  (`.env` — gere com `openssl rand -base64 64`, mínimo 256 bits, nunca commitado) e
  claims mínimas: `sub` (usuario.id), `tenantId` (empresa.id), `perfil`, `exp` (8h),
  `iat`. **De propósito sem** `nome`/`escoposGestor`/`lojaId`/`setorId`/`tecnicoId`/
  `ativo` — isso é resolvido fresco no banco a cada request (`ObterSessao`), senão
  editar o escopo de um Gestor ou desativar um usuário só valeria depois do token
  expirar.
- **Depois do login, `tenant_id` autoritativo é o do token, não o header
  `X-tenant-ID`** — o header só importa em `POST /autenticacao/login`, antes de existir
  token, pra saber contra qual `empresa` autenticar. Trocar o header depois de logado
  não pode mudar de tenant. Na prática: **`Login` é o único handler que chama
  `middleware.GetTenantID` (header); todo o resto chama `GetTenantIDToken`.** Usar o do
  header numa rota autenticada deixa um administrador do tenant A escrever no tenant B
  só trocando o `X-tenant-ID` — o banco aceita calado, é só um `int64`. Já foi bug uma
  vez.
- `middleware.AutenticacaoJwt()` lê o token do cookie `token` (produção) ou do header
  `Authorization: Bearer <token>` (Postman/Insomnia, sem cookie) e injeta no contexto:
  `userId`, `user_perfil`, `user_TenantId` (todos `int64`/`string`). Aborta com 401 se
  qualquer claim obrigatória faltar — não deixa o handler seguir com contexto
  incompleto.
- `service.UsuarioService.Login(ctx, model.Login, tenantId)` devolve `(token, sessão,
  erro)` — o service já assina o JWT, o controller só precisa pôr no cookie. Sem
  transação de propósito: são leituras + o `UPDATE` de `ultimo_acesso`.
- **E-mail inexistente, senha errada, usuário inativo e perfil trocado devolvem todos
  `helper.ErrCredenciaisInvalidas`**, a mesma mensagem — senão o login vira um oráculo
  de quais e-mails existem no tenant. O `perfil` vem do formulário, então é palpite do
  cliente: quando não bate com o do banco é credencial inválida, **nunca** promoção.
  (`ObterUsuarioPorEmail` já filtra `AND ativo`, então usuário desativado cai sozinho no
  mesmo `ErrNoRows`.)
- `SessaoUsuario` é montada por perfil em `montarSessao`: administrador não tem nada
  (a ausência de escopo É o acesso total), técnico leva `tecnicoId` = o próprio
  `usuario.id` (`fk_os_tecnico` aponta pra `usuario`; não existe tabela `tecnico`),
  gestor leva `escoposGestor`, solicitante leva `lojaId`/`setorId`/`setorNome`.
- Cookie de sessão: `HttpOnly` + `Secure` + `SameSite=Lax`, `maxAge` 86400 (24h), nome
  `token` (tem que bater com o que o middleware lê). **O `maxAge` do cookie (24h) é
  maior que o `exp` do JWT (8h) — não é intencional, é só sobra**: passadas as 8h o
  browser ainda manda o cookie, o middleware rejeita e o front desloga no 401 do
  `/sessao` (que apaga o cookie). Se for igualar, mexa nos dois: `auth/jwt.go` e
  `cookieSessao`. Sai só de `cookieSessao` em
  `controller/loginController.go`. `Lax` basta porque front e API ficam sob o mesmo
  domínio registrável (`*.radaptech.com.br`, `localhost` em dev); só vira `None` (que
  obriga `Secure`) se o front sair pra outro domínio.
- Mapa de erro → status nos handlers de auth: `ErrValidacao` → 400,
  `ErrCredenciaisInvalidas` → 401, `ErrSessaoExpirada` → 401 **+ cookie apagado** (o
  front desloga no 401; sem limpar o cookie ele reenvia a mesma sessão morta em loop),
  `ErrDadoDuplicado` → 409, `ErrNaoEncontrado`/`ErrConflitoIntegridade` → 422, resto →
  500 **com o erro só no `log`**: o erro cru do pgx carrega nome de constraint/coluna e
  às vezes o SQL, não pode ir no corpo da resposta.
- **Revogação: metade feita.** `ObterSessao` já derruba sessão de usuário desativado e
  de **empresa desativada** (`EmpresaAtiva`), e o front desloga sozinho no 401 do
  `/sessao`. Mas isso fecha só o caminho do browser: o `AutenticacaoJwt` valida
  assinatura e não vai ao banco, então **um token cru na mão continua escrevendo em todas
  as outras rotas até o `exp` de 8h**. Trocar senha e logout também não matam token
  emitido, e `HttpOnly` não impede o dono do navegador de
  copiar o cookie no DevTools (ele protege contra XSS, não contra o usuário). O `exp`
  de 8h limita a janela a um turno. Conserto barato quando for mexer: `token_version`
  na `usuario`, o JWT carrega o valor e o middleware compara — dá pra juntar na mesma
  query que o RBAC de escopo vai precisar fazer.

## Rotas e rate limit
- Tudo em `internal/router/router.go`, sob o grupo `/api`. Registradas hoje:

  | rota | RBAC |
  |---|---|
  | `GET /api` (healthcheck) | pública |
  | `POST /autenticacao/login` | pública (rate limit + `TenantMiddleware`) |
  | `POST /autenticacao/logout` | pública, ver abaixo |
  | `GET /autenticacao/sessao` | autenticada |
  | `GET·POST /usuarios`, `GET·PUT·DELETE /usuarios/:id` | administrador |
  | `GET /empresas` | administrador |
  | `GET /tecnicos` | gestor, administrador |
  | `GET /lojas`, `GET /setores` | **qualquer perfil autenticado** |
  | `GET·PUT·DELETE /lojas/:id`, `POST /lojas` | administrador |
  | `GET·PUT·DELETE /setores/:id`, `POST /setores` | administrador |
  | `GET /maquinas`, `GET /preventivas` | **qualquer perfil autenticado** (filtrado por escopo) |
  | `GET·PUT·DELETE /maquinas/:id`, `POST /maquinas` | administrador |
  | `GET·PUT·DELETE /preventivas/:id`, `POST /preventivas` | administrador |
  | `GET /empresas-terceirizadas` | **técnico**, administrador |
  | `GET·PUT·DELETE /empresas-terceirizadas/:id`, `POST /empresas-terceirizadas` | administrador |

- **`GET /maquinas` e `GET /preventivas` são abertas mas não são amplas**: o RBAC libera
  qualquer perfil e o **escopo é aplicado no `WHERE`** (ver "Escopo no `WHERE`" em "Queries
  e repository"). O Solicitante escolhe máquina do próprio setor em Nova Solicitação e o
  Gestor lista as preventivas das lojas dele — os dois chamam a mesma rota e recebem
  recortes diferentes. `GET /maquinas/:id` e `GET /preventivas/:id` **são de
  administrador**: a única tela que lê o registro inteiro é o formulário de edição dele.
- **`GET /empresas-terceirizadas` é do técnico, não do gestor**: terceirizar é decisão
  do Técnico (`front-end/CLAUDE.md` item 9), e é ele quem escolhe a empresa no
  `ModalAcionarTerceiro`. O Gestor não consome essa lista — o nome da empresa chega
  denormalizado na OS. **Sem escopo no `WHERE`**, diferente de `/maquinas` e
  `/preventivas`: a entidade não pende de loja nem setor, é do tenant inteiro, e por isso
  o service nem recebe `usuarioId`/`perfil`.
- **`POST /preventivas` é só a preventiva avulsa** (`ModalManutencaoPreventiva`). As
  preventivas do formulário de máquina **não passam por essa rota**: viajam dentro do
  corpo de `POST`/`PUT /maquinas` e gravam na mesma transação da máquina
  (`gravarPreventivas`). O controller de máquina nunca toca no `PreventivaService`.
- **As listagens de loja e setor sem `Permitir` são de propósito**: o painel do gestor agrupa por
  loja e nomeia os blocos por setor (`acessoGestor.ts` procura o setor por id), e os
  selects em cascata de cadastro dependem das duas. Restringir a administrador deixa o
  painel do gestor sem nomes. Escrever continua só do administrador.
- **`GET /tecnicos` é projeção somente-leitura sobre `usuario`**, não CRUD: técnico é
  usuário com `perfil = 'tecnico'`, e escrever continua sendo `/usuarios` (duas superfícies
  de escrita deixariam o mesmo e-mail entrar duas vezes). Existe separada de
  `GET /usuarios?perfil=tecnico` por três motivos, sendo o primeiro o que obriga: (1) o
  RBAC — `/usuarios` é só do administrador, e quem precisa da lista é o **Gestor**, no
  select de "Técnico Responsável" do `ModalAbrirOrdemServico`; (2) a forma — o tipo
  `Tecnico` do front pede `area` como **nome** e `lojasIds`, e `/usuarios` é paginada;
  (3) a leitura não precisa de nada que o CRUD de usuário faz. Mora no `LoginController`
  pelo mesmo motivo de `GET /empresas` morar no de loja.
  **`GET /tecnicos/:id` não foi feito**: `servicoTecnicos.obterPorId` existe no front mas
  não é chamado em lugar nenhum. Entra quando a tela do Administrador existir — que
  também não existe (o `front-end/CLAUDE.md` descreve `AdministradorTecnicos`, mas não há
  pasta, card no painel nem rota no código).
- `GET /empresas` mora no `LojaController` porque empresa **não tem CRUD** — o tenant
  nasce pela CLI de provisionamento, e a única tela que pergunta por ela é a de loja.
- **`TenantMiddleware` entra só em `/autenticacao/login`** — é o único endpoint que lê o
  header `X-tenant-ID`; ver "Autenticação". Rota autenticada que precise de tenant usa
  `GetTenantIDToken`, não o middleware.
- **`Logout` é de propósito a única rota de sessão sem `AutenticacaoJwt`**: logout tem
  que apagar o cookie mesmo com token expirado/inválido. Exigir JWT ali devolveria 401 e
  deixaria o cookie morto no browser em loop.
- `LimitarPorIP(taxa, burst)` (`middleware/rateLimit.go`) é token bucket por IP com
  `x/time/rate`: mapa `IP → *rate.Limiter` sob mutex + goroutine que varre inativos a
  cada 5min (senão o mapa cresce sem fim). No login: `rate.Every(12*time.Second), 5`.
  Responde **429**, nunca 401/403.
- **Ordem no login: o limiter vem antes do `TenantMiddleware`.** Invertido, cada
  tentativa de força bruta gasta um `ObterEmpresaPorSubdominio` (ida ao banco) antes de
  o limite recusar — o ataque paga em CPU nossa. O limiter é em memória e não toca o
  banco, então é ele que fica na frente.
- **`ClientIP` só é confiável porque `main.go` chama `SetTrustedProxies`** com o que
  vier em `TRUSTED_PROXIES` (IPs/CIDRs separados por vírgula; vazio = não confia em
  ninguém e o `X-Forwarded-For` é ignorado). O padrão do Gin é confiar em **todo mundo**
  — aí o `X-Forwarded-For` vira campo livre do cliente, um header diferente a cada
  request ganha um bucket novo e o rate limit não limita nada. Já foi assim: 10 POSTs
  com `-H 'X-Forwarded-For: 10.0.0.$i'` passavam os 10; hoje passam 5 e o resto é 429.
- **Liste o endereço do proxy, nunca a faixa em volta dele.** O Gin caminha o
  `X-Forwarded-For` da direita pra esquerda e para no primeiro IP não-confiável — mas se
  a lista acabar, ele devolve o valor **mais à esquerda**, que é o que o cliente
  escreveu. Uma faixa larga que também contenha o cliente (ex: `172.16.0.0/12` numa rede
  Docker) faz a busca passar direto por ele e cair justamente no valor forjado. Por isso
  o compose dá IP fixo ao traefik e a api confia só em `172.29.0.2/32`. Em produção o
  valor é o endereço do proxy do Railway.
- ⚠️ Duas limitações conhecidas do limiter, aceitas por ora: (1) o estado é **em memória,
  por instância** — escalar o Railway pra 2 réplicas dobra o limite efetivo; (2) a chave
  é **só o IP** — escritório atrás de NAT compartilha as 5 tentativas, e ataque
  distribuído contra uma conta só não é limitado por nada. Se doer, a chave vira
  `IP+email` e o estado vai pro Redis.

## Queries e repository (sqlc)
- `database/queries/*.sql` — queries anotadas (`-- name: NomeQuery :one/:many/:exec`),
  um arquivo por tabela/domínio. `sqlc generate` (config em `sqlc.yaml`) gera
  `database/repository/` (`Querier` interface + structs) — **nunca edite
  `database/repository/` na mão**, é tudo gerado.
- Convenção de nomenclatura: em português, verbo primeiro (`CriarX`, `ObterXPorId`,
  `ListarX`, `AtualizarX`, `DeletarX`), espelhando o estilo do resto do Go do projeto.
- **Exclusão é sempre soft delete** — ver "Soft delete" em
  `docs/modelagem-banco-dados.md`. Não há `DELETE` de linha em lugar nenhum.
  ⚠️ A coluna é **`ativa`** na `loja`/`maquina`/`preventiva` (feminino) e **`ativo`** em
  `usuario`/`setor`.
  Em `preventiva` o soft delete não é só convenção: **`fk_solicitacao_preventiva` não tem
  `ON DELETE`**, então preventiva que já disparou uma solicitação automática recusa o
  `DELETE` com 23503 (testado). É por isso que a substituição do conjunto no
  `PUT /maquinas/:id` também desativa em vez de deletar — um `DELETE` em massa quebraria a
  edição de qualquer máquina cuja preventiva já tivesse vencido uma vez.
  ⚠️ Em `preventiva`, `ativa` acumula **dois sentidos**: o alternador "Preventiva
  habilitada no sistema" do modal e o soft delete do `DELETE /preventivas/:id`. Desabilitar
  e excluir produzem o mesmo estado. Consistente com "não existe reativação pela API", mas
  é decisão, não acidente.
- **Toda query de desativar é `:execrows`**, nunca `:exec`: sem a contagem de linhas,
  desativar um id inexistente (ou de outro tenant) responde sucesso igual a desativar um
  de verdade. `linhas == 0` → `ErrNaoEncontrado`. Já desativado casa a linha e conta 1,
  então desativar de novo é idempotente — e é assim que o front espera.
- **Não existe reativação pela API**: `ativo`/`ativa` só vai para `false`. É por isso que
  `DesativarLoja` recusa em vez de cascatear nos setores.
- `AtualizarSetor` **não** muda `loja_id`: mover setor de loja arrastaria junto máquinas,
  histórico de OS e o escopo de quem tem acesso a ele. Setor na loja errada se resolve
  desativando e criando na certa. O front manda `lojaId` no PUT; o service ignora.
- **Escopo de acesso (`usuario_escopo` + `usuario_escopo_setor`, ver
  `docs/modelagem-banco-dados.md` 3.8) edita substituindo o conjunto inteiro**, mesmo
  padrão de preventivas em `CadastrarMaquina`: o service apaga tudo do usuário
  (`DeletarSetoresDosEscoposPorUsuario` **antes de** `DeletarEscoposPorUsuario` — não há
  `ON DELETE CASCADE` entre as duas) e recria numa transação. Não existe
  `AtualizarEscopo` de propósito.
- **`NovoUsuarioPayload` é plano (um `setoresIds` só para N `lojasIds`), mas isso NÃO
  significa o mesmo conjunto de setores em toda loja**: o id do setor já identifica a
  loja (`setor.loja_id`), então a lista plana expressa escopo por loja — é só
  distribuir. `setoresPorLoja` (`EscopoPerfilService.go`) faz isso e recusa setor de
  loja não selecionada e loja que ficou sem setor nenhum. **Jogar `setoresIds` inteiro
  em cada `CriarEscopo` grava "acessa a Padaria da Loja A dentro da Loja B"** —
  `usuario_escopo_setor` só tem FK para `setor (id)`, não checa se o setor é da loja do
  escopo, então o banco aceita calado. Já foi bug uma vez.
- ⚠️ O que o payload plano **realmente** não expressa: acesso total numa loja e parcial
  noutra — `acessoTotalSetores` é um flag só, global. Um Gestor assim exige mudar o
  contrato para `escopos: [{lojaId, setoresIds}]` (front junto; `front-end/CLAUDE.md`
  item 7 registra a mesma lacuna, mas atribui a ela mais do que ela é).
- `ObterEscopoSessaoPorUsuario` já devolve no formato que o front consome
  (`EscopoAcessoGestor[]`, um `array_agg` de `setor_id` por loja) — é a query certa pra
  montar `SessaoUsuario.escoposGestor` no login/sessão.
  **`ObterEscoposSessaoPorUsuarios`** (plural) é a mesma coisa para uma lista de ids, e
  existe pro `ListarUsuarios` montar o escopo da página inteira numa ida só ao banco.
  Listagem nova que precise de escopo usa a plural — a singular é do caminho da sessão.
- **Filtro por loja em `ListarUsuarios`/`ContarUsuarios` é `EXISTS` sobre
  `usuario_escopo`, não `JOIN`**: com `JOIN`, um usuário com N escopos aparece N vezes e
  come o `LIMIT` com repetição. As duas queries repetem o mesmo `WHERE` de propósito —
  divergir dá `total` que não bate com a página. Efeito colateral correto: administrador
  não tem escopo nenhum, então some de qualquer listagem filtrada por loja.
- **`ListarTecnicos` (`usuario.sql`) tem o escopo com uma torção a mais**: os filtros
  `loja_id` (a loja da solicitação, do modal) e `escopo_usuario_id` (quem chama) vivem no
  **mesmo `EXISTS`, sobre a mesma linha de `usuario_escopo`**. Separados, eles pedem
  "atende a loja X" **E** "divide alguma loja comigo" — que não é a mesma coisa: um gestor
  da Loja A pedindo `?lojaId=B` receberia o técnico que atende A e B, porque cada condição
  passava por uma loja diferente. Foi assim que a primeira versão saiu, e o teste de
  integração pegou. `lojas_ids` sai de `array_agg ... FILTER` + `COALESCE` + cast — sem o
  cast o sqlc gera `interface{}`, mesma armadilha do `vencida` em `preventiva.sql`.
- **Escopo no `WHERE`, nunca no cliente (`ListarMaquinas`/`ListarPreventivas`):** as duas
  são abertas a qualquer perfil no RBAC, então quem recorta é a query. O parâmetro é
  `escopo_usuario_id`: **NULL não filtra** e é sempre o administrador (ele não tem linha em
  `usuario_escopo` — `trg_usuario_escopo_nao_admin` recusa — então filtrar por escopo
  devolveria zero justamente pra quem enxerga tudo); para os outros perfis vai o
  `usuario.id` do token e a linha só aparece se ele alcança **a loja E o setor**. Quem
  traduz é `escopoDe(usuarioId, perfil)` em `EscopoPerfilService.go`, e o `usuario.id`/
  `perfil` vêm de `atorDaRota` — nunca da query string.
  O mesmo `EXISTS` serve os três perfis porque `escopoDoPerfil` já os normaliza: o
  solicitante tem um escopo com um setor, o técnico escopos com `acesso_total_setores`, o
  gestor os setores marcados ou a loja inteira.
  ⚠️ **`EXISTS` e não `JOIN`**, mesmo motivo de `ListarUsuarios`: com JOIN a máquina
  apareceria uma vez por escopo que a alcança.
  ⚠️ **O filtro do cliente estreita, nunca amplia**: `?lojaId=` de uma loja fora do escopo
  devolve lista vazia, não a loja. É o que `TestEscopoDasListagens` tranca.
- `ObterLojaParaEscrita` é o `ObterLojaPorID` com **`FOR SHARE`**, usado dentro da
  transação que cria setor: sem o lock, alguém desativa a loja entre o cheque de `ativa`
  e o `INSERT`. `FOR SHARE` e não `FOR UPDATE` porque ninguém altera a loja ali.
- Onde o front fala **nome** e o banco guarda **id**, a tradução é do service, com query
  própria: `area_tecnico.sql` (`ObterAreaTecnicoPorNome` — `NovoUsuarioPayload.area` vem
  como o texto de `AreaTecnico` no front, `usuario.area_tecnico_id` é `smallint`) e
  `setor.sql` (`ObterSetorPorID` — `SessaoUsuario.setorNome`, já que `usuario_escopo`
  só guarda o `setor_id`; e `ObterSetoresPorIDs`, que devolve `loja_id` de cada setor
  para o service distribuir o escopo).
- **`foto_chave` entra em `CriarMaquina` direto e em `AtualizarMaquina` como
  `COALESCE(sqlc.narg('foto_chave'), foto_chave)`**: sem foto nova no multipart o valor
  chega NULL e a antiga fica. NULL ali significa "não mexi", não "apague" — o front só sabe
  *trocar* a foto, não remover (`UploadFoto` em `CadastrarMaquina.tsx`).
  ⚠️ Trocar a foto deixa o objeto antigo **órfão no bucket**: ninguém apaga do R2. É lixo
  barato e sem referência; se incomodar, o caminho é um `DELETE` do objeto antigo **depois**
  do commit (nunca antes — a transação pode voltar).
- **Leitura de máquina e preventiva traz os nomes por `JOIN`, sempre.** `ListarMaquinas`/
  `ObterMaquinaPorID` projetam `setor_nome`, `loja_id` e `loja_nome`; as de preventiva
  projetam ainda `maquina_nome`. Não é enfeite: o front tipa `setorNome`/`lojaId` como
  **obrigatórios**, e `maquina` **não guarda `loja_id`** — a loja só existe via setor. Os
  JOINs são INNER porque as FKs são NOT NULL; LEFT só faria o Go receber ponteiro em campo
  que nunca é nulo. `Criar`/`Atualizar` **não conseguem** fazer isso (`RETURNING` não
  enxerga tabela juntada), por isso releem por `Obter...PorID` dentro da transação.
- `maquina.criticidade` é o **ENUM `nivel_criticidade`** (`'Baixa','Média','Alta'`,
  migration `000004`), não FK pra tabela. Era tabela por tenant, mas nada customizava (o
  front tipa tupla fixa e não há tela de cadastro) e nenhuma migration a populava — a
  mesma lacuna de `area_tecnico`, que deixaria o cadastro de máquina travado em todo tenant
  novo. Como ENUM o valor nasce com o schema, e a ordem de declaração já dá o
  `ORDER BY criticidade` (enums do Postgres são ordenáveis) que a coluna `ordem` fazia.
  `nivel_urgencia` **continua tabela** de propósito: não está neste caminho e
  `ordem_servico` ainda não tem escrita.
- ⚠️ **Armadilha do sqlc em coluna calculada:** expressão booleana composta vira `*bool`,
  e `COALESCE` sozinho vira `interface{}`. Para sair `bool` limpo, **feche com cast**:
  `COALESCE(<expr>, false)::boolean AS x` — é o que `vencida` usa nas duas queries de
  preventiva. Manter a expressão **idêntica** nas duas também é o que deixa as rows
  geradas com a mesma forma, permitindo um `MontarPreventiva` só para as duas.
- **Data "de hoje" em SQL usa fuso explícito, não `CURRENT_DATE`:**
  `(now() AT TIME ZONE 'America/Sao_Paulo')::date`. O container roda em UTC, então com
  `CURRENT_DATE` uma preventiva apareceria vencida até 3h antes da virada do dia no Brasil.
- `AvancarProximaData` soma o intervalo **a partir da `proxima_data` vencida, não de hoje**
  — senão um ciclo processado com atraso arrastaria todos os seguintes (vencida há 5 dias
  com intervalo 30 vai pra hoje+25, não hoje+30).
- ⚠️ **`ListarPreventivasVencidas` é a única query do projeto sem `tenant_id` no `WHERE`**,
  e sem parâmetro nenhum: é do job, não de um request — não há token, e o `tenant_id` viaja
  na linha direto pro INSERT da solicitação. Não "conserte" adicionando filtro. Ver a seção
  do job para o papel de `m.ativa` e do `NOT EXISTS`.
- `database/queries/solicitacao_os.sql` existe hoje **só** com
  `CriarSolicitacaoPreventiva` — o resto da fase 1 (as duas criações humanas e as
  leituras) entra nele. `tipo` e `origem` são **literais no SQL, não parâmetros**:
  `ck_solicitacao_alvo` e `ck_origem` não deixam variar, e `solicitante_id` nem aparece
  na lista de colunas porque ali ele é *proibido*, não opcional.
  ⚠️ `maquina_id` e `preventiva_id` levam `::bigint` **de propósito**: as colunas são
  nullable no schema (precisam ser, para `reparo` e para a origem humana) e sem o cast o
  sqlc gera `*int64` no parâmetro — ponteiro num job que roda sozinho é só mais uma forma
  de gravar NULL por engano. Mesma família da armadilha do `vencida`.

**`area_tecnico` nasce populada (migration `000006`).** Todo tenant novo ganha as cinco
áreas que o front tipa (`areasTecnico`) por um **trigger `AFTER INSERT ON empresa`** —
`fn_seed_area_tecnico(tenant_id)`, com `ON CONFLICT DO NOTHING`, é a lista num lugar só e
serve o trigger e o backfill dos tenants que já existiam. Antes disso nada populava a
tabela e cadastrar técnico falhava em **todo** tenant com "área técnica ... não cadastrada
neste tenant" — sem técnico, o Gestor não abre OS nenhuma.
- **Trigger e não seed na migration** porque migration roda uma vez e tenant nasce depois
  dela (`make provisionar-admin`): só o seed deixaria todo tenant futuro travado de novo.
- **Não virou ENUM como `nivel_criticidade`** (que tinha o mesmo sintoma): a seção 2.4 de
  `docs/modelagem-banco-dados.md` dá uma razão explícita para esta continuar tabela — "um
  supermercado pode querer 'Automação' onde outro quer 'Ar-condicionado'". As cinco áreas
  são ponto de partida, não lista fechada; área inexistente continua sendo 422.
- A migration também criou o **`uq_area_tecnico_nome (tenant_id, nome)`** que faltava (sem
  ele o seed rodando duas vezes duplicaria a área, e `ObterAreaTecnicoPorNome` é `:one` —
  devolveria "a primeira" das duas, calado) e **deduplica antes de criá-lo**, repontuando
  `usuario.area_tecnico_id` para a linha sobrevivente: `UNIQUE` em coluna com duplicata é o
  jeito clássico de a migration falhar no boot e derrubar a API.
- Consequência nos testes: `bancoDeTeste` não precisa mais inserir área na mão — a empresa
  do seed já nasce com elas.

## O que falta no back (retomar aqui)

Cadastros: **completos**. Falta o miolo do fluxo, na ordem em que o front precisa:

1. **Solicitações** — `POST /solicitacoes/maquinario` e `/reparo` (multipart, use
   `corpoMultipart[T]`), `GET /solicitacoes/minhas` (paginado), `GET /solicitacoes`,
   `/:id`, `/resumo`, `POST /:id/abrir-os` e `/:id/rejeitar`. Junto vem o CRUD de
   `solicitacao_anexo` (nada em `database/queries/` ainda) e o `URLLeitura` resolvendo
   `chave` → `url` na resposta, como já é feito em máquina.
2. **Ordem de serviço** — o ciclo de vida (`iniciar`/`pausar`/`retomar`/
   `acionar-terceiro`/`encerrar`/`custo`), os dois relógios e a flag `finalizada`.
   Destrava os dois cards mortos do painel do Administrador.
3. **Indicadores** (`GET /indicadores/maquinas/:id`). O **job de preventiva vencida**
   saiu desta lista — está pronto e testado, falta só o Cron Job no Railway (ver seção
   abaixo).

Listagem nova que precise recortar por escopo usa `atorDaRota` no controller +
`escopoDe(usuarioId, perfil)` no service, com o `EXISTS` no `WHERE` — ver "Escopo no
`WHERE`" em "Queries e repository". Rota com arquivo usa `corpoMultipart`, sem arquivo usa
`corpoJSON`.

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

## Ambiente local
- `.env` na raiz: `DB_SERVER`, `DB_USER`, `DB_PORT`, `DATABASE`, `DB_PASSWORD`
  (`DB_SSLMODE` opcional, default `disable`) e `JWT_SECRET`. `TRUSTED_PROXIES` **não**
  fica no `.env`: vem do `environment` do compose em dev (é endereço de infra, muda com
  a topologia) e do ambiente do Railway em produção.
- Dentro da rede Docker do projeto, o Postgres resolve por `DB_SERVER=postgres`,
  `DB_PORT=5432` — é o que `api_sistema-OS` usa. Para testar comandos Go pontualmente,
  rode dentro do container já ativo: `docker exec api_sistema-OS go run . ...`.
- **A porta 5431 no host é obrigatória, não estética**: o projeto vizinho
  `sgeepi-infra` já publica `0.0.0.0:5432->5432` com o `postgres_container` dele. Duas
  aplicações não dividem a mesma porta do host, então este compose sai da frente e usa
  a 5431. Não "conserte" isso publicando na 5432 — os dois bancos brigam e o que subir
  depois não sobe.
- ⚠️ **O que está furado é o lado direito do mapeamento, não o esquerdo.** O compose diz
  `5431:5431`, mas dentro do container o Postgres escuta **só na 5432** (imagem
  `postgres:16-alpine` sem `-p` no command; confira com
  `docker exec postgres_container-sistema-OS psql -U postgres -tAc "show port"`). Ou
  seja: a porta publicada não tem ninguém do outro lado e `localhost:5431` **não conecta
  do host** — dá `connection reset by peer`, porque o proxy do Docker aceita e não acha
  upstream. O conserto mantém a sua escolha de porta: **`5431:5432`** (host 5431, livre
  do conflito; container 5432, onde o Postgres está).
- Enquanto o mapeamento não for corrigido, os testes de integração precisam falar direto
  com o IP do container:
  `TEST_DB_DSN='postgres://postgres:postgres@172.29.0.3:5432/postgres?sslmode=disable'`.
  Sintoma de esquecer: `go test ./...` **verde sem ter rodado a integração** (`t.Skip`
  silencioso) — exatamente o que o job de CI existe pra evitar.
- O compose (`../docker-compose.yml`, um nível acima deste repo) sobe **front + api atrás
  de um traefik**: `http://<tenant>.localhost:8090` serve o Vite, e `/api` cai na api.
  Dashboard do traefik em `:8091`, pgadmin em `:5051`. **`api` e `front` não publicam
  porta** — quem publica é o traefik.
- **Front e API na mesma origem é requisito, não arrumação.** O front tira o tenant de
  `window.location.hostname.split('.')[0]`, então precisa ser servido de
  `<tenant>.localhost`; e o cookie de sessão é `SameSite=Lax`, mas pro browser
  `a.localhost` e `b.localhost` são **sites diferentes** (`localhost` se comporta como
  TLD) — front e API em subdomínios distintos não trocariam cookie em dev. Em produção
  o problema some porque `radaptech.com.br` é o domínio registrável comum, e aí a API
  pode viver em `api.radaptech.com.br`.
- Por isso o compose passa `REACT_APP_URL_API=/api` pro front (o `environment` do
  compose vence o `.env` no `loadEnv` do Vite). `front-end/src/servicos/api.ts` deixa
  valor começado por `/` passar como mesma origem — sem isso ele forçaria `https://` e
  montaria `https:///api`.
- ⚠️ **Há um segundo traefik na máquina** (projeto `sgeepi-infra`, portas 80/8080) e os
  dois leem o mesmo socket do Docker. A separação é dos dois lados, e quebra se alguém
  mexer nela: o traefik daqui só aceita containers com o label de projeto `sistema-os`
  (`--providers.docker.constraints`), e os routers daqui usam o entrypoint `websos`,
  que não existe lá — o traefik vizinho até descobre os containers, mas marca os
  routers como `disabled` ("entryPoint websos doesn't exist"). Renomear o entrypoint ou
  tirar a constraint faz os dois brigarem por `*.localhost`.
- A sub-rede do compose é declarada (`172.29.0.0/16`) só pra o traefik ter IP fixo
  (`172.29.0.2`) e a api poder confiar exatamente nele — ver `TRUSTED_PROXIES` em
  "Rotas e rate limit". Mudar a sub-rede sem mudar o `TRUSTED_PROXIES` deixa o
  `ClientIP` preso no IP do proxy (um bucket de rate limit pro mundo inteiro).

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
  - **Os dois têm o teste "ator vem do token"**: `usuario.id`/`perfil` chegando da query em
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
    `escopoListagemIntegracao_test.go` —
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

## Deploy e produção (decisões tomadas, retomar aqui)

**Não existe servidor de homologação, e é decisão consciente.** Staging serve pra pegar
o que localhost não pega; neste projeto isso é conexão com o banco gerenciado, cookie
`Secure`/`SameSite` no domínio real, CORS e `TRUSTED_PROXIES` com o proxy do Railway —
quatro coisas de **configuração de ambiente, que se erram uma vez só**. Não sustentam um
ambiente permanente. No plano Hobby ainda custam dinheiro: staging seria um segundo
Postgres + segunda API 24/7, dobrando o consumo em cima de um crédito de $5.

**O primeiro deploy É a homologação.** A janela entre subir e o admin começar a cadastrar
é o ambiente de teste: mesma infra, mesmo código, banco ainda descartável. Suba,
provisione um tenant de teste (`make provisionar-admin`), exercite os fluxos, apague o
tenant, entregue.

### Estado em 22/08/2026 — pronto para subir, com uma condição

Todo o CRUD do Administrador está de pé e **validado pelo navegador contra a API real**
(não só por curl): login/cookie, lojas, setores em cascata, os quatro perfis de usuário,
máquina com foto subindo pro R2 e voltando assinada no `<img>`, preventiva, terceirizada.
Ver "Verificação pelo navegador" em `../sistema-OSm--Front-end/CLAUDE.md` para os bugs que essa
passagem encontrou.

**A condição é o backup.** Enquanto não há dado dentro ele é opcional; no minuto em que o
admin cadastrar a primeira loja, passa a ser a diferença entre um susto e recomeçar do
zero (Hobby não tem PITR).

**Resolvido em código e testado local (23/08/2026), falta só produção.** O subcomando
`backup-banco` (ver "Backup do banco" acima) faz `pg_dump` + upload pro R2 — testado local
de ponta a ponta contra o R2 e o Postgres reais, bucket `backups-cooprata` já criado no
Cloudflare, upload confirmado sem erro, e o restore também testado (`pg_restore` contra
banco descartável, contagem de linhas batendo). Ganhou também um `dockerfile` de
produção próprio (multi-stage, sem CompileDaemon — ver "Dois Dockerfiles"), testado
local com `docker run <imagem> backup-banco` rodando limpo. O que falta pra fechar o
bloqueio: (1) configurar o Cron Job no Railway apontando pra esse `dockerfile` (não o
`dockerfile.dev`) com Custom Start Command `./main backup-banco`; (2) rodar o restore ensaiado
**uma vez contra o dump que sai do R2 de produção**, não só contra o teste local.

**Ordem de execução do primeiro deploy:**

1. Variáveis no serviço da API: `DB_*` (host do **Session Pooler** do Supabase, porta
   `5432`, usuário `postgres.<project-ref>`),
   `DB_SSLMODE`, **`JWT_SECRET` novo** (`openssl rand -base64 64` — não reaproveitar o do
   `.env` local, que já circulou), `TRUSTED_PROXIES` com o endereço do proxy do Railway, e
   os quatro `R2_*` (sem eles o CRUD funciona e só o upload de foto responde 500 — a
   guarda de nil em `bucketR2` evita o panic).
2. ⚠️ **A URL da API é congelada no BUILD do front**, não em runtime: o `define` do
   `vite.config.ts` resolve `process.env.REACT_APP_URL_API` em build time. Setar a
   variável só no runtime do serviço não adianta — ela precisa existir no `vite build`,
   senão o bundle sai com o fallback compilado dentro. `VITE_USE_MOCKS` ausente ou `false`.
3. Subir a API primeiro e conferir o boot (as migrations rodam sozinhas; `000006` cria
   trigger + constraint). Falha aqui = API não sobe, com restart automático virando crash
   loop — tenha o rollback do Railway à mão.
4. `provisionar-admin` contra o banco de produção (`railway run`), com o subdomínio real.
5. Só então apontar o DNS do front.

**Front e API sob o mesmo domínio registrável** (`*.radaptech.com.br` nos dois, ex:
`app.` + `api.`) — decisão fechada. O cookie é `SameSite=Lax`: em domínios registráveis
diferentes o login responde 200 e o navegador **descarta o cookie**, sem erro visível. O
CORS (`middleware/cors.go`) também só libera `radaptech.com.br` e `localhost`.

**Resolvido no front (23/08/2026):** os cards "Custos Pendentes" e "OS Finalizadas" do
painel do Administrador chamavam `/ordens-servico`, que **não existe** aqui — o admin
clicava e recebia toast de erro. Os dois **saíram da Home do painel**; as telas e as rotas
do front continuam prontas, esperando este back. Quando a fase 2 (Ordem de serviço) subir,
o gesto no front é um `git revert` do commit que os removeu — não recriar os cards na mão.
Deixa de ser aceite consciente para entregar o acesso.

### Antes de entregar pro admin
- **Backup com restore ensaiado.** O mecanismo (`backup-banco`, `pg_dump` → R2) está
  pronto e testado local de ponta a ponta, bucket no Cloudflare já criado — falta o Cron
  Job no Railway e um restore ensaiado **contra o dump real de produção**, não só o
  local. Backup nunca testado com o dump de verdade não é backup.
- **A partir daqui, toda migration é migration com dado dentro.** A regra de testar
  `up`+`down` num Postgres descartável (ver "Migrations") sobe de nível: teste contra um
  **restore do dump de produção**, não contra banco vazio. O que passa no vazio e quebra
  com dado é sempre o mesmo: `NOT NULL` sem default, `UNIQUE` em coluna que já tem
  duplicata, `CHECK` novo em linha antiga.
- **Faça a bagunça de schema antes.** Buraco de modelagem descoberto depois do handover
  vira `ALTER` com dado. Dois já apareceram assim: empresa×loja (resolvido: empresa É o
  tenant) e o `os_evento` que `docs/modelagem-banco-dados.md` lista em "Pontos em aberto".

### Riscos operacionais conhecidos
- ⚠️ **Migration falha = produção fora do ar, não feature quebrada.** `main.go` roda
  `RunMigrationPostgress` no boot e faz `log.Fatal` no erro — a API não sobe, e com
  restart automático vira crash loop. Tenha o rollback do deploy do Railway à mão. É o
  preço de migrar no boot, que continua valendo a pena enquanto o deploy for um só.
- **O Railway redeploya sozinho a cada push do branch conectado.** Com o admin cadastrando
  em produção enquanto o resto do software é construído, isso é um restart no meio do
  formulário dele. Trabalhe em `dev` e conecte o Railway só ao `master` (o CI já roda nos
  dois). Merge pra `master` passa a ser o gesto de "quero publicar isto".
- **Tenant de teste em produção é o staging dos pobres.** O sistema é multi-tenant: um
  `teste.<dominio>` exercita fluxo com dado descartável, isolado do tenant real, sem
  custo nenhum. Não cobre erro de schema — migration pega todos os tenants.

### R2 — storage de anexos (parcialmente implementado)
Schema e cliente R2 prontos (`bucketR2/`, migration `000003_anexo_chave_r2`); falta o
CRUD que os usa. Decisões já fechadas, não reabrir sem motivo novo:
- **Key, não URL, prefixada por tenant** (`tenant/{id}/{timestamp}{ext}`, ver
  `bucketR2.UploadFoto`) e **URL assinada de leitura** gerada na hora (`bucketR2.
  URLLeitura`), com TTL curto — nunca persistida. Bucket público num sistema
  multi-tenant deixaria qualquer um com o link ver a foto de outro tenant, e uma URL
  persistida acumula link morto sem indicar que quebrou (docs/modelagem-banco-dados.md
  3.10). O contrato do front não muda: `fotoUrl`/`AnexoSolicitacao.url` continuam string,
  só que resolvida no service a partir da key, nunca devolvida crua.
- **Colunas já renomeadas** (migration `000003`): `maquina.foto_url` → `foto_chave`,
  `solicitacao_anexo.url` → `chave`. Sem coluna `bucket` — cada tipo de anexo sobe pra
  um bucket fixo, escolhido no código que registra a rota (`UploadFoto(url, bucket)`),
  não varia por linha.
- **Egress do R2 é grátis** — é o motivo de ele estar aqui. Sirva o arquivo direto pro
  browser; nunca faça proxy pela API.
- **Vídeo passando pelo container é o que vai doer primeiro.** O contrato hoje manda
  multipart pra API (`POST /maquinas`, as três criações de solicitação) e o Gin bufferiza
  32MB por padrão — no Hobby isso é RAM e CPU que não sobram. `UploadFoto` já limita em
  10MB (`tamanhoMaximoFoto`, com `http.MaxBytesReader` — sem ele o `ParseMultipartForm`
  só limita o que fica em memória, não o tamanho do request). Mantenha multipart por
  enquanto e troque por `PUT` assinado direto do browser quando incomodar (muda o
  contrato, precisa do front junto).

**O que falta pra fechar o fluxo:** wirear `UploadFoto`/`URLLeitura` numa rota real
(`POST /maquinas`, as criações de solicitação), o CRUD de `solicitacao_anexo` (nada em
`database/queries/` ainda — o de `maquina` já existe), e o service resolvendo
`foto_chave`/`chave` em `fotoUrl`/`url` assinada na resposta. Validação de
content-type/extensão também não existe — `UploadFoto` aceita qualquer arquivo enviado no
campo `foto`.

⚠️ **`MontarListaMaquinarios` copia `foto_chave` direto para `FotoUrl`** — ou seja, o
service devolve a **chave**, não a URL. Quem troca é o controller, em `resolverFoto`, nos
quatro caminhos que respondem uma máquina (POST, PUT, GET lista, GET por id). Falhar a
assinatura **não vira 500**: a máquina já está no banco, e erro depois do commit faria o
front mostrar falha para um cadastro que existe — some só a foto (`fotoUrl` é opcional no
tipo do front). O que não pode, em hipótese alguma, é a chave crua sair na resposta; há
teste trancando isso.

## Regras herdadas do contrato com o front (não reinvente)
- **401 é só "sem sessão"**; fora de escopo/perfil errado é sempre **403** — 401 fora de
  `/login` desloga o usuário no front. O RBAC por perfil é `middleware.Permitir(...)`;
  o escopo (loja/setor) **não** é middleware, é o `WHERE` da query.
- **Datas** trafegam como texto `dd/mm/yyyy HH:MM:SS` (ou `dd/mm/yyyy` sem hora) — use
  `config.DataBr` em todo campo de data de resposta, nunca `time.Time` cru.
- **Multi-tenant por subdomínio**: header `X-tenant-ID`, tenant nunca vem por rota nem
  por corpo.
- **Empresa É o tenant** (decisão fechada). `loja.tenant_id` referencia `empresa (id)`
  direto, não existe coluna `empresa_id` nem tabela intermediária — o front já tipa a
  hierarquia como "Tenant = Empresa > Loja > Setor". Consequências:
  `GET /empresas` devolve **uma lista de um item só** (a empresa do tenant autenticado,
  `id` = `tenant_id`), e `Loja.empresaId` é o `tenant_id` da própria linha — campo
  derivado, não coluna, que o front usa só para exibir o nome da empresa. **O front já
  não manda `empresaId`**: `NovaLojaPayload` (lá) perdeu o campo e o select de empresa em
  `CadastrarLoja` virou campo somente leitura, já que a lista sempre teve um item só.
  Mesmo assim, **`empresaId` no corpo de `POST/PUT /lojas` continua ignorado aqui**: como empresa = tenant, aceitá-lo do cliente seria aceitar o tenant do
  corpo — o mesmo buraco do `X-tenant-ID` em rota autenticada, por outra porta.
- **Escopo (loja/setor/técnico) é sempre filtrado no `WHERE` do servidor**, nunca
  devolvido inteiro pro cliente filtrar.
- **Transições de estado são `POST` em sub-recurso** (`/iniciar`, `/pausar`,
  `/acionar-terceiro`, etc.), nunca `PATCH` de um campo `status` genérico.
