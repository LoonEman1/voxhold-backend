MIGRATIONS_DIR := ./migrations
DATABASE_URL := sqlite://data/voxhold.db

.PHONY: run migrate-up migrate-down migrate-version migrate-create

run:
	go run ./cmd/api

migrate-up:
	migrate \
		-path $(MIGRATIONS_DIR) \
		-database "$(DATABASE_URL)" \
		up

migrate-down:
	migrate \
		-path $(MIGRATIONS_DIR) \
		-database "$(DATABASE_URL)" \
		down 1

migrate-version:
	migrate \
		-path $(MIGRATIONS_DIR) \
		-database "$(DATABASE_URL)" \
		version

migrate-create:
	migrate create \
		-ext sql \
		-dir $(MIGRATIONS_DIR) \
		-seq $(name)