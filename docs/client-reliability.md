# Channel history, retries and outgoing event queues

Apply migrations **000020** and **000021** before starting this backend. Deploy
the backend before the matching frontend. Existing clients can continue using
the original history and send endpoints without a client message ID.

## Message synchronization

`GET /api/v1/servers/{serverID}/channels/{channelID}/messages/sync` returns
`messages`, `deleted_ids`, `cursor`, `reset`, `has_more`, and `has_older`.
Omitting `after` returns the latest 50 messages and their cursor from the same
SQLite read transaction. `?after=<cursor>` reads up to 200 journal changes;
keep requesting pages while `has_more` is true. Apply edits by the existing
message revision and apply deletion tombstones before merging messages.

Journal entries are written by SQLite triggers in the message transaction,
including deletions. A background worker prunes in transactions of at most
500 selected changes, with a 24-hour retention and a target of 100,000 journal
rows. It preserves a per-channel cursor and pruning floor. A cursor behind
the floor, or ahead of the current cursor, returns `reset: true`: discard the
old history window and apply the authoritative snapshot. Retention cleanup
is time-budgeted; the size target is not a hard admission limit during bursts.
Every page checks current server membership and the channel's server.

## Safe send retries

Message creation accepts optional `client_message_id` (1–64 ASCII letters,
digits, underscores or hyphens). Use a fresh UUID per logical send and retain
it for manual retries. The key is scoped to author and channel. Reusing it
with different content returns 409; retrying after deletion returns 410.
Concurrent retries return the same message ID. Only a content hash is retained
in the retry table; deleted message text is not duplicated there.
Creation responses and WS `message.created` echo the optional key. Replays may
repeat the WS event; clients must deduplicate by message ID and revision.

## Backpressure and diagnostics

An authenticated WS client can advertise `message_sync: true` in its auth data.
It must then handle `channel.sync_required` (`server_id`, `channel_id`) by
syncing messages and refreshing pins. For these clients a full chat queue can
be compacted into a catch-up marker, preserving the media session. Legacy
clients retain disconnect-on-overflow behavior and never silently lose deltas.

Ordinary and media events have independent 128-event limits. Each lane is FIFO;
the writer serves ordinary events after at most eight consecutive media events.
Only presence updates and read marks with the same entity key are replaceable;
read marks never move backwards. SDP, ICE, permission changes and replies with
request IDs are not discarded. Genuine control or media overflow still closes
an unresponsive client. Queue summaries log high watermarks, coalescing and
resync totals without message contents or SDP.

Client diagnostic ingestion keeps its existing hard global and per-user limits.
A trigger-maintained storage count replaces the full-table OFFSET sweep on
each request. Expiration is cleaned in 500-row background batches; global
overflow evicts only the rows needed by the incoming batch.

## Русский

Перед обновлением примените миграции **000020**, **000021**. Сначала обновляется
backend, затем frontend. Старые клиенты работают с прежними endpoint; новый
frontend использует `/messages/sync`.

Синхронизация восстанавливает изменения и удаления после обрыва. Курсор и
снимок читаются в одной транзакции; журнал меняется атомарно с сообщениями.
При устаревшем курсоре сервер возвращает новый снимок, а не неполную дельту.
История журнала очищается фоновыми порциями; целевые ограничения — 24 часа и
100 000 изменений. Это целевой размер после очистки, не жёсткий предел при пике.

`client_message_id` защищает повторную отправку от дублей. Ключ привязан к
автору и каналу; другой текст с тем же ключом даёт 409, удалённое сообщение —
410. Повтор не восстанавливает удалённое сообщение. Таблица хранит хеш текста.

Новый клиент объявляет `message_sync: true` и обрабатывает
`channel.sync_required`. Переполненные дельты чата заменяются запросом
синхронизации, а медиа используют отдельную ограниченную очередь. Сигналинг
и изменения доступа не отбрасываются. Для действительно зависшего клиента
сохраняется отключение при исчерпании очередей.

Очистка диагностики вынесена из HTTP-запросов. Общий лимит остаётся жёстким;
счётчик поддерживается триггерами, включая каскадные удаления.
