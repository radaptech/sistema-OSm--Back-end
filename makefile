migration:
	@migrate create -ext  sql -dir database/migrate -seq $(filter-out $@, $(MAKECMDGOALS)) 