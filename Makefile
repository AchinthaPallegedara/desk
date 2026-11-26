.PHONY: db-up db-down db-reset migrate migrate-status backend web tenant test lint

# Load backend env for Go targets
BACKEND_ENV = set -a && . ./backend/.env && set +a

db-up:            ## start local Postgres
	docker compose up -d --wait postgres

db-down:
	docker compose down

db-reset:         ## wipe local DB volume and re-create
	docker compose down -v && $(MAKE) db-up && $(MAKE) migrate

migrate:          ## run migrations up
	$(BACKEND_ENV) && cd backend && go run ./cmd/server migrate up

migrate-status:
	$(BACKEND_ENV) && cd backend && go run ./cmd/server migrate status

tenant:           ## create a business and its owner login
	$(BACKEND_ENV) && cd backend && go run ./cmd/server create-tenant

backend:          ## run Go API + MCP on :8080
	$(BACKEND_ENV) && cd backend && go run ./cmd/server

web:              ## run Next.js on :3000
	cd web && npm run dev

test:
	$(BACKEND_ENV) && cd backend && go test ./...

lint:
	cd backend && go vet ./... && cd ../web && npm run lint
