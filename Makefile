.PHONY: init up down logs test integration integration-local check fmt

export GOCACHE := $(CURDIR)/.cache/go-build
export GOMODCACHE := $(CURDIR)/.cache/go-mod

init:
	@sh scripts/init-env.sh

up: init
	docker compose up --build -d --wait

down:
	docker compose down

logs:
	docker compose logs -f --tail=100

test:
	go test -race ./...
	node --test web/*.test.mjs

integration:
	@sh scripts/test-docker.sh

integration-local:
	sh scripts/test-integration.sh

check:
	go vet ./...
	node --check web/app.js
	docker compose config --quiet

fmt:
	gofmt -w cmd internal tests
