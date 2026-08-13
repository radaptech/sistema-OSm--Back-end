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
- **Hash de senha: argon2id** (`alexedwards/argon2id`, `auth/passHash.go` —
  `HashPassword`/`HashCompare`, `argon2id.DefaultParams`). `bcrypt` foi removido —
  `provisionamento.go` também passou a usar `auth.HashPassword`; não reintroduza bcrypt
  em nenhum caminho de senha nova.
- Infra alvo: Railway (app) + Supabase (Postgres + Storage). Local: Docker Compose
  (`postgres_container-sistema-OS`, `api_sistema-OS` com hot-reload via CompileDaemon,
  `pgadmin_container-sistema-OS`).

## Documentos de referência (leia antes de implementar endpoint novo)
- `docs/modelagem-banco-dados.md` — modelo de dados completo, revisão 4, com todo o
  raciocínio de cada constraint/trigger/view.
- `docs/der-banco-dados.mmd` (+ `.svg`/`.png` gerados) — diagrama ER.
- Artefatos publicados (fonte de verdade do contrato HTTP — pedir link se precisar):
  DER revisado, Contrato de API v1.1 (53 endpoints), RBAC, PRD, Roteiro do Back-end
  (16 fases). Ver `../front-end/CLAUDE.md` para a lógica de negócio do ponto de vista
  do front (é o documento mais detalhado de regras de negócio do projeto).

## Regra de ouro: banco e API não divergem da modelagem
Toda constraint, ENUM, trigger e view documentados em `modelagem-banco-dados.md` existem
por um motivo explicado ali — não simplifique/pule sem entender o porquê (normalmente é
uma regra de negócio que já mordeu alguém, ex: "terceirizar é decisão do Técnico, não do
Solicitante", "horas_parada só existe se afeta_producao", "tipo da OS só promove pra
terceiros, nunca volta"). Peça o artefato do Contrato de API antes de implementar um
endpoint pra bater o JSON de envio/resposta campo a campo — os nomes já são exatamente o
que o front espera (camelCase, datas `dd/mm/yyyy HH:MM:SS`, ver `config/dataBr.go`).

## Estrutura atual
- `main.go` — entrypoint. Roda migrations automaticamente no boot
  (`RunMigrationPostgress`) e sobe o Gin na porta 8081. Também despacha subcomandos de
  CLI antes de subir o servidor (ver abaixo). **Ainda não registra nenhuma rota** —
  `router/`, `controller/` seguem vazios; `middleware/` e `auth/` já têm conteúdo mas
  nada os importa em `main.go` ainda.
- `config/` — `VariaveisDeAmbiente` (lê `.env`), `ConnPostgresql` (pool + migrations),
  `DataBr` (tipo de data custom, layout `02/01/2006 15:04:05`, nunca RFC3339).
- `auth/` — `jwt.go` (`GerarJwt`, claims `sub`/`tenantId`/`perfil`/`exp`/`iat`,
  HS256), `passHash.go` (`HashPassword`/`HashCompare`, argon2id).
- `middleware/` — `middJwt.go` (`AutenticacaoJwt`, lê cookie `token` ou
  `Authorization: Bearer`, valida e injeta `userId`/`user_perfil`/`user_TenantId` no
  contexto do Gin — falha fechada: claim ausente/malformada aborta com 401), `cors.go`
  (`CorsConfig`, libera `localhost`/`*.localhost` e `radaptech.com.br`/subdomínios,
  `AllowCredentials: true` pro cookie ir junto).
- `internal/model/` — structs de request/response, com `json` (camelCase, espelhando os
  tipos do front) e `binding` (validação do `go-playground/validator` via Gin) nas tags.
  `login.go` (`Login`, `SessaoUsuario`, `EscopoAcessoGestor`), `usuarios.go`
  (`NovoUsuarioPayload`, `AtualizarUsuarioPayload`, `Usuario`).
- `internal/helper/Errors.go` — `TraduzErroPostgres`: converte código de erro do
  Postgres (`23505`, `23503`, `23502`, ...) em erro de negócio (`ErrDadoDuplicado`,
  `ErrConflitoIntegridade`, ...) pros controllers não fazerem `switch` em `pgErr.Code`
  espalhado pelo código.
- `internal/service/` — regras de negócio (ex: `provisionamento.go`).
- `internal/router/`, `controller/` — criados mas ainda vazios.
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

Implementado em `internal/service/provisionamento.go` (`ProvisionarAdministrador`) +
`cli_provisionar_admin.go`. Idempotente por tenant: rodar de novo com o mesmo
`--subdominio` não recria a empresa, só adiciona outro administrador a ela.

## Autenticação (JWT)
- `auth.GerarJwt(id, tenantId int64, perfil string)` assina um HS256 com `JWT_SECRET`
  (`.env` — gere com `openssl rand -base64 64`, mínimo 256 bits, nunca commitado) e
  claims mínimas: `sub` (usuario.id), `tenantId` (empresa.id), `perfil`, `exp` (24h),
  `iat`. **De propósito sem** `nome`/`escoposGestor`/`lojaId`/`setorId`/`tecnicoId`/
  `ativo` — isso é resolvido fresco no banco a cada request (`GET /autenticacao/sessao`
  e o próprio middleware de RBAC, ainda não escritos), senão editar o escopo de um
  Gestor ou desativar um usuário só valeria depois do token expirar.
- **Depois do login, `tenant_id` autoritativo é o do token, não o header
  `X-tenant-ID`** — o header só importa em `POST /autenticacao/login`, antes de existir
  token, pra saber contra qual `empresa` autenticar. Trocar o header depois de logado
  não pode mudar de tenant.
- `middleware.AutenticacaoJwt()` lê o token do cookie `token` (produção) ou do header
  `Authorization: Bearer <token>` (Postman/Insomnia, sem cookie) e injeta no contexto:
  `userId`, `user_perfil`, `user_TenantId` (todos `int64`/`string`). Aborta com 401 se
  qualquer claim obrigatória faltar — não deixa o handler seguir com contexto
  incompleto.
- **Ainda faltam:** o handler de login (que assina o JWT e faz `ctx.SetCookie` com
  `HttpOnly` + `Secure` + `SameSite`, nome do cookie precisa bater com o que o
  middleware lê — `"token"`), o RBAC por perfil (401 vs 403 — ver regra abaixo) e uma
  forma de revogar sessão antes do `exp` (desativar usuário/trocar senha/logout hoje não
  matam um token já emitido).

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
- `ObterEscopoSessaoPorUsuario` já devolve no formato que o front consome
  (`EscopoAcessoGestor[]`, um `array_agg` de `setor_id` por loja) — é a query certa pra
  montar `SessaoUsuario.escoposGestor` no login/sessão.

## Ambiente local
- `.env` na raiz: `DB_SERVER`, `DB_USER`, `DB_PORT`, `DATABASE`, `DB_PASSWORD`
  (`DB_SSLMODE` opcional, default `disable`).
- Dentro da rede Docker do projeto, o Postgres resolve por `DB_SERVER=postgres`,
  `DB_PORT=5432` — é o que `api_sistema-OS` usa. **A porta publicada no host (5431) está
  mal mapeada** (o Postgres dentro do container escuta em 5432, não 5431) — isso é do
  `docker-compose`, não conserte sem avisar; para testar comandos Go pontualmente,
  rode dentro do container já ativo: `docker exec api_sistema-OS go run . ...`.

## Regras herdadas do contrato com o front (não reinvente)
- **401 é só "sem sessão"**; fora de escopo/perfil errado é sempre **403** — 401 fora de
  `/login` desloga o usuário no front.
- **Datas** trafegam como texto `dd/mm/yyyy HH:MM:SS` (ou `dd/mm/yyyy` sem hora) — use
  `config.DataBr` em todo campo de data de resposta, nunca `time.Time` cru.
- **Multi-tenant por subdomínio**: header `X-tenant-ID`, tenant nunca vem por rota nem
  por corpo.
- **Escopo (loja/setor/técnico) é sempre filtrado no `WHERE` do servidor**, nunca
  devolvido inteiro pro cliente filtrar.
- **Transições de estado são `POST` em sub-recurso** (`/iniciar`, `/pausar`,
  `/acionar-terceiro`, etc.), nunca `PATCH` de um campo `status` genérico.
