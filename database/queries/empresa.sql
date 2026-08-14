-- name: ObterEmpresaPorSubdominio :one
SELECT id, nome
FROM empresa
WHERE subdominio = $1
  AND ativa;
