set dotenv-load := true

build:
  mkdir -p build
  go build -o build/server ./cmd/server

run:
  go run ./cmd/server

init-config:
  cp -n config/example.config.yaml config/config.yaml

up:
  docker compose up --build

down:
  docker compose down -v

migrate:
  docker compose up flyway

test:
  go test -p 1 ./...

test-integration:
  TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/app?sslmode=disable' go test -p 1 ./... -v

fmt:
  golangci-lint fmt
  gofmt -w $(find . -name '*.go' -not -path './vendor/*')

lint:
  go run ./cmd/lintlimits
  golangci-lint run
