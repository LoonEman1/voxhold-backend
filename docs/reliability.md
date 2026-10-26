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
