# План улучшений качества и стабильности — Voxhold-backend

Архитектурный разбор backend-репозитория. Факты с путями, приоритеты P0 → P3.

## Что уже хорошо

- Архитектура: ports/adapters (hexagonal-lite) — домен → `*/http` → `*/sqlite`, зависимости
  через интерфейсы у потребителя (`internal/realtime/http/handler.go:32-131`); 36 пакетов,
  main.go всего 368 строк ручного DI.
- SQLite: WAL + busy_timeout(5000) + synchronous(NORMAL) + foreign_keys через DSN
  (`internal/storage/sqlite.go:25-29`); пул ограничен (MaxOpen/Idle = 10).
- Безопасность: bcrypt DefaultCost + dummy-hash против timing-атак
  (`account/service.go:14,154-158`); токены 32B crypto/rand, в БД только SHA-256
  (`account/token.go:12-30`); server-side revoke с закрытием WS-сессий; refresh с ротацией;
  WS re-auth на каждом ping с проверкой сессии в БД (`handler.go:626-650`).
- WS hub: read limit 16 KiB, write deadline 3s, auth за 5s, slow-consumer disconnect при
  переполнении буфера 128 событий (`client.go:107-126`, `hub.go:587-590`), revoke по
  сессии/юзеру/серверу.
- Медиа: общий UDPMux, SetICETimeouts(10s/30s/2s), лимиты участников/зрителей с резервом мест,
  per-session bitrate limiters → failSession, ICE-restart recovery контроллер, bounded ICE-очереди.
- HTTP: MaxBytesReader 64 KiB + DisallowUnknownFields (`httpapi/request.go`), таймауты сервера,
  graceful shutdown c fallback Close() (`main.go:338-350`).
- Интеграционные тесты sqlite-in-memory по ключевым репозиториям.

## P0 — вероятные инциденты

### P0.1. Паники в фоновых горутинах убивают весь процесс

Факт: `recover()` — 0 вхождений. net/http сам гасит паники в хендлерах, но процесс целиком
падает от паники в собственных горутинах: hub broadcast/writePump (`internal/realtime`),
Pion-колбэки OnConnectionStateChange/RTCP-drain (`voice/session.go:256,460-479`,
`stream/manager.go`). Один nil-map/nil-pointer в broadcast = отказ всех сервисов сразу.

Предложение:
- Обёртка `safeGo(fn)` / `RunRecovered(name string, fn func())` с логом стека для всех фоновых
  горутин (hub, менеджеры, watchdog'и).
- Recover-middleware для HTTP — не для спасения процесса (его спасает net/http), а ради
  структурированного лога и корректного 500 вместо обрыва соединения.

### P0.2. healthz без проверки БД

Факт: `main.go:290-292` — всегда 204. Контейнер healthy при мёртвой БД; оркестрация и Caddy
продолжают слать трафик.

Предложение: healthz делает дешёвый `SELECT 1` c 500ms таймаутом; 503 при недоступности.
Деплой-healthcheck (`wget /healthz`) начнёт честно перезапускать контейнер.

### P0.3. CI без -race

Факт: `backend-ci.yml:39-43` — только `go test ./...` + `go vet`. Проект наполнен горутинами
(hub, медиа, RTCP-drain) — data race здесь класс №1 скрытых багов, и он воспроизводится именно
под детектором.

Предложение: `go test -race ./...` отдельным job (допускаю ×2 времени — приемлемо);
golangci-lint (govet, errcheck, staticcheck, gosec); позже coverage-gate на internal/realtime
и voice/stream.

## P1 — наблюдаемость

### P1.1. Структурированные логи + request ID

Факт: stdlib log, неструктурированный; request-ID/correlation нет нигде кроме WS-протокола
(`realtime/event.go:40,46`). Инцидент «у пользователя X пропали сообщения» не расследуется
корреляцией логов.

Предложение: slog (stdlib, JSON handler) + middleware, генерирующий request-id (или принимающий
X-Request-Id от Caddy), прокидывание через context в сервисы. Уровни: info в проде, debug под флагом.
Не логировать токены/credential (уже есть sanitize в diagnostics — переиспользовать подход).

### P1.2. Метрики

Факт: ни `/metrics`, ни pprof.

Минимальный набор Prometheus-совместимых метрик:
- ws_active_connections, ws_slow_consumer_disconnects_total
- voice_sessions_active, stream_viewers_active, p2p_viewers_active
- webrtc_recovery_attempts_total{result}
- rate_limit_rejected_total{scope}
- sqlite_busy_errors_total, http_requests_total{route,status}
- session_refresh_total{result}

Плюс pprof (net/http/pprof) на отдельном внутреннем порту или под admin-флагом.
Это разблокирует алерты деплой-уровня (см. IMPROVEMENTS.md Voxhold-deploy).

### P1.3. События жизненного цикла в логах

Факт: ключевые переходы (session created/expired, room full, failSession, viewer kick) частично
молчат. Для post-mortem нужен единый набор событий: actor, reason, ids, duration.

## P2 — надёжность и API-контракт

- **Централизованная конфигурация**: 58 env-переменных читаются os.Getenv вразброс
  (`cmd/api/main.go:81-118`, `antiabuse/config.go:59-119`, `storage/sqlite.go:14-17`).
  Один config-пакет: структура, дефолты, валидация, единица-тесты. Упростит будущий
  TURN REST-auth-secret и split CLIENT/SERVER ICE переменных.
- **Runtime endpoint из новых имён**: реализовать чтение `WEBRTC_CLIENT_ICE_*` (браузеры)
  и `WEBRTC_SERVER_ICE_*` (Pion) с fallback на legacy `WEBRTC_ICE_*` — закроет план
  README.webrtc-fixes.ru.md; deploy уже готов к этому.
- **Refresh-эндпоинт принимает тот же bearer** (`account/service.go:216-262`) — разделить
  access/refresh токены с разными TTL и scopes (сейчас компрометация одного токена = всё).
- **Origin allow-list**: только `wails.localhost` (`realtime/http/handler.go:200-208`);
  добавить конфигурируемый список для браузерных деплоев (PUBLIC_HOST).
- **Миграции**: embedded (embed.FS + golang-migrate source) → один бинарь, минус отдельный
  контейнер migrate в deploy.
- **Graceful degradation БД**: busy_timeout есть; рассмотреть ретраи на SQLITE_BUSY в
  транзакционных путях и метрику busy_errors (см. P1.2).

## P3 — структурное (осознанно)

- Разбиение realtime-пакета (4984 LOC, 25 файлов) на hub/client/presence/revoke под-домены —
  сейчас читаемо, но это самое толстое место для изменений.
- Интеграционный smoke-тест «два клиента: голос+стрим» под -race в CI (headless, без браузера —
  два in-process peer connection через hub) — ловит регрессии сигналинга до релиза.
- Документирование протокола WS-событий (event schema) как версии контракта frontend↔backend.

## Чего не делать

- Микросервисы, внешние очереди, распределённый лок — текущая single-binary модель правильная
  для масштаба.
- Переход на Postgres без доказанной потребности: SQLite+WAL покрывает текущие нагрузки,
  миграция добавит операционной нагрузки без выигрыша.

## Рекомендуемый порядок

```
Неделя 1: P0.1 (safeGo/recover), P0.2 (healthz+БД), P0.3 (-race в CI)
Неделя 2: P1.1 (slog+request-id)
Неделя 3: P1.2 (метрики+pprof), P2 config-пакет
Позже:    runtime ICE split, refresh-токены, embedded migrations, P3
```

## Статус реализации (после ревью VOXHOLD_REVIEW.md)

- B-P0.2 (по ревью): добавлен отдельный `GET /readyz` с PingContext(500ms); `/healthz`
  остался liveness без зависимостей.
- B-P0.1 (по ревью): пакет `internal/safego` (+тесты); границы паники на собственных
  горутинах и Pion-колбэках realtime/voice/stream/webrtcrecovery; повреждённая
  media-session закрывается через failSession, WS-loop сообщает об ошибке в канал.
- B-P0.3 (фаза 1): CI-job `test-race` (`go test -race ./...`), publish требует оба job.
- B-P2.2 (повышен до P1 по ревью): реализован split `WEBRTC_CLIENT_ICE_*` (runtime endpoint)
  / `WEBRTC_SERVER_ICE_*` (Pion) с fallback на legacy `WEBRTC_ICE_*`; `.env.example`
  документирует все три набора. Deploy-алиасы убираются только после релиза этого тега.
