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
- Infra alvo: **Railway** (app + Postgres, plano Hobby) e **Cloudflare R2** (bucket de
  fotos/vídeos, S3-compatível). O Postgres do Railway é instância direta, sem pooler no
  meio — `pgx`/`sqlc` conectam sem ajuste de prepared statements. Aponte
  `DB_SERVER`/`DB_PORT`/... pras variáveis do plugin de Postgres e use o host **interno**
  (`*.railway.internal`, rede privada IPv6), não o proxy público.
  ⚠️ **Hobby não tem PITR**: backup é `pg_dump` agendado mandando pro R2, com restore
  ensaiado — não existe rede de proteção gerenciada aqui. Local: Docker Compose
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
  (`NovoUsuarioPayload`, `AtualizarUsuarioPayload`, `Usuario`), `loja.go` (`Loja`,
  `Empresa`, `NovaLojaPayload`), `setor.go` (`Setor`, `NovoSetorPayload`),
  `paginacao.go` (`RespostaPaginada[T]`, genérico — espelha `RespostaPaginada<T>` do
  front; só `GET /usuarios` usa hoje).
  **Toda struct de resposta precisa do `id` e das tags camelCase**: sem tag o Go
  serializa `Nome` e o front lê `undefined`, e sem `id` a listagem não serve pra nada
  (é o `value` do select, o `/:id` do botão editar e o que vai pro escopo). Já
  aconteceu com `Loja` e `Setor`.
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
- `controller/` — `loginController.go`: `LoginController` recebe um
  `LoginServiceInterface` (a interface existe pro teste do handler poder trocar o
  service — `UsuarioService` guarda `*pgxpool.Pool` concreto, não dá pra mockar de
  outro jeito). Handlers: `Registrar` (`POST /usuarios`), `Login`, `Logout`, `Sessao`.
  O cookie de sessão sai só de `cookieSessao(ctx, token, maxAge)` — login e logout
  passam pela mesma função porque o `Set-Cookie` de remoção só apaga se casar com o de
  criação (nome, path, `Secure`, `SameSite`). Mais `Obter`/`ListarUsuarios`/
  `Atualizar`/`Desativar`, e os helpers de pacote `idDaRota` (`:id` malformado é **400**,
  não 404 — `/abc` não é um id que não existe, é bug de cliente) e `tenantDaRota`.
  `lojaController.go` e `setorController.go` seguem o mesmo molde (interface própria +
  `corpoLoja`/`corpoSetor`).
  **Mapa de erro → status**: `ErrValidacao` 400, `ErrDadoDuplicado` 409,
  `ErrConflitoIntegridade` 422, resto 500 **com o erro cru só no `log`**.
  `ErrNaoEncontrado` é **404 quando o `:id` da rota é a única coisa que pode faltar**
  (loja, setor, `GET`/`DELETE /usuarios/:id`) e **422 em `POST`/`PUT /usuarios`**, onde o
  mesmo sentinela também cobre "área técnica citada no corpo não existe" e `errors.Is`
  não distingue os dois.
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
  faltar: nesse ponto o `AutenticacaoJwt` já devia ter garantido ela, então `!ok` aqui é
  bug de wiring, não sessão inválida) e `ContentType` do header do arquivo (senão o R2
  serve como `application/octet-stream` e o browser força download em vez de exibir).
  `URLLeitura(ctx, bucket, key string, ttl time.Duration) (string, error)` gera a URL
  assinada de leitura via `presignClient.PresignGetObject` — é o que resolve
  `maquina.foto_chave`/`solicitacao_anexo.chave` (guardados como key, não URL — ver
  "R2 — storage de anexos" abaixo) num `fotoUrl`/`url` de resposta. **Ainda não está
  wireado no router** — nenhuma rota chama `UploadFoto` nem `URLLeitura` hoje; é infra
  pronta esperando o CRUD de `maquina`/`solicitacao_anexo`.

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
  | `GET /lojas`, `GET /setores` | **qualquer perfil autenticado** |
  | `GET·PUT·DELETE /lojas/:id`, `POST /lojas` | administrador |
  | `GET·PUT·DELETE /setores/:id`, `POST /setores` | administrador |

- **As duas listagens sem `Permitir` são de propósito**: o painel do gestor agrupa por
  loja e nomeia os blocos por setor (`acessoGestor.ts` procura o setor por id), e os
  selects em cascata de cadastro dependem das duas. Restringir a administrador deixa o
  painel do gestor sem nomes. Escrever continua só do administrador.
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
  ⚠️ A coluna é **`ativa`** na `loja` (feminino) e **`ativo`** em `usuario`/`setor`.
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
- `ObterLojaParaEscrita` é o `ObterLojaPorID` com **`FOR SHARE`**, usado dentro da
  transação que cria setor: sem o lock, alguém desativa a loja entre o cheque de `ativa`
  e o `INSERT`. `FOR SHARE` e não `FOR UPDATE` porque ninguém altera a loja ali.
- Onde o front fala **nome** e o banco guarda **id**, a tradução é do service, com query
  própria: `area_tecnico.sql` (`ObterAreaTecnicoPorNome` — `NovoUsuarioPayload.area` vem
  como o texto de `AreaTecnico` no front, `usuario.area_tecnico_id` é `smallint`) e
  `setor.sql` (`ObterSetorPorID` — `SessaoUsuario.setorNome`, já que `usuario_escopo`
  só guarda o `setor_id`; e `ObterSetoresPorIDs`, que devolve `loja_id` de cada setor
  para o service distribuir o escopo).

⚠️ **`area_tecnico` não é populada por nada hoje** — nem migration de seed, nem
`ProvisionarAdministrador`, nem CRUD. Cadastrar técnico falha com "registro não
encontrado: área técnica ... não cadastrada neste tenant" até existir uma dessas, e o
front exige `area` obrigatória para o perfil Técnico. É o próximo bloqueio real do
cadastro de usuários.

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
  - `loginIntegracao_test.go`, `lojaIntegracao_test.go`, `setorIntegracao_test.go` —
    integração de verdade contra Postgres. `bancoDeTeste` (em `loginIntegracao_test.go`,
    compartilhado) cria um banco descartável (`teste_<nome do teste>_<pid>`), aplica as
    migrations nele e dropa no fim; o seed é criado pelos próprios services, então o
    caminho de escrita entra junto. **Sem Postgres alcançável ele dá `t.Skip`**, não falha.
    O que só aparece aqui: transação voltando inteira quando a validação recusa, gatilhos
    `DEFERRABLE` que só disparam no commit (gestor virando administrador precisa perder o
    escopo junto), unicidade por tenant/loja, e `ErrNoRows` virando `ErrNaoEncontrado` em
    vez de 500.
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

### Antes de entregar pro admin
- **Backup com restore ensaiado.** `pg_dump` agendado mandando pro R2 (Hobby não tem
  PITR — ver "Stack"). Faça o caminho de volta pelo menos uma vez: dump → restaura em
  banco novo → confere. Backup nunca testado não é backup.
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
(`POST /maquinas`, as criações de solicitação), o CRUD de `maquina`/`solicitacao_anexo`
em si (nada em `database/queries/` ainda), e o service resolvendo `foto_chave`/`chave`
em `fotoUrl`/`url` assinada na resposta. Validação de content-type/extensão também não
existe — `UploadFoto` aceita qualquer arquivo enviado no campo `foto`.

## Regras herdadas do contrato com o front (não reinvente)
- **401 é só "sem sessão"**; fora de escopo/perfil errado é sempre **403** — 401 fora de
  `/login` desloga o usuário no front. O RBAC por perfil é `middleware.Permitir(...)`;
  o escopo (loja/setor) **não** é middleware, é o `WHERE` da query.
- **Datas** trafegam como texto `dd/mm/yyyy HH:MM:SS` (ou `dd/mm/yyyy` sem hora) — use
  `config.DataBr` em todo campo de data de resposta, nunca `time.Time` cru.
- **Multi-tenant por subdomínio**: header `X-tenant-ID`, tenant nunca vem por rota nem
  por corpo.
- **Empresa É o tenant** (decisão fechada). `loja.tenant_id` referencia `empresa (id)`
  direto, não existe coluna `empresa_id` nem tabela intermediária, e o front tipa a
  "Hierarquia Tenant > Empresa > Loja > Setor" como se houvesse. Consequências:
  `GET /empresas` devolve **uma lista de um item só** (a empresa do tenant autenticado,
  `id` = `tenant_id`), e `Loja.empresaId` é o `tenant_id` da própria linha — campo
  derivado, não coluna. O front precisa dele porque filtra "lojas já cadastradas dessa
  empresa" comparando com o valor do select. **`empresaId` no corpo de `POST/PUT /lojas`
  é ignorado**: como empresa = tenant, aceitá-lo do cliente seria aceitar o tenant do
  corpo — o mesmo buraco do `X-tenant-ID` em rota autenticada, por outra porta.
- **Escopo (loja/setor/técnico) é sempre filtrado no `WHERE` do servidor**, nunca
  devolvido inteiro pro cliente filtrar.
- **Transições de estado são `POST` em sub-recurso** (`/iniciar`, `/pausar`,
  `/acionar-terceiro`, etc.), nunca `PATCH` de um campo `status` genérico.
