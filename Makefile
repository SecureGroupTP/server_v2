SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

.PHONY: default up docker-preflight init-config cert-selfsigned nginx-check down logs migrate

default: up

docker-preflight:
	command -v docker >/dev/null
	command -v openssl >/dev/null
	docker compose version >/dev/null

init-config:
	cp -n config/example.config.yaml config/config.yaml

cert-selfsigned:
	mkdir -p config/certs
	if [[ -s config/certs/server.crt && -s config/certs/server.key ]] && openssl x509 -checkend 86400 -noout -in config/certs/server.crt >/dev/null 2>&1; then \
	  echo "config/certs/server.crt and config/certs/server.key already exist"; \
	else \
	  openssl req -x509 -newkey rsa:4096 -sha256 -days 365 -nodes \
	    -keyout config/certs/server.key \
	    -out config/certs/server.crt \
	    -subj "/CN=localhost" \
	    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"; \
	fi

nginx-check: docker-preflight cert-selfsigned
	docker run --rm \
	  -v "$$PWD/config/nginx/nginx.conf:/etc/nginx/nginx.conf:ro" \
	  -v "$$PWD/config/certs:/etc/nginx/certs:ro" \
	  nginx:1.27-alpine nginx -t

up: docker-preflight init-config cert-selfsigned nginx-check
	docker compose up --build

down: docker-preflight
	docker compose down -v

logs: docker-preflight
	docker compose logs -f --tail=200

migrate: docker-preflight
	docker compose up flyway
