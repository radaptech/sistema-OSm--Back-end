# API: autenticação, rotas, RBAC e o contrato com o front

Leia antes de registrar rota nova, mexer em middleware ou montar corpo de resposta.

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

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
  | `POST /solicitacoes/maquinario`, `/reparo` | **solicitante** |
  | `GET /solicitacoes/minhas`, `/resumo` | **qualquer perfil autenticado** (o service filtra pelo próprio ator) |
  | `GET /solicitacoes` (fila) | gestor, administrador |
  | `GET /solicitacoes/:id` | **qualquer perfil autenticado** (escopo no `WHERE`) |
  | `POST /solicitacoes/:id/abrir-os`, `/:id/rejeitar` | gestor, administrador |
  | `GET /ordens-servico` | gestor, administrador, **técnico** (escopo no `WHERE`) |

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
- **As duas criações de `/solicitacoes` são só do solicitante** — é a única tela que
  chama (`NovaSolicitacao` no front). `/minhas` e `/resumo` ficam sem `Permitir` de
  propósito, mesmo critério de `/lojas`/`/setores`: o service já filtra pelo
  `usuario.id` de quem chama (nunca recebe `perfil`), então RBAC ali não filtraria nada
  a mais, só barraria administrador/gestor de testar a própria rota. `GET /solicitacoes`
  (a fila) é só gestor/administrador — Técnico não participa da aprovação, só recebe a
  OS depois que ela existe. `GET /solicitacoes/:id` é aberta a qualquer perfil, recortada
  pelo escopo no `WHERE` (mesmo `EXISTS` de `ListarSolicitacoes`) — sem isso um
  Solicitante enumerando id leria foto/descrição de outro setor.
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
