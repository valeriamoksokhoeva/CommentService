.PHONY: up down logs run test test-postgres fmt vet

up:
	docker compose up --build -d

down:
	docker compose down

run:
	STORAGE=memory go run ./cmd

logs:
	docker compose logs -f


test: 
	docker compose up -d --wait postgres
	@set -a; . ./.env; set +a; \
	if ! docker compose exec -T postgres psql -U "$$POSTGRES_USER" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'comments_test'" | grep -q 1; then \
		docker compose exec -T postgres psql -U "$$POSTGRES_USER" -d postgres -c "CREATE DATABASE comments_test"; \
	fi
	@set -a; . ./.env; set +a; \
	if ! docker compose exec -T postgres psql -U "$$POSTGRES_USER" -d comments_test -tAc "SELECT to_regclass('public.posts')" | grep -q posts; then \
		docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d comments_test < migrations/001_init.sql; \
	fi
	@set -a; . ./.env; set +a; go test ./... -count=1

test-postgres: 
	docker compose up -d --wait postgres
	@set -a; . ./.env; set +a; \
	if ! docker compose exec -T postgres psql -U "$$POSTGRES_USER" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'comments_test'" | grep -q 1; then \
		docker compose exec -T postgres psql -U "$$POSTGRES_USER" -d postgres -c "CREATE DATABASE comments_test"; \
	fi
	@set -a; . ./.env; set +a; \
	if ! docker compose exec -T postgres psql -U "$$POSTGRES_USER" -d comments_test -tAc "SELECT to_regclass('public.posts')" | grep -q posts; then \
		docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d comments_test < migrations/001_init.sql; \
	fi
	@set -a; . ./.env; set +a; go test ./internal/store -run '^TestPostgresStore_' -count=1 -v

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...
