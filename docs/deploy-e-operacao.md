# Deploy, produção e os jobs de CLI

Leia antes de mexer em Dockerfile, criar Cron Job no Railway, subir para produção
ou escrever subcomando de CLI novo.

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

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
do front continuam prontas, esperando este back. `GET /ordens-servico` já existe (ver a
seção dela), mas as duas telas também escrevem — `AdministradorCustosPendentes` chama
`POST /:id/custo` —, então o `git revert` do commit que os removeu só vale depois do ciclo
de vida da fase 2. Não recriar os cards na mão.
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
