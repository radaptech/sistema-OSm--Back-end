# Banco de dados: migrations, queries e sqlc

Leia antes de escrever migration, query nova ou rodar `sqlc generate`.
O modelo de dados em si (o porquê de cada constraint) está em
[modelagem-banco-dados.md](modelagem-banco-dados.md), que continua sendo a fonte da verdade.

> Parte do contexto do back-end. O índice é o [CLAUDE.md](../CLAUDE.md) na raiz.

---

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
  humana, `000006` seed de `area_tecnico`, `000007` urgência vira ENUM.
- ⚠️ **Tabela e tipo dividem namespace no Postgres** — trocar uma tabela por um ENUM
  homônimo exige dropar a tabela **antes** de criar o tipo (foi o caso de `000004`).

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
  `ordem_servico.urgencia` é o **ENUM `nivel_urgencia`** também, desde a migration
  `000007` — mesma lacuna e mesmo motivo de `nivel_criticidade`: tupla fixa no front
  (`niveisUrgencia`, `front-end/src/tipos/ordemServico.ts`), sem tela de cadastro, tabela
  vazia em todo tenant. Ficou tabela em `000004` só porque `ordem_servico` ainda não tinha
  nenhum caminho de escrita; `CriarOrdemServicoDeSolicitacao` (`solicitacao_os.sql`, fase 1
  de Solicitações) foi o primeiro, e sem a migration `POST /solicitacoes/:id/abrir-os`
  travaria em todo tenant do mesmo jeito que cadastro de máquina travava antes da `000004`.
  A migration precisou dropar e recriar `vw_os_finalizada`/`vw_os_custo_sem_lancamento`
  também — as duas fazem `SELECT os.*`, e o Postgres resolve isso em colunas concretas na
  criação da view, então as duas ficam dependentes de `urgencia_id` por baixo mesmo sem
  citar a coluna no texto; `DROP COLUMN` direto falha com "other objects depend on it".
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
