# Аудит и выпуск Voxhold 2.1.0

Дата: 27 октября 2026. Исходная точка: backend c5c1adb, frontend 0a46c32.
Пакет аудита: audit-handoff-2026-10-27. Изменения production не выполнялись.

## Выполненная работа

| Задания | Результат |
|---|---|
| B01 | Отложенные offers учитывают уже изменённые senders и pending ICE restart; актуальный snapshot читается под negotiationMu. Удаление последней stream-дорожки также согласуется. |
| B02 | В обоих Caddyfile перезаписывается X-Real-IP адресом TCP-клиента. Реальный Caddy 2.11.4 + Guard проверены на spoof/invalid headers, двух IP, HTTP rate limit и WS IP cap. Backend HTTP не публикуется наружу в compose. |
| B03 | Выбирается целиком CLIENT/SERVER namespace. Явная пустота отключает соответствующий ICE-набор; legacy используется только при полном отсутствии нового namespace. Credentials не смешиваются между наборами. |
| B04 | Один scheduler/refresh на поколение сессии, защита от старого 401 и результата после logout/login/unmount. Browser Web Locks и storage events координируют вкладки. Без Web Locks сохраняется защита одной вкладки; для старых браузеров межвкладочная координация не гарантируется. |
| B05 + U01 | Полный каталог восстанавливается после ready/reconnect. Пересечение HTTP с живой мутацией требует нового snapshot; старый сервер/запрос игнорируется. last_seen монотонен. Имеющие доступ + online — согласованный смысл списка. Ручная кнопка убрана, retry после ошибки сохранён. |
| B06 | Согласованные пользователем 20 секунд на первый SDP answer и 30 секунд на initial connection. Viewer до первого publisher track не ограничивается. Stop/close/успех отменяют timers; cleanup привязан к экземпляру сессии. |
| B07 | Общий контракт renditions для hub/SFU: обязательный SDR, codec/profile/color validation, уникальные ID/range. Invalid start отвергается до live state. |
| B08 | Сценарии lifecycle/revoke/replacement, 100 unanswered voice joins и 100 publisher/waiting-viewer циклов; Linux race и GC-based leak checks. |
| P01 | Опциональный отдельный loopback diagnostics listener: агрегаты peers/rooms/senders/queues, DBStats, фиксированные latency histograms и pprof. RTP timings — выборка 1/128, packets/errors — все. Нет user labels/SDP/tokens. Не реализован общий перехват всех SQL или счётчик всех SQLITE_BUSY. |
| P02 | Enqueue всем исправным получателям завершается до cleanup неудачных. Controlled blocking closer проверяет отсутствие этой задержки. Очереди/число workers не увеличены. |
| P03/P04 | Пакетная загрузка ACL с сохранением общего барьера revoke; одна проверка grants и одна bulk insert в транзакции. Fail-closed, dedup вкладок, purge private queues и rollback invalid grants проверены. |
| P06 | Каждую минуту удаляется максимум 256 истёкших sessions с двухсекундным context, отдельно от других maintenance операций. Срок авторизации не изменён; активная/rotated session сохраняется. |
| U02/U03 | Компактная карточка по центру комнаты; отдельные SVG записи/архива/чата, видимые короткие подписи, pressed/busy/disabled/focus. Проверка в настоящем браузере. |

## Проверки и нагрузка

Frontend: 255 unit-тестов (59 файлов), TypeScript, ESLint и production build;
30 Chromium UI-тестов и 12 performance/reliability browser-тестов прошли.

Linux Docker, Go 1.27.3: go test -race ./... с VOXHOLD_CHECK_GOROUTINE_LEAKS=1 и go vet ./....
Тесты Caddy запускаются отдельно с VOXHOLD_CADDY_BIN и VOXHOLD_DEPLOY_DIR;
обычный unit-прогон их явно пропускает, когда зависимости не заданы.
В Windows-песочнице recordings Rename и отдельные loopback TCP connections ограничены средой; Linux-прогон проходит.

Нагрузочный smoke использует настоящие Pion PeerConnections, ICE/DTLS/SRTP и UDP;
payload синтетический. 2/5/10 клиентов, voice 64/128 Кбит/с, sequential/simultaneous
join; stream 1/4/9 viewers при 6/16 Мбит/с с поздним подключением. По 3 повтора,
2 CPU и 1 GiB на весь контейнер; steady window 2 секунды, stream дополнительно
прогревается 1 секунду. Всего 54 сценария. Проверяется получение каждого speaker
каждым listener, ожидаемый fan-out, доставка не менее 80% отправленных RTP,
закрытие peers/workers и возврат sessions/rooms к нулю; затем runtime leak profile.

CPU/RSS/heap в этих тестах относятся **ко всему процессу: SFU + генераторы/получатели**.
Это не серверный budget/SLA, не качество декодирования и не доказательство вместимости production.
Voice all_tracks_ready — от начала Join до первого RTP от всех остальных; join_enqueue
измеряет только постановку signaling. Stream join_to_first_rtp_all — от начала Watch
до получения первого RTP всеми viewers. p95 в наборе из 2/5/10 — ближайший ранг,
то есть максимум этого малого набора; медиана далее берётся по трём повторам.
Исходные измерения и локальные ACL-бенчмарки: [measurements.json](audit-2.1/measurements.json).

ACL WAL microbench на Windows, Ryzen 7 9800X3D, Go 1.27.3, benchtime=200ms,
3 повтора, медианы (ns/op). Загрузка 1000 пользователей: 27 097 062 → 2 186 788;
SetAccess 1000 grants: 23 963 856 → 7 383 303. Для 10/100/1000 результаты
превышают порог 10% целевой SQL-метрики. Это не ускорение всей комнаты на такой процент.

Диагностика off/on, одинаковые 10 peers и три повтора: медиана CPU всего
процесса voice 128 Кбит/с sequential 0,0630 → 0,0590 ядра, simultaneous
0,0570 → 0,0604; stream 16 Мбит/с 0,1279 → 0,1401. Наблюдавшееся изменение
от −6,4% до +9,6% включает разброс короткого стенда; не является оценкой overhead
отдельного production SFU. Диагностика остаётся опциональной и по умолчанию выключенной.

## Сознательно оставлено без изменения

P05: для переписывания блокировок recordings нет замера на медленном диске,
который подтвердил бы полезность и crash/cleanup гарантии новой схемы.
P07: GOMEMLIMIT, GOGC, room caps, UDP/NACK buffers не менялись; прежние числа из
аудита не утверждались как SLA. Тестовый 1 GiB включает клиентов и не обосновывает лимит production.
P08/P09: нет представительного production CPU/alloc profile для PGO, общего JSON blob
или изменения владения RTP. DisableCopy не включён.

C01 закрыт как воспроизводимый direct synthetic smoke; смешанная voice+stream
browser-комната, TURN allocations/candidate pairs, tc netem RTT/loss/bandwidth,
длительная запись/аватары и несколько комнат на раздельных load/server процессах
остаются необходимыми перед утверждением capacity.
C02: выполнены burst/deferred regression и подсчёт offers; debounce/batching не добавлялись.
C03: реальный UDP fan-out и продолжение после write error проверены; mock slow writer
не считается доказательством production HOL. Пакетных очередей на viewer нет.
Решения C04–C07: [media-decisions-2.1.md](media-decisions-2.1.md).

## Воспроизведение

```sh
VOXHOLD_CHECK_GOROUTINE_LEAKS=1 go test -race ./... -count=1 -timeout 180s
go vet ./...
VOXHOLD_MEDIA_LOAD=1 VOXHOLD_LOAD_METRICS=1 VOXHOLD_CHECK_GOROUTINE_LEAKS=1 \
  go test ./internal/voice ./internal/stream -run 'Test(Voice|Stream)RoomLoad' -p 1 -count=3 -v -timeout 240s
go test ./internal/channel/sqlite -run '^$' -bench 'Benchmark(SetAccess|LoadAccess)' -benchtime=200ms -count=3
```

Proxy integration, из backend при соседнем checkout Voxhold-deploy:

```sh
docker build -f scripts/audit/proxy.Dockerfile -t voxhold-audit-caddy .
docker run --rm -v "$PWD:/src:ro" -v "$(dirname "$PWD")/Voxhold-deploy:/deploy:ro" \
  -w /src -e VOXHOLD_CADDY_BIN=/usr/local/bin/caddy -e VOXHOLD_DEPLOY_DIR=/deploy \
  voxhold-audit-caddy go test -race ./internal/antiabuse -run TestCaddyTrustedClientIdentity -count=1 -v
```
