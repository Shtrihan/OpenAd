.PHONY: up down logs seed smoke loadtest test lint-api fmt

COMPOSE := docker compose -f deploy/compose.yaml --env-file deploy/.env

up:          ## Start the whole stack
	$(COMPOSE) up -d --build

down:        ## Stop the stack (data volumes are kept)
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

seed:        ## Load demo data (deploy/seed/dev.sql)
	$(COMPOSE) exec -T postgres psql -U openad -d openad -v ON_ERROR_STOP=1 < deploy/seed/dev.sql

smoke:       ## End-to-end test against the running, seeded stack
	cd deploy && set -a && . ./.env && set +a && test/smoke.sh

loadtest:    ## Load test POST /v1/ad against the running, seeded stack
	cd server && go run ./cmd/loadtest -c 32 -d 20s

test:        ## Go unit tests
	cd server && go test -race ./...

fmt:
	cd server && gofmt -w .

lint-api:    ## Validate api/openapi.yaml
	npx -y @redocly/cli@1.25.11 lint
