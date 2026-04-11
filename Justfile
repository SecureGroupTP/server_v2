set shell := ["bash", "-eu", "-o", "pipefail", "-c"]
set dotenv-load := true

server_bin := "build/server"

[doc("Show available tasks with documentation.")]
default:
    @just --list --unsorted

[doc("Run local preflight checks for required toolchain binaries.")]
preflight:
    command -v go >/dev/null
    command -v docker >/dev/null
    command -v openssl >/dev/null
    command -v golangci-lint >/dev/null

[doc("Run preflight checks for Docker Compose based tasks.")]
docker-preflight:
    command -v docker >/dev/null
    docker compose version >/dev/null

[doc("Create config/config.yaml from config/example.config.yaml if it does not exist.")]
init-config:
    cp -n config/example.config.yaml config/config.yaml

[doc("Create a local self-signed TLS certificate for TCP/TLS, HTTPS, and WSS.")]
cert-selfsigned:
    mkdir -p config/certs
    openssl req -x509 -newkey rsa:4096 -sha256 -days 365 -nodes \
      -keyout config/certs/server.key \
      -out config/certs/server.crt \
      -subj "/CN=localhost" \
      -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"

[doc("Remove local TLS certificate files.")]
cert-clean:
    rm -f config/certs/server.crt config/certs/server.key

[doc("Build the server binary into build/server.")]
build:
    mkdir -p build
    go build -trimpath -o {{server_bin}} ./cmd/server

[doc("Run the server locally using APP_CONFIG_PATH or config/config.yaml.")]
run:
    go run ./cmd/server

[doc("Start Postgres, Redis, Flyway migrations, and the app through Docker Compose.")]
up: docker-preflight init-config
    docker compose up --build

[doc("Start only infrastructure services and migrations through Docker Compose.")]
infra-up: docker-preflight
    docker compose up -d postgres redis flyway

[doc("Stop Docker Compose services and remove local volumes.")]
down: docker-preflight
    docker compose down -v

[doc("Show Docker Compose logs.")]
logs: docker-preflight
    docker compose logs -f --tail=200

[doc("Run Flyway migrations against the Docker Compose Postgres service.")]
migrate: docker-preflight
    docker compose up flyway

[doc("Apply Go formatting changes.")]
fmt:
    gofmt -w $(find . -name '*.go' -not -path './vendor/*')

[doc("Verify Go formatting without changing files (CI-safe).")]
fmt-check:
    test -z "$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))"

[doc("Run static checks and repository-specific lint rules.")]
lint:
    go run ./cmd/lintlimits
    golangci-lint run

[doc("Compile all packages and tests without running test bodies.")]
check:
    go test -run '^$' ./...

[doc("Run all tests. Integration tests start disposable PostgreSQL containers through testcontainers.")]
test:
    go test -p 1 ./...

[doc("Run tests with verbose integration output.")]
test-integration:
    go test -p 1 ./... -v

[doc("Run all tests with the Go race detector.")]
test-race:
    go test -p 1 -race ./...

[doc("Download modules and prune go.mod/go.sum.")]
tidy:
    go mod tidy

[doc("Run the standard local quality gate.")]
validate: fmt-check lint check test

[doc("Run the CI-style pipeline.")]
ci: validate

[doc("Remove local build artifacts.")]
clean:
    rm -rf build
