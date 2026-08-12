DC        := docker compose -f docker-compose.dev.yml
DC_PROD   := docker compose -f docker-compose.prod.yml --env-file .env.prod
MIGRATE   := $(DC) run --rm migrate
DB_URL    := postgres://postgres:postgres@localhost:5432/mailpulse?sslmode=disable

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- dev
.PHONY: up
up: ## Start the development stack
	$(DC) up -d --build

.PHONY: down
down: ## Stop the development stack
	$(DC) down

.PHONY: clean
clean: ## Stop and delete all volumes (destroys local data)
	$(DC) down -v

.PHONY: logs
logs: ## Tail web and worker logs
	$(DC) logs -f web worker

.PHONY: ps
ps: ## Show service status
	$(DC) ps

.PHONY: shell
shell: ## Shell into the web container
	$(DC) exec web sh

.PHONY: psql
psql: ## Open psql against the dev database
	$(DC) exec postgres psql -U postgres -d mailpulse

.PHONY: redis-cli
redis-cli: ## Open redis-cli against the dev cache
	$(DC) exec redis redis-cli

# ---------------------------------------------------------------- migrations
.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	$(MIGRATE) up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration
	$(MIGRATE) down 1

.PHONY: migrate-version
migrate-version: ## Print the current schema version
	$(MIGRATE) version

.PHONY: migrate-create
migrate-create: ## Create a migration pair: make migrate-create name=add_something
	@test -n "$(name)" || (echo "usage: make migrate-create name=add_something" && exit 1)
	$(DC) run --rm --entrypoint migrate migrate \
		create -ext sql -dir /migrations -format 20060102150405 $(name)

# ---------------------------------------------------------------- go
.PHONY: test
test: ## Run the integration tests against the dev stack
	$(DC) exec web go test ./test/... -v

.PHONY: build
build: ## Compile both binaries locally
	go build -o bin/web ./cmd/web && go build -o bin/worker ./cmd/worker

.PHONY: fmt
fmt: ## Format and vet
	gofmt -w . && go vet ./...

.PHONY: tidy
tidy: ## Tidy modules
	go mod tidy

# ---------------------------------------------------------------- prod
.PHONY: prod-up
prod-up: ## Start the production stack
	$(DC_PROD) up -d --build

.PHONY: prod-down
prod-down: ## Stop the production stack
	$(DC_PROD) down

.PHONY: prod-logs
prod-logs: ## Tail production web and worker logs
	$(DC_PROD) logs -f web worker
