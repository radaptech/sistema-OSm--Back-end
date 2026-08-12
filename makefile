migration:
	@migrate create -ext  sql -dir database/migrate -seq $(filter-out $@, $(MAKECMDGOALS))

# Cria o primeiro administrador de um tenant (e o proprio tenant, se o
# subdominio ainda nao existir). Roda fora da API HTTP -- POST /usuarios
# exige um administrador ja autenticado, que nao existe ate este comando
# rodar. Uso:
#   make provisionar-admin ARGS="-subdominio=cooprata -empresa='Cooprata' -nome='Davi' -email=admin@cooprata.com -senha=SENHA_FORTE"
provisionar-admin:
	@go run . provisionar-admin $(ARGS) 