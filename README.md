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
- `just build` - собрать бинарь в `build/server`
- `just run` - запустить сервер локально
- `just up` - поднять весь Docker Compose stack
- `just infra-up` - поднять только Postgres, Redis и Flyway
- `just down` - остановить compose и удалить volumes
- `just migrate` - прогнать Flyway миграции в compose
- `just fmt` / `just fmt-check` - форматирование
- `just lint` - локальные lint-проверки
- `just test` - все тесты, включая testcontainers-интеграцию
- `just test-integration` - все тесты verbose
- `just validate` - стандартный локальный quality gate

## Быстрый старт

Создать конфиг:

```bash
just init-config
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

- HTTP + WS: `127.0.0.1:8080`
- HTTPS + WSS: `127.0.0.1:8443`, если есть TLS cert/key
- TCP: `127.0.0.1:9000`
- TCP/TLS: `127.0.0.1:9443`, если есть TLS cert/key
- Postgres: `127.0.0.1:5432`
- Redis: `127.0.0.1:6379`

Discovery:

```bash
curl -s http://127.0.0.1:8080/api/v1/discovery/ --output discovery.cbor
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
- `app.output_ports` - порты, которые сервер отдаёт клиентам через discovery
- `app.tls.cert_file` / `app.tls.key_file` - сертификат и ключ
- `postgres` - подключение к PostgreSQL
- `redis` - подключение к Redis

Если сервер стоит за reverse proxy или load balancer, `output_ports` должны описывать
внешние порты, а не внутренние порты контейнера.

## TLS Сертификаты

Для локальной разработки можно выписать self-signed сертификат:

```bash
just cert-selfsigned
```

Он создаст:

```text
config/certs/server.crt
config/certs/server.key
```

После этого сервер включит TCP/TLS, HTTPS и WSS listeners. Без этих файлов сервер
продолжит работать, но только на non-TLS TCP и HTTP/WS.

Удалить локальные сертификаты:

```bash
just cert-clean
```

Для production лучше использовать нормальный публичный TLS-сертификат: Let's Encrypt,
сертификат от инфраструктуры, или TLS termination на ingress/reverse proxy.

## Нужен Ли Caddy/Nginx

Не обязателен.

Сервер сам умеет:

- HTTP API
- WebSocket upgrade на `/api/v1/client`
- raw TCP RPC
- HTTPS/WSS/TCP-TLS при наличии cert/key

Caddy/Nginx полезен, если нужно:

- автоматическое Let's Encrypt TLS
- единая публичная точка входа `443`
- HTTP/WS routing
- rate limiting, access logs, compression, allowlists
- проксирование из интернета в Docker/private network

Для HTTP/WS можно ставить Caddy/Nginx перед сервером. Важно прокидывать WebSocket upgrade.
Для raw TCP нужен либо прямой publish порта `9000/9443`, либо TCP/stream proxy
(например, Nginx `stream {}` или отдельный L4 load balancer). Обычный HTTP reverse proxy
не проксирует raw TCP RPC.

Минимальный Caddy пример для HTTP/WS:

```caddyfile
chat.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

Если TLS завершается в Caddy на `443`, а приложение внутри слушает `8080`, поставь
в `app.output_ports` внешние значения для клиентов, например `https_port: 443`,
`wss_port: 443`, а internal `app.ports.http_port` оставь `8080`.

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
APP_HTTP_PORT=8080
APP_HTTPS_PORT=8443
APP_TCP_PORT=9000
APP_TCP_TLS_PORT=9443
POSTGRES_PORT=5432
REDIS_PORT=6379
```

Если меняешь published ports, синхронизируй `config/config.yaml`:

- `app.ports` - то, что слушает процесс внутри контейнера
- `app.output_ports` - то, что увидит клиент снаружи

## Production Notes

- Не храни production secrets в `config/config.yaml` внутри репозитория.
- Для публичного HTTP/WS обычно удобнее TLS termination через Caddy/Nginx/ingress.
- Для raw TCP оставь прямой порт или используй L4/TCP proxy.
- Логи управляются секцией `logger`.
- TLS listeners автоматически отключаются, если cert/key отсутствуют.
