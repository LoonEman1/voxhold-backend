# Reliability and upgrade notes — v0.10.0

Apply migration **000019** before starting this backend. It adds a positive,
monotonically increasing `revision` to messages. History, search, context,
pins, HTTP mutation responses and WebSocket message events include this
additive field. Clients must retain the highest revision they have seen for
each message; timestamps alone cannot order edits made in the same second.
The matching web client is v1.11.0. Update the backend before the web client.

Write transactions now reserve SQLite's writer with `BEGIN IMMEDIATE` before
authorization reads. The WAL pool still supports concurrent read-only
transactions. This prevents read-to-write upgrade failures when another
writer commits between a permission check and a message insertion. Regression
tests exercise the real file-backed production pool with eight writers.

Voice and stream failures are scoped to the logical join/watch that created
their PeerConnection. Delayed teardown callbacks cannot disconnect a newer
join using the same WebSocket. Media transitions are serialized per user,
and independent users retain independent locks. Room capacity reservations
are made before a voice room can be removed from its manager.

Account deletion removes the avatar and client diagnostics together with the
profile and sessions. Server recordings and their previews retain their
existing expiration and remain available to current instance members until
the normal 24-hour cleanup. Deleted accounts receive a collision-resistant
anonymous display name. Expired login failure windows no longer accumulate
old failures indefinitely. WebSocket signaling accepts bounded messages up
to 128 KiB to support SDP answers for the configured maximum room size.

## Обновление и поведение

Перед запуском v0.10.0 примените миграцию **000019**. Поле `revision` добавлено
во все представления сообщений; клиент v1.11.0 отбрасывает устаревшие правки
и ответы истории. Сначала обновите backend, затем frontend.

Исправлены конфликты параллельной записи SQLite, запоздалое закрытие новой
голосовой сессии или просмотра трансляции, гонка резервирования голосовой
комнаты и подписки после отключения клиента. Закрытый hub не принимает
новые подключения. Истёкшие ошибки входа больше не влияют на новое окно
ограничения попыток, лимит сигналинга увеличен до 128 КиБ.

При удалении аккаунта удаляются аватар и диагностика. Записи и превью
сохраняются до своего обычного срока удаления через 24 часа. Имя удалённого
пользователя больше не конфликтует с заранее занятым отображаемым именем.


## Audit reliability release (2.1)

See [audit coverage and measurements](audit-2.1.ru.md) and
[media architecture decisions](media-decisions-2.1.md). Initial WebRTC answer and
connection deadlines are 20 and 30 seconds respectively; a stream viewer waiting
for its first publisher track has no offer deadline until an offer is sent.
A later session on the same connection is protected from stale failure callbacks.

Optional `DIAGNOSTICS_LISTEN_ADDR=127.0.0.1:9091` enables a separate local listener
with `/debug/vars` and `/debug/pprof/`. Leave it empty to disable instrumentation.
Only an explicit loopback IP is accepted; never reverse-proxy or publish this port.
For a container, access it from inside that container or a deliberately secured
local tunnel. Counters contain fixed metric names and aggregate resource counts,
not user IDs, SDP, bearer tokens or private content. Duration buckets are upper
bounds of 1/10/100 microseconds, 1/10/100 milliseconds, 1/10 seconds, then infinity.
RTP `Count`/`Errors` include every write; timing totals/buckets cover one in 128.
Use `TimingSamples`, not `Count`, to compute sampled average forwarding duration.
`voice_answer`/`stream_answer` include initial and later offer-answer exchanges.
DBStats describes pool utilization; this does not instrument every SQL statement.

CLIENT/SERVER ICE namespaces are selected as a whole. If any variable of a namespace
is set, unset fields there are empty and no individual legacy credential is inherited.
Legacy-only installations keep their existing behavior. Explicitly empty SERVER values
prevent Pion from creating client TURN allocations. The client TURN configuration is
independent. Deploy Caddy must overwrite `X-Real-IP` at the trusted public edge.
