MIGRATIONS_DIR := ./migrations
DATABASE_URL := sqlite://data/voxhold.db
PGO ?= auto

.PHONY: run build build-linux test migrate-up migrate-down migrate-version migrate-create

run:
	go run ./cmd/api

build:
	go build -trimpath -ldflags="-s -w" -pgo="$(PGO)" -o bin/voxhold ./cmd/api
	go build -trimpath -ldflags="-s -w" -o bin/voxhold-bootstrap ./cmd/bootstrap

build-linux:
	PGO="$(PGO)" sh scripts/build-linux.sh

test:
	go test ./...

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
