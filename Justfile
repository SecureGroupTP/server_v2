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
