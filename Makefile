SHELL := /bin/bash
GO ?= go

# Todo diretório com go.mod é um módulo (raiz + SDKs normativos).
MODULES := $(shell find . -name go.mod -not -path './.git/*' -exec dirname {} \; | sort)

GOLANGCI_VERSION ?= v2.14.0
GOVULNCHECK_VERSION ?= v1.9.0
GOLANGCI := $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
GOVULNCHECK := $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

.DEFAULT_GOAL := help
.PHONY: help tidy fmt lint test test-race test-integration cover bench vuln generate generate-check check kb linux-binaries up up-obs down ps logs chaos load

help: ## lista os alvos
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

tidy: ## go mod tidy em todos os módulos
	@for m in $(MODULES); do (cd $$m && $(GO) mod tidy) || exit 1; done

fmt: ## gofumpt + goimports
	@for m in $(MODULES); do (cd $$m && $(GOLANGCI) fmt ./...) || exit 1; done

lint: ## golangci-lint em todos os módulos
	@for m in $(MODULES); do echo "› lint $$m"; (cd $$m && $(GOLANGCI) run ./...) || exit 1; done

test: ## testes unitários
	@for m in $(MODULES); do (cd $$m && $(GO) test ./...) || exit 1; done

test-race: ## testes com race detector, ordem aleatória e sem cache
	@for m in $(MODULES); do (cd $$m && $(GO) test -race -shuffle=on -count=1 ./...) || exit 1; done

test-integration: ## testes com containers reais (precisa de docker; colima funciona)
	DOCKER_HOST=$${DOCKER_HOST:-$$(docker context inspect --format '{{.Endpoints.docker.Host}}')} \
	TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=$${TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE:-/var/run/docker.sock} \
	$(GO) test -tags=integration -count=1 -timeout=20m ./internal/integration/...

cover: ## cobertura do módulo raiz
	$(GO) test -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out | tail -1

bench: ## benchmarks com alocações
	@for m in $(MODULES); do (cd $$m && $(GO) test -run=^$$ -bench=. -benchmem ./...) || exit 1; done

vuln: ## govulncheck em todos os módulos
	@for m in $(MODULES); do echo "› vuln $$m"; (cd $$m && $(GOVULNCHECK) ./...) || exit 1; done

generate: ## go generate em todos os módulos
	@for m in $(MODULES); do (cd $$m && $(GO) generate ./...) || exit 1; done

generate-check: generate ## falha se o código gerado não estiver em dia
	@git diff --exit-code --stat -- . || (echo "código gerado desatualizado: rode make generate" && exit 1)

check: lint test-race generate-check ## o mesmo que a CI roda

kb: ## confere a base de conhecimento contra o código
	$(GO) run ./cmd/intenza-kb check

COMPOSE := docker compose -f deploy/compose.yaml
LINUX_ARCH ?= $(shell $(GO) env GOARCH)

linux-binaries: ## compila os binários linux no host para a imagem local
	GOOS=linux GOARCH=$(LINUX_ARCH) CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o bin/linux/ ./cmd/...

up: linux-binaries ## sobe a pilha (mongo, redis, kafka, toxiproxy, serviços e prometheus)
	$(COMPOSE) up -d --build --wait

up-obs: linux-binaries ## sobe também grafana (localhost:3000) e jaeger (localhost:16686)
	$(COMPOSE) --profile obs up -d --build --wait

down: ## derruba a pilha e apaga os volumes
	$(COMPOSE) --profile obs down -v

ps: ## estado dos containers
	$(COMPOSE) ps

logs: ## logs dos serviços do intenza
	$(COMPOSE) logs -f api relay worker psp

chaos: ## roda todos os experimentos de caos contra a pilha (make up antes)
	$(GO) run ./cmd/intenza-chaos run -out chaos/results chaos/experiments/*.yaml

load: ## carga estocástica por 60 s contra a pilha
	$(GO) run ./cmd/intenza-chaos load -rate 1 -duration 60s -settlement
