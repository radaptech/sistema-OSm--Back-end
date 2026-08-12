# Contexto do Projeto - Back-end (Solicitação OS)

API do sistema de manutenção/OS para redes de varejo, consumida pelo front-end em
`../front-end` (React). Este back-end está em construção — ver `docs/` para o roteiro
completo e os contratos já fechados com o front.

## Stack
- **Go 1.26**, módulo `github.com/radaptech/sistema-OSm--Back-end`.
- **Gin** (HTTP), **pgx/v5** (Postgres, via `pgxpool`), **golang-migrate** (migrations).
- **bcrypt** (senha), JWT planejado para sessão (cookie HttpOnly — ver front-end CLAUDE.md).
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
  CLI antes de subir o servidor (ver abaixo).
- `config/` — `VariaveisDeAmbiente` (lê `.env`), `ConnPostgresql` (pool + migrations),
  `DataBr` (tipo de data custom, layout `02/01/2006 15:04:05`, nunca RFC3339).
- `internal/model/` — structs de request/response.
- `internal/service/` — regras de negócio (ex: `provisionamento.go`).
- `internal/router/`, `internal/helper/`, `auth/`, `controller/`, `middleware/` —
  criados mas ainda vazios; é aqui que entram rotas, RBAC, middlewares de tenant/sessão.
- `database/migrate/` — migrations SQL puro (`golang-migrate`), numeradas e sequenciais.
  `database/queries/`, `database/repository/` — vazios ainda.

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
