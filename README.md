# Solicitação OS — Back-end

API do sistema de **solicitações e ordens de serviço de manutenção** (máquinas e predial).
Um Solicitante abre a solicitação, o Gestor converte em Ordem de Serviço (ou rejeita), o
Técnico executa e encerra lançando o custo, e o Administrador confere o custo e cuida dos
cadastros (lojas, setores, usuários, máquinas, preventivas, terceirizadas). Preventivas
vencidas abrem solicitação sozinhas, e os indicadores de cada máquina saem do histórico
de OS concluídas.

É **multi-tenant por subdomínio** (`<empresa>.radaptech.com.br`) e é consumida pelo
front em React ([`sistema-OSm--Front-end`](../sistema-OSm--Front-end)).

## Stack

Go + Gin · Postgres (pgx, sqlc, golang-migrate) · JWT em cookie HttpOnly · senha em
argon2id · Cloudflare R2 para fotos/vídeos e backup · Resend (e-mail) e Evolution API
(WhatsApp), ambos opcionais. Produção: Railway + Supabase.

## Rodando local

O ambiente sobe pelo `docker-compose.yml` **um nível acima** deste repositório (front,
API com hot-reload, Postgres, pgAdmin e Traefik):

```sh
cp .env-example .env          # troque o JWT_SECRET: openssl rand -base64 64
cd .. && docker compose up -d
```

- Aplicação: `http://<tenant>.localhost:8090` (front, e `/api` cai aqui)
- Postgres no host: `localhost:5431` · pgAdmin: `:5051` · dashboard do Traefik: `:8091`

As migrations rodam quando a API sobe. Para ter com quem logar, crie o tenant e o
primeiro administrador:

```sh
make provisionar-admin ARGS="-subdominio=demo -empresa='Demo' -nome='Admin' -email=admin@demo.com -senha=SENHA_FORTE"
```

Depois, `http://demo.localhost:8090`.

## Comandos

| | |
|---|---|
| build / lint | `go build ./...` · `gofmt -l .` · `go vet ./...` |
| testes | `go test -race ./...` com o compose de pé (ou `TEST_DB_DSN=...`) |
| gerar código do banco | `sqlc generate` — nunca edite `database/repository/` à mão |
| nova migration | `make migration nome_da_migration` |
| jobs (Railway Cron em produção) | `make backup-banco` · `make preventivas-vencidas` |

> Se `go test` passar verde rápido demais, os testes de integração provavelmente
> pularam por não alcançar o Postgres — ver [docs/testes-e-ci.md](docs/testes-e-ci.md).

## Estrutura

```
main.go, cli_*.go      entrada da API e dos subcomandos de CLI
internal/router        rotas e RBAC por perfil
controller/            handlers HTTP
internal/service       regras de negócio
internal/model         payloads e respostas
database/migrate       migrations SQL
database/queries       SQL de origem do sqlc → database/repository (gerado)
middleware/, auth/     JWT, tenant, rate limit, hash de senha
config/, bucketR2/     conexão com o banco, formatos, upload no R2
```

## Documentação

| | |
|---|---|
| [arquitetura](docs/arquitetura.md) | stack, papel de cada pacote, convenções |
| [api-e-rotas](docs/api-e-rotas.md) | autenticação, rotas, RBAC, formato das respostas |
| [fluxo-de-negocio](docs/fluxo-de-negocio.md) | o que está pronto, o que falta e o porquê das regras |
| [banco-de-dados](docs/banco-de-dados.md) | migrations, queries e sqlc |
| [modelagem-banco-dados](docs/modelagem-banco-dados.md) | modelo de dados e o motivo de cada constraint ([DER](docs/der-banco-dados.svg)) |
| [testes-e-ci](docs/testes-e-ci.md) | como os testes são montados e o CI |
| [ambiente-local](docs/ambiente-local.md) | quando algo não sobe ou não conecta |
| [deploy-e-operacao](docs/deploy-e-operacao.md) | Railway, Cron, backup e subida para produção |
