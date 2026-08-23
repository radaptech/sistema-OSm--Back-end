-- Empresa terceirizada é a prestadora externa que o TÉCNICO aciona quando
-- decide não resolver a OS internamente (front-end/CLAUDE.md item 9:
-- "terceirizar é decisão do Técnico"). Entidade simples: não pende de loja nem
-- de setor -- é do tenant inteiro, e por isso nenhuma listagem daqui filtra por
-- escopo, diferente de maquina/preventiva.
--
-- Quem cadastra é o Administrador; quem consome é o Técnico, no
-- ModalAcionarTerceiro. Escrita só do administrador, leitura dos dois.
--
-- Exclusão é soft delete (ativa = false), como em loja/setor/maquina:
-- ordem_servico.empresa_terceirizada_id aponta pra cá (fk_os_empresa_terceirizada,
-- composta por tenant), então apagar de verdade levaria junto o histórico de
-- quem executou o quê -- e a nota fiscal lançada em cima disso.
--
-- ⚠️ A coluna é `ativa` (feminino), como em loja/maquina/preventiva -- e `ativo`
-- em usuario/setor.

-- name: CriarEmpresaTerceirizada :one
-- Nome é único por tenant (uq_empresa_terceirizada_nome): duplicado volta 23505
-- e vira ErrDadoDuplicado no helper, sem switch em pgErr.Code no service.
-- especialidade e telefone são opcionais (NULL) -- o front tipa os dois como `?`.
INSERT INTO empresa_terceirizada (tenant_id, nome, especialidade, telefone)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ObterEmpresaTerceirizadaPorID :one
-- Sem filtro de `ativa`, igual a ObterLojaPorID/ObterMaquinaPorID: a tela de
-- edição precisa ler o registro para preencher o formulário mesmo desativado.
-- Quem decide o que fazer com uma empresa inativa é o service.
SELECT * FROM empresa_terceirizada
WHERE id = $1 AND tenant_id = $2;

-- name: ListarEmpresasTerceirizadas :many
-- Só as ativas: a lista alimenta o select do ModalAcionarTerceiro, e oferecer
-- uma empresa desativada é oferecer o que o Administrador acabou de tirar do ar.
--
-- Sem paginação e sem filtro de propósito -- servicoEmpresasTerceirizadas.listar()
-- não manda parâmetro nenhum e a tela do Administrador pagina no cliente
-- (front-end/CLAUDE.md item 12).
SELECT * FROM empresa_terceirizada
WHERE tenant_id = $1
  AND ativa
ORDER BY nome;

-- name: AtualizarEmpresaTerceirizada :one
-- Todos os campos editáveis de uma vez (o front manda o objeto inteiro no PUT).
-- `ativa` fica de fora: reativar não existe pela API, e desativar tem rota
-- própria -- ver DesativarEmpresaTerceirizada.
UPDATE empresa_terceirizada
SET nome = $3,
    especialidade = $4,
    telefone = $5
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: DesativarEmpresaTerceirizada :execrows
-- :execrows e não :exec, mesmo motivo de DesativarLoja/DesativarMaquina: sem a
-- contagem de linhas, desativar um id que não existe (ou que é de outro tenant)
-- responderia sucesso igual a desativar um de verdade. linhas == 0 ->
-- ErrNaoEncontrado. Já desativada casa a linha e conta 1, então repetir é
-- idempotente -- e é assim que o front espera.
--
-- Sem cheque de dependente antes, diferente de DesativarLoja (que recusa com
-- setor ativo): empresa terceirizada não tem filho. Ela é REFERENCIADA por
-- ordem_servico, mas como o delete é soft a FK nunca reclama, e a OS antiga
-- continua mostrando quem executou o serviço.
UPDATE empresa_terceirizada
SET ativa = false
WHERE id = $1 AND tenant_id = $2;
