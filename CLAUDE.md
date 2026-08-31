# Back-end (Solicitação OS) — índice

API Go/Gin, multi-tenant por subdomínio, consumida pelo front em `../sistema-OSm--Front-end` (React).

## Comandos

| | |
|---|---|
| build / lint | `go build ./...` · `gofmt -l .` · `go vet ./...` |
| testes | `TEST_DB_DSN='postgres://postgres:postgres@<ip-do-container>:5432/postgres?sslmode=disable' go test -race ./...` |
| dev | `docker compose up -d` (um nível acima) → `http://<tenant>.localhost:8090` |
| sqlc / migration | `sqlc generate` (nunca edite `database/repository/` na mão) · `make migration nome` |
| jobs de CLI | `make provisionar-admin ARGS="..."` · `make backup-banco` · `make preventivas-vencidas` |

## Diretrizes rápidas

- **Tenant vem do token** (`GetTenantIDToken`), nunca do header `X-tenant-ID` — só o login usa o header.
- **Escopo de loja/setor é `WHERE`**, nunca RBAC nem filtro do cliente. Filtro do cliente estreita, nunca amplia.
- **401 é só "sem sessão"**; perfil/escopo errado é **403** — 401 fora de `/login` desloga o usuário.
- **Resposta**: tags camelCase, sempre com `id`; data é `*config.DataBr` (`dd/mm/yyyy HH:MM:SS`) — valor não-ponteiro serializa `{}` calado; listagem devolve slice não-nil, `null` quebra o `.map`.
- **Erros**: `errors.Is(err, pgx.ErrNoRows)` **antes** de `TraduzErroPostgres` (senão id inexistente vira 500); sentinela na frente do `%w` (`fmt.Errorf("%w: detalhe", helper.ErrX)`).
- **Exclusão é sempre soft delete**, sem reativação pela API; coluna `ativa` (loja/máquina/preventiva) ou `ativo` (usuário/setor); query de desativar é `:execrows` e `linhas == 0` → `ErrNaoEncontrado`.
- **Transição de estado é `POST` em sub-recurso** (`/iniciar`, `/rejeitar`), nunca `PATCH` de `status`.
- **Não simplifique constraint/trigger/view** sem ler o porquê em `docs/modelagem-banco-dados.md`.

## Documentação — nada aqui é carregado automático: abra só o que a tarefa exigir

| arquivo | leia quando |
|---|---|
| [docs/arquitetura.md](docs/arquitetura.md) | criar arquivo novo, mexer em camada desconhecida, decidir onde uma regra mora |
| [docs/banco-de-dados.md](docs/banco-de-dados.md) | escrever migration, query ou rodar `sqlc generate` |
| [docs/api-e-rotas.md](docs/api-e-rotas.md) | registrar rota, mexer em middleware/RBAC, montar corpo de resposta |
| [docs/fluxo-de-negocio.md](docs/fluxo-de-negocio.md) | endpoint do miolo do fluxo; **começar tarefa nova** (diz o que falta) |
| [docs/testes-e-ci.md](docs/testes-e-ci.md) | escrever teste, ou quando `go test` passar verde rápido demais |
| [docs/deploy-e-operacao.md](docs/deploy-e-operacao.md) | Dockerfile, Cron do Railway, subir para produção, subcomando de CLI |
| [docs/ambiente-local.md](docs/ambiente-local.md) | algo não sobe, porta não conecta, teste de integração pula sozinho |
| [docs/modelagem-banco-dados.md](docs/modelagem-banco-dados.md) | **fonte da verdade** do modelo de dados: o porquê de cada constraint |
| `../sistema-OSm--Front-end/CLAUDE.md` | regra de negócio pelo lado do front (o doc mais detalhado do projeto) |
| Contrato de API v1.2, RBAC, PRD | artefatos publicados — **peça o link** antes de implementar endpoint novo, para bater o JSON campo a campo |
