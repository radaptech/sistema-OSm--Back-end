-- name: ObterEmpresaPorSubdominio :one
SELECT id, nome
FROM empresa
WHERE subdominio = $1
  AND ativa;

-- name: ObterEmpresaPorID :one
-- GET /empresas: o tenant É a empresa (loja.tenant_id referencia empresa
-- direto), então a listagem que alimenta o select de Empresa no cadastro de
-- loja tem exatamente uma linha -- a do próprio tenant autenticado.
SELECT id, nome
FROM empresa
WHERE id = $1
  AND ativa;

-- name: EmpresaAtiva :one
-- Usado por ObterSessao: desativar uma empresa precisa derrubar as sessões
-- abertas dela, senão um token emitido antes continua valendo até expirar (8h).
-- Query própria em vez de JOIN em ObterUsuarioPorID porque aquela devolve
-- repository.Usuario, que montarSessao consome inteiro -- virar Row ali
-- espalharia a mudança por todo o caminho da sessão.
SELECT ativa FROM empresa
WHERE id = $1;
