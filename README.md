# server_v2

Go-сервер SGTP v2: client RPC over HTTP/WS/TCP, auth challenge flow, профили, друзья,
комнаты, сообщения, события и discovery endpoint.

## Зависимости

Для локальной разработки и команд из `Justfile` нужны:

- `just` (проверено с форматом рецептов как в `chat_core`)
- `go` `1.25.x`
- Docker с `docker compose`
- `openssl` для локальных self-signed сертификатов
- `golangci-lint` для `just lint` и `just validate`

Для production-сервера, где нужен только `make`, достаточно Docker с `docker compose`
и `openssl`; `just`, Go и `golangci-lint` нужны для разработки и локальных проверок.

Быстрая проверка:

```bash
just preflight
go version
docker compose version
openssl version
golangci-lint version
```

Интеграционные тесты используют testcontainers и сами поднимают временный PostgreSQL
контейнер. Отдельно запущенная локальная БД для тестов не нужна.

## Команды

Показать все команды:

```bash
just
```

Основные команды:

- `just init-config` - создать `config/config.yaml` из примера, если файла ещё нет
- `just refresh-config` - перезаписать `config/config.yaml` текущим шаблоном
- `just build` - собрать бинарь в `build/server`
- `just run` - запустить сервер локально
- `just up` - поднять весь Docker Compose stack: Nginx, приложение, Postgres, Redis, Flyway
- `just infra-up` - поднять только Postgres, Redis и Flyway
- `just down` - остановить compose и удалить volumes
- `just migrate` - прогнать Flyway миграции в compose
- `just cert-selfsigned` - создать локальный self-signed сертификат для Nginx
- `just nginx-check` - проверить синтаксис Nginx-конфига
- `just fmt` / `just fmt-check` - форматирование
- `just lint` - локальные lint-проверки
- `just test` - все тесты, включая testcontainers-интеграцию
- `just test-integration` - все тесты verbose
- `just validate` - стандартный локальный quality gate
- `make` - поднять Docker Compose stack без установки `just`

## Быстрый старт

Создать конфиг:

```bash
just init-config
```

Если локальный `config/config.yaml` был создан старой версией шаблона, обнови его явно:

```bash
just refresh-config
```

Для локального запуска без Docker инфраструктуры сначала подними зависимости:

```bash
just infra-up
just run
```

Для полного запуска в контейнерах:

```bash
just up
```

По умолчанию Docker Compose публикует:

- HTTP + WS через Nginx: `127.0.0.1:80`
- HTTPS + WSS через Nginx TLS termination: `127.0.0.1:443`
- TCP через Nginx stream proxy: `127.0.0.1:834`
- TCP/TLS через Nginx stream TLS termination: `127.0.0.1:9443`
- Postgres: `127.0.0.1:5432`
- Redis: `127.0.0.1:6379`

Discovery:

```bash
curl -s http://127.0.0.1:80/api/v1/discovery/ --output discovery.cbor
```

Discovery отдаёт CBOR с портами `tcp_port`, `tcp_tls_port`, `http_port`, `https_port`,
`ws_port`, `wss_port`. Клиенты должны использовать эти порты как публичную точку входа.

## Конфигурация

Шаблон лежит в `config/example.config.yaml`, рабочий файл по умолчанию:

```text
config/config.yaml
```

Путь можно переопределить:

```bash
APP_CONFIG_PATH=/path/to/config.yaml just run
```

Важные секции:

- `app.ports` - реальные bind-порты процесса
- `app.output_ports` - публичные порты, которые сервер отдаёт клиентам через discovery
- `postgres` - подключение к PostgreSQL
- `redis` - подключение к Redis

Go-сервер слушает только чистые внутренние `tcp`, `http` и `ws` порты. TLS завершается
в Nginx, поэтому `app.output_ports.tcp_tls_port`, `https_port` и `wss_port` описывают
внешние Nginx-порты, а не bind-порты Go-процесса.

## TLS Сертификаты

TLS живёт на Nginx-слое. Для локальной разработки можно выписать self-signed сертификат:

```bash
just cert-selfsigned
```

Он создаст:

```text
config/certs/server.crt
config/certs/server.key
```

Эти файлы монтируются в Nginx как:

```text
/etc/nginx/certs/server.crt
/etc/nginx/certs/server.key
```

Nginx использует их для HTTPS/WSS (`443`) и TCP/TLS stream (`9443`). Сам Go-сервер
сертификаты не читает и TLS listeners не поднимает.

Проверить конфигурацию Nginx:

```bash
just nginx-check
```

Удалить локальные сертификаты:

```bash
just cert-clean
```

Для production положи публичный сертификат в `config/certs/server.crt` и ключ в
`config/certs/server.key`, либо замени volume в `docker-compose.yml` на путь к
сертификатам инфраструктуры, например `/etc/letsencrypt/live/<domain>`.

Если используешь Let's Encrypt на хосте, Nginx stream-блок для raw TCP/TLS будет выглядеть
так. Важно: `stream {}` должен быть на одном уровне с `http {}`, а не внутри него.

```nginx
stream {
  server {
    listen 9443 ssl;
    ssl_certificate /etc/letsencrypt/live/<domain>/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/<domain>/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    proxy_pass app:9000;
  }
}
```

Не ставь простой HTTPS/WSS `server { listen 443 ssl; }` и TCP/TLS `stream { listen 443 ssl; }`
на один и тот же IP:port одновременно. Для локального compose поэтому используются разные
порты: `443` для HTTPS/WSS и `9443` для raw TCP/TLS.

## Нужен Ли Caddy/Nginx

В Docker Compose Nginx теперь является публичной точкой входа.

Go-сервер внутри сети `server-v2-backend` слушает:

- HTTP API и WebSocket upgrade на `app:8080`
- raw TCP RPC на `app:9000`

Nginx снаружи даёт:

- HTTP/WS: `80 -> app:8080`
- HTTPS/WSS: `443 -> app:8080`
- TCP: `834 -> app:9000`
- TCP/TLS: `9443 -> app:9000`

Для raw TCP обязательно нужен `stream {}` или отдельный L4 load balancer. Обычный
`http {}` reverse proxy не проксирует raw TCP RPC.

## Тесты

Обычный запуск:

```bash
just test
```

Verbose интеграционный прогон:

```bash
just test-integration
```

Тесты, которые ходят в PostgreSQL, поднимают временный `postgres:16-alpine` через
testcontainers, накатывают SQL из `db/migrations`, прогоняют сценарии и удаляют контейнер
после завершения теста. Docker должен быть доступен пользователю, который запускает тесты.

Самый крупный e2e сценарий сейчас лежит в:

```text
integration/clientrpc/social_flow_test.go
```

Он проверяет discovery, TCP-клиента, WebSocket-клиента, auth, обязательный `updateProfile`,
friend request decline/accept и событие `profile.updated`.

## База Данных И Миграции

Миграции лежат в:

```text
db/migrations
```

В Docker Compose миграции выполняет Flyway:

```bash
just migrate
```

При `just up` приложение зависит от успешного завершения Flyway.

## Порты Docker Compose

Переопределить published ports можно через `.env` или переменные окружения:

```env
NGINX_HTTP_PORT=80
NGINX_HTTPS_PORT=443
NGINX_TCP_PORT=834
NGINX_TCP_TLS_PORT=9443
POSTGRES_PORT=5432
REDIS_PORT=6379
```

Если меняешь published Nginx ports, синхронизируй `config/config.yaml`:

- `app.ports` - то, что слушает Go-процесс внутри контейнера: обычно `tcp_port: 9000`,
  `http_port: 8080`, `ws_port: 8080`
- `app.output_ports` - то, что увидит клиент снаружи через Nginx

## Docker Push Credentials

Для FCM положи service account JSON в локальную ignored-папку:

```bash
mkdir -p secrets
cp /path/to/firebase-service-account.json secrets/firebase-service-account.json
chmod 600 secrets/firebase-service-account.json
```

И задай в `server/.env`:

```env
PUSH_FCM_ENABLED=true
PUSH_FCM_CREDENTIALS_FILE=/app/secrets/firebase-service-account.json
PUSH_FCM_PROJECT_ID=sgtp-3a2b6
```

`docker-compose.yml` монтирует `./secrets` в контейнер как `/app/secrets:ro`.

## Production Notes

- Не храни production secrets в `config/config.yaml` внутри репозитория.
- TLS termination для HTTP/WS и raw TCP/TLS находится в Nginx.
- Для raw TCP используй Nginx `stream {}` или отдельный L4/TCP proxy.
- Логи управляются секцией `logger` и пишутся в Serilog Compact JSON из `guide/LOGS.MD`.
