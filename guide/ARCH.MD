---
name: clean-architecture-generic-skill
description: Универсальный LLM Skill для проектирования и развития Go-проектов в стиле Clean Architecture: слои, dependency rule, DI/wiring, тестовая стратегия, конфигурация, эволюция API и production policy.
---

# Clean Architecture Generic Skill (Go)

## 1. Назначение

Этот документ — абстрактный skill для LLM/агентов, которые проектируют, реализуют, рефакторят и ревьюят backend-проекты в стиле Clean Architecture.

Цели:
- сохранить чистые границы слоев;
- удерживать dependency rule при росте системы;
- стандартизировать DI, тесты, конфиг, эволюцию API;
- минимизировать архитектурный дрейф.

## 2. Когда применять

Применяй skill, если задача связана с:
- добавлением новой бизнес-фичи;
- рефакторингом архитектурных слоев;
- подключением новых адаптеров (HTTP, БД, очереди, кэш, внешние API);
- ревью архитектурной корректности изменений.

## 3. Архитектурные аксиомы

1. Dependency rule: зависимости только снаружи внутрь.
2. Domain не зависит от transport/framework/DB.
3. Интерфейсы зависимостей объявляет consumer-слой (обычно usecase/service).
4. Composition root один (единая точка wiring).
5. Бизнес-правила находятся в usecase/service, не в delivery/repository.

## 4. Базовая структура проекта

```text
project/
  cmd/ or app/                 # entrypoints, composition root
  domain/                      # entities, value objects, domain errors
  application/ or features/    # usecases/services, ports/interfaces
  internal/
    delivery/                  # http/grpc/cli handlers/controllers
    repository/                # db adapters
    integrations/              # external clients/adapters
    workers/                   # async consumers/producers
  configs/ or example.env
  migrations/
  tests/
```

Допустима feature-first и layer-first организация, если dependency rule сохраняется.

## 5. Слои и ответственность

### 5.1 Domain

Содержит:
- сущности;
- value objects;
- доменные инварианты;
- доменные ошибки.

Не содержит:
- SQL/ORM;
- HTTP/GRPC DTO;
- framework-зависимости.

### 5.2 Usecase / Service (Application Core)

Содержит:
- бизнес-сценарии;
- orchestration;
- порты (интерфейсы) для внешних зависимостей.

Не содержит:
- транспортные детали;
- реализацию БД/кэша/очередей.

### 5.3 Delivery adapters

Содержит:
- маршрутизацию;
- валидацию входа;
- mapping transport <-> usecase;
- mapping ошибок в transport status.

Не содержит:
- бизнес-решения.

### 5.4 Repository / Integration adapters

Содержит:
- реализацию портов usecase-слоя;
- SQL/NoSQL/HTTP-вызовы и mapping данных.

Не содержит:
- transport response logic;
- бизнес-политику уровня usecase.

## 6. DI и composition root

Порядок wiring:
1. Загрузить конфиг.
2. Создать infra clients (db/cache/queue/http clients).
3. Создать adapter implementations.
4. Передать adapters в usecase constructors.
5. Передать usecases в delivery handlers.
6. Запустить entrypoint.

Инвариант:
- ни один usecase не создает инфраструктурные зависимости сам.

## 7. Матрица допустимых зависимостей

1. `domain` -> stdlib + domain.
2. `application/usecase` -> domain + собственные порты.
3. `delivery` -> usecase contracts + transport libs.
4. `repository/integrations` -> domain + usecase ports + infra libs.
5. `composition root` -> все внутренние пакеты.

Запрещено:
- domain -> delivery/repository/integrations
- usecase -> delivery/repository concrete impl
- repository -> delivery

## 8. Паттерн добавления новой фичи

1. Описать доменный сценарий.
2. Добавить/обновить domain модели и ошибки.
3. Создать usecase + интерфейсы портов.
4. Написать unit tests для usecase.
5. Реализовать delivery adapter.
6. Реализовать repository/integration adapters.
7. Подключить в composition root.
8. Добавить integration/adapter tests.
9. Обновить архитектурную документацию.

## 9. Ошибки и контракты

1. Domain/usecase формируют бизнес-ошибки.
2. Delivery маппит их в transport-specific статусы.
3. Repository не должен возвращать transport-ошибки.
4. Error mapping должен быть централизован и предсказуем.

## 10. Конфигурация

1. Конфиг через env/конфиг-файл, читается в bootstrap.
2. Usecase/domain не читают env напрямую.
3. Обязательные параметры валидируются на старте (fail fast).
4. Секреты не логируются.

## 11. Транзакции и консистентность

Если сценарий атомарный:
1. Добавь порт транзакций в usecase слое.
2. Реализуй транзакционный адаптер в infrastructure.
3. Usecase координирует шаги внутри transaction boundary.

Для межсервисной консистентности рассматривай outbox/saga как внешние паттерны.

## 12. Наблюдаемость

Минимум:
1. Структурированные логи + request/correlation id.
2. Метрики latency/error/throughput по ключевым операциям.
3. Tracing через propagation в `context.Context`.

Правило:
- observability SDK остается в адаптерах/инфраструктуре.

## 13. Тестовая стратегия

### 13.1 Unit (обязательно)
- usecase тестируется в изоляции через моки портов.

### 13.2 Adapter tests
- delivery тестирует контракт протокола и валидацию.
- repository/integration тестирует mapping, retry, обработку ошибок.

### 13.3 Integration
- критические сценарии с реальными infra-компонентами.

### 13.4 Regression
- каждый bugfix сопровождается тестом, воспроизводящим баг.

## 14. Политика моков

1. Генерируй моки из интерфейсов портов.
2. Держи моки рядом с тестируемым слоем или в выделенном `mocks/`.
3. При изменении порта обновляй моки и тесты.

## 15. Anti-patterns

1. Бизнес-логика в handler/controller.
2. SQL/ORM в usecase.
3. Framework типы внутри domain.
4. Несколько неявных composition roots.
5. Прямые зависимости между sibling adapters.

## 16. Эволюция API

1. Предпочитай additive changes.
2. Breaking changes только с версионированием или миграционным планом.
3. Deprecated поля/эндпоинты удаляй после окна совместимости.
4. Контрактные тесты обновляй синхронно с API-изменениями.

## 17. ADR-подход

Создавай ADR при:
1. изменении направлений зависимостей;
2. вводе нового архитектурного паттерна;
3. значимых trade-off между консистентностью/латентностью/стоимостью;
4. изменении публичных контрактов.

Минимум ADR:
- Context
- Decision
- Alternatives
- Consequences

## 18. Production Policy (MUST / SHOULD / MAY)

### MUST

1. Соблюдать dependency rule.
2. Держать бизнес-правила в usecase/service.
3. Поддерживать единый composition root.
4. Прокидывать `context.Context` сквозь вызовы.
5. Покрывать измененные сценарии тестами по слоям.
6. Документировать архитектурные изменения.

### SHOULD

1. Использовать table-driven тесты для usecase.
2. Поддерживать централизованный error mapping.
3. Вести ADR для нетривиальных решений.
4. Держать конфиг и секреты отделенными от кода.

### MAY

1. Использовать CQRS/read models при росте нагрузки.
2. Добавлять async/event-driven интеграции при необходимости.
3. Выделять отдельные bounded contexts в самостоятельные сервисы.

## 19. Compact Checklist

1. Границы слоя изменения определены.
2. Запрещенные зависимости не появились.
3. Контракты портов обновлены корректно.
4. Wiring не размазан.
5. Бизнес-логика не утекла в adapters.
6. Тесты обновлены на всех затронутых слоях.
7. API эволюция задокументирована.
8. Документация синхронизирована.

## 20. LLM Execution Template

```text
You are implementing changes in a Go Clean Architecture codebase.

Hard rules:
1) Respect dependency rule (outer -> inner only).
2) Keep business logic in usecase/service layer.
3) Define dependency interfaces on consumer side.
4) Wire dependencies only in one composition root.
5) Propagate context.Context through all layers.
6) Update tests per touched layer.
7) Do not introduce undocumented breaking API changes.
8) Sync architecture docs with contract/invariant changes.

Execution order:
1) Update domain/usecase contracts and logic.
2) Update adapters.
3) Update wiring/bootstrap.
4) Update tests.
5) Update docs/ADR/checklists.
```
