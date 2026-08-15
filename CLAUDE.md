# Contexto do Projeto - Back-end (Solicitação OS)

API do sistema de manutenção/OS para redes de varejo, consumida pelo front-end em
`../front-end` (React). Este back-end está em construção — ver `docs/` para o roteiro
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
- Infra alvo: Railway (app) + Supabase (Postgres + Storage). Local: Docker Compose
  (`postgres_container-sistema-OS`, `api_sistema-OS` com hot-reload via CompileDaemon,
  `pgadmin_container-sistema-OS`).

## Documentos de referência (leia antes de implementar endpoint novo)
- `docs/modelagem-banco-dados.md` — modelo de dados completo, revisão 4.1, com todo o
  raciocínio de cada constraint/trigger/view.
- `docs/der-banco-dados.mmd` (+ `.svg`/`.png` gerados) — diagrama ER.
- Artefatos publicados (fonte de verdade do contrato HTTP — pedir link se precisar):
  DER revisado, Contrato de API v1.2 (53 endpoints), RBAC, PRD, Roteiro do Back-end
  (16 fases). Ver `../front-end/CLAUDE.md` para a lógica de negócio do ponto de vista
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
  porta 8081. Também despacha subcomandos de CLI antes de subir o servidor (ver abaixo).
  `proxiesConfiaveis()` (mesmo arquivo) alimenta o `SetTrustedProxies` — ver "Rotas e
  rate limit".
- `config/` — `VariaveisDeAmbiente` (lê `.env`), `ConnPostgresql` (pool + migrations),
  `DataBr` (tipo de data custom, layout `02/01/2006 15:04:05`, nunca RFC3339).
- `auth/` — `jwt.go` (`GerarJwt`, claims `sub`/`tenantId`/`perfil`/`exp`/`iat`,
  HS256), `passHash.go` (`HashPassword`/`HashCompare`, argon2id).
- `middleware/` — `middJwt.go` (`AutenticacaoJwt`, lê cookie `token` ou
  `Authorization: Bearer`, valida e injeta `userId`/`user_perfil`/`user_TenantId` no
  contexto do Gin — falha fechada: claim ausente/malformada aborta com 401; as chaves
  são as consts `UserId`/`UserPerfil`/`UserTenantId`, e `GetUserID`/`GetTenantIDToken`
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
  (`NovoUsuarioPayload`, `AtualizarUsuarioPayload`, `Usuario`).
- `internal/helper/Errors.go` — `TraduzErroPostgres`: converte código de erro do
  Postgres (`23505`, `23503`, `23502`, ...) em erro de negócio (`ErrDadoDuplicado`,
  `ErrConflitoIntegridade`, ...) pros controllers não fazerem `switch` em `pgErr.Code`
  espalhado pelo código. `ErrCredenciaisInvalidas` é o erro único do login — ver
  "Autenticação" abaixo. `ErrValidacao` é o sentinela que os erros de regra de negócio
  do service embrulham com `%w` (`validarEscopo`, `setoresPorLoja`) pro controller
  responder 400 sem olhar o texto do erro.
- `internal/service/` — regras de negócio. `UsuarioService` guarda o `*pgxpool.Pool`
  (não um `Querier`): as escritas precisam de transação e `Querier` não expõe `WithTx`,
  então cada método monta o seu `repository.New(tx)`.
  - `loginService.go` — `CadastrarUsuario` (usuário + escopo numa transação só),
    `Login` e `ObterSessao` (o `GET /autenticacao/sessao`: `ObterUsuarioPorID` **não**
    filtra `ativo` como o `ObterUsuarioPorEmail`, então o `!user.Ativo` está explícito
    ali; usuário sumido ou desativado devolve `ErrSessaoExpirada`).
  - `EscopoPerfilService.go` — o que é por perfil e não por endpoint: `validarEscopo`
    (cardinalidade de 3.8, que as tags de `binding` não alcançam), `escopoDoPerfil`
    (normaliza o payload plano do front), `setoresPorLoja` (distribui cada setor no
    escopo da loja certa — ver "Queries e repository") e `montarSessao` (o corpo de
    `SessaoUsuario`, compartilhado entre `POST /autenticacao/login` e
    `GET /autenticacao/sessao`).
- `controller/` — `loginController.go`: `LoginController` recebe um
  `LoginServiceInterface` (a interface existe pro teste do handler poder trocar o
  service — `UsuarioService` guarda `*pgxpool.Pool` concreto, não dá pra mockar de
  outro jeito). Handlers: `Registrar` (`POST /usuarios`), `Login`, `Logout`, `Sessao`.
  O cookie de sessão sai só de `cookieSessao(ctx, token, maxAge)` — login e logout
  passam pela mesma função porque o `Set-Cookie` de remoção só apaga se casar com o de
  criação (nome, path, `Secure`, `SameSite`).
- `internal/router/router.go` — `Container` (injeção: guarda os controllers montados +
  o `*repository.Queries` que o `TenantMiddleware` precisa) e `ConfigurarRotas`. Ver
  "Rotas e rate limit".
- `database/migrate/` — migrations SQL puro (`golang-migrate`), numeradas e sequenciais.
- `database/queries/` + `database/repository/` — ver "Queries e repository (sqlc)"
  abaixo.

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
- **Ainda falta revogar sessão antes do `exp`** — desativar usuário, trocar senha e
  logout não matam um token já emitido, e `HttpOnly` não impede o dono do navegador de
  copiar o cookie no DevTools (ele protege contra XSS, não contra o usuário). O `exp`
  de 8h limita a janela a um turno. Conserto barato quando for mexer: `token_version`
  na `usuario`, o JWT carrega o valor e o middleware compara — dá pra juntar na mesma
  query que o RBAC de escopo vai precisar fazer.

## Rotas e rate limit
- Tudo em `internal/router/router.go`, sob o grupo `/api`. Registradas hoje:
  `GET /api` (healthcheck), `POST /autenticacao/login`, `POST /autenticacao/logout`,
  `GET /autenticacao/sessao`, `POST /usuarios`.
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
- **Exclusão é sempre soft delete** (`ativo = false`) — ver "Soft delete" em
  `docs/modelagem-banco-dados.md`. Não há `DELETE` de usuário; só `DesativarUsuario`.
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
- Onde o front fala **nome** e o banco guarda **id**, a tradução é do service, com query
  própria: `area_tecnico.sql` (`ObterAreaTecnicoPorNome` — `NovoUsuarioPayload.area` vem
  como o texto de `AreaTecnico` no front, `usuario.area_tecnico_id` é `smallint`) e
  `setor.sql` (`ObterSetorPorID` — `SessaoUsuario.setorNome`, já que `usuario_escopo`
  só guarda o `setor_id`; e `ObterSetoresPorIDs`, que devolve `loja_id` de cada setor
  para o service distribuir o escopo).

⚠️ **`area_tecnico` não é populada por nada hoje** — nem migration de seed, nem
`ProvisionarAdministrador`, nem CRUD. Cadastrar técnico falha com "área técnica não
cadastrada neste tenant" até existir uma dessas.

## Ambiente local
- `.env` na raiz: `DB_SERVER`, `DB_USER`, `DB_PORT`, `DATABASE`, `DB_PASSWORD`
  (`DB_SSLMODE` opcional, default `disable`) e `JWT_SECRET`. `TRUSTED_PROXIES` **não**
  fica no `.env`: vem do `environment` do compose em dev (é endereço de infra, muda com
  a topologia) e do ambiente do Railway em produção.
- Dentro da rede Docker do projeto, o Postgres resolve por `DB_SERVER=postgres`,
  `DB_PORT=5432` — é o que `api_sistema-OS` usa. No host, o compose publica em
  `localhost:5431`. Para testar comandos Go pontualmente, rode dentro do container já
  ativo: `docker exec api_sistema-OS go run . ...`.
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
    respondia 200 vazio, sem cookie, e o erro escrevia dois corpos.
  - `middleware/perfil_test.go` — `Permitir` com um perfil, vários, nenhum, e o caso de
    falha fechada (contexto sem perfil **nega**, protege contra montar o middleware na
    ordem errada).
  - `middleware/tenantId_test.go` — só os ramos que abortam antes de tocar no banco
    (header ausente/em branco → 400, `www`/`api` → 403), por isso passa `nil` como
    `*repository.Queries`. O `TestGetTenantID` existe porque o cast já esteve em `int32`
    e nunca casava com o `bigint` de `empresa.id` — falhava calado devolvendo `false`.
- Dois níveis em `internal/service/`:
  - `loginService_test.go` — unitário, sem banco: tabela cobrindo `validarEscopo` +
    `escopoDoPerfil` nos 4 perfis.
  - `loginIntegracao_test.go` — integração de verdade contra Postgres. `bancoDeTeste`
    cria um banco descartável (`teste_login_<pid>`), aplica as migrations nele e dropa
    no fim; os usuários do seed são criados via `CadastrarUsuario`, então o caminho de
    escrita entra junto. **Sem Postgres alcançável ele dá `t.Skip`**, não falha.
- O DSN vem de `TEST_DB_DSN`; o default é a porta publicada no host pelo compose
  (`localhost:5431`). De dentro da rede docker ou em CI:
  `TEST_DB_DSN=... go test ./internal/service/`.
- **A ordem dos dois `t.Cleanup` em `bancoDeTeste` é proposital** (`t.Cleanup` roda em
  LIFO: o `migrate` fecha antes do `pool`). Invertida, `pool.Close()` espera para sempre
  pela conexão que o `migrate` ainda segura e o teste **pendura em vez de falhar** — o
  banco descartável também fica órfão. Se um teste travar em ~nada de saída, é isso.
- Não há mock do `repository`: o service guarda `*pgxpool.Pool` concreto. Regra de
  negócio pura vai em função livre (como `validarEscopo`) justamente pra ser testável
  sem banco.

## CI
- `.github/workflows/ci.yml` — push em `master`/`dev` e todo PR: `gofmt` (falha se algum
  arquivo estiver fora de formato), `go vet`, `go test -race ./...`.
- **O job sobe um service container de Postgres 16 e exporta `TEST_DB_DSN`** apontando
  pra `localhost:5432`. Sem ele o `loginIntegracao_test.go` daria `t.Skip` e o CI
  passaria verde tendo rodado só os unitários — pior que não ter CI. `JWT_SECRET`
  também vai no env do job: `AutenticacaoJwt` dá `panic` sem ela.
- **Não há job de deploy de propósito**: o Railway redeploya sozinho no push do repo
  conectado. Um job aqui seria uma segunda fonte da verdade + um `RAILWAY_TOKEN` pra
  guardar. Migrations rodam no boot (`main.go`), então também não precisam de passo.

## Regras herdadas do contrato com o front (não reinvente)
- **401 é só "sem sessão"**; fora de escopo/perfil errado é sempre **403** — 401 fora de
  `/login` desloga o usuário no front. O RBAC por perfil é `middleware.Permitir(...)`;
  o escopo (loja/setor) **não** é middleware, é o `WHERE` da query.
- **Datas** trafegam como texto `dd/mm/yyyy HH:MM:SS` (ou `dd/mm/yyyy` sem hora) — use
  `config.DataBr` em todo campo de data de resposta, nunca `time.Time` cru.
- **Multi-tenant por subdomínio**: header `X-tenant-ID`, tenant nunca vem por rota nem
  por corpo.
- **Escopo (loja/setor/técnico) é sempre filtrado no `WHERE` do servidor**, nunca
  devolvido inteiro pro cliente filtrar.
- **Transições de estado são `POST` em sub-recurso** (`/iniciar`, `/pausar`,
  `/acionar-terceiro`, etc.), nunca `PATCH` de um campo `status` genérico.
