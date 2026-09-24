# Ambiente local (Docker Compose)

Leia quando algo não subir, a porta não conectar ou o teste de integração pular sozinho.

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

## Ambiente local
- `.env` na raiz (modelo em `.env-example`): `DB_SERVER`, `DB_USER`, `DB_PORT`, `DATABASE`,
  `DB_PASSWORD` (`DB_SSLMODE` opcional, default `disable`) e `JWT_SECRET` — os únicos
  obrigatórios. Opcionais, e sem eles só o recurso correspondente falha: `R2_*` (upload e
  foto), `EVOLUTION_*` (WhatsApp), `RESEND_API_KEY` + `URL_FRONTEND_FORMATO` (e-mail de
  recuperação de senha; sem a chave o pedido responde 200 e o envio falha no log). `TRUSTED_PROXIES` **não**
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
- ⚠️ **O mapeamento é `5431:5432`, e o lado direito importa.** Dentro do container o
  Postgres escuta **só na 5432** (imagem `postgres:16-alpine` sem `-p`). O compose já disse
  `5431:5431`: a porta publicada não tinha ninguém do outro lado e `localhost:5431` dava
  `connection reset by peer`. Com `5431:5432` o default de `TEST_DB_DSN` conecta do host.
  (A correção está no `docker-compose.yml` do repo `sistema-os-infra` — confira se foi
  commitada antes de estranhar um clone novo.)
- Sintoma de o banco não estar alcançável: `go test ./...` **verde sem ter rodado a
  integração** (`t.Skip` silencioso) — exatamente o que o job de CI existe pra evitar.
  Alternativa sem depender da porta publicada: o IP do container,
  `TEST_DB_DSN='postgres://postgres:postgres@172.29.0.3:5432/postgres?sslmode=disable'`.
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
