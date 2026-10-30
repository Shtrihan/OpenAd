.PHONY: up down logs test lint-api fmt

COMPOSE := docker compose -f deploy/compose.yaml --env-file deploy/.env

up:          ## Start the whole stack
	$(COMPOSE) up -d --build

down:        ## Stop the stack (data volumes are kept)
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

test:        ## Go unit tests
	cd server && go test -race ./...

fmt:
	cd server && gofmt -w .

lint-api:    ## Validate api/openapi.yaml
	npx -y @redocly/cli@1.25.11 lint
