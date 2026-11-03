# Parallel screen streams in a voice channel

Follow-up to 2.6.3. The channel catalog and SFU now identify streams by their
publisher connection, allowing up to eight publishers in one voice channel.
`streamcontract.MaxChannelPublishers` bounds both signaling and media resources.
Existing per-stream viewer and bitrate ceilings still apply.

## Additive WebSocket contract

- Authentication may include `multi_streams: true`; Ready advertises the same
  feature. Omitting it retains the legacy single-primary behavior.
- `stream.watch` accepts optional `publisher_connection_id`. The selected
  publisher must belong to the same authorized voice channel/server. An explicit
  unknown or foreign ID is rejected, without falling back to another publisher.
- Omitting the ID selects the deterministic primary (start time, then connection
  ID), preserving older clients. Snapshot ordering follows the same rule.
- `stream.stopped` and leave acknowledgements include the publisher connection
  ID. A publisher stop removes that stream and its viewers, leaving other
  publishers/viewers intact.
- New clients receive the full channel catalog. Legacy tabs receive one primary
  per channel and ignore other publishers' updates/stops. When their primary
  ends, the next active publisher is announced to them. Legacy clients retain
  the one-publisher start restriction.

SFU rooms are keyed by publisher connection ID. Media selection validates
server/channel ownership again. All users remain subject to voice membership,
channel access and stream permissions. Channel removal closes every publisher;
revoking one user's stream permission closes only that user's show. P2P offers,
answers, ICE and recovery remain bound to the actual publisher/viewer pair.
Generation checks prevent an old viewer failure from closing a newly selected
stream on the same connection.

Each connection preserves its existing single media role: publisher or viewer.
Viewers can switch streams after a leave acknowledgement. This change does not
create automatic fan-out to every card or permit arbitrary cross-channel viewing.

Live quality changes are handled by the paired frontend through capture
constraints and RTP sender limits. The server keeps the negotiated codec and
rendition contract; no transcoding or session restart is introduced.

No HTTP endpoints, database schema changes or migrations are added. Update
backend before frontend. Tests cover two publishers and independent viewers,
publisher capacity, legacy filtering, role revocation, P2P isolation, stale
failure callbacks and actual ICE/DTLS/SRTP forwarding of distinguishable RTP
payloads. The second publisher continues delivering new packets after the first
publisher stops.

## Русский

В голосовом канале допускается до восьми издателей. Каталог и SFU разделяют
потоки по `publisher_connection_id`; зритель выбирает нужный экран через
дополнительное поле в `stream.watch`. Неизвестный или чужой идентификатор
отклоняется. Остановка одного потока не затрагивает остальные.

Capability `multi_streams` сохраняет совместимость: старые вкладки получают
один основной поток и не закрывают его при остановке другого издателя.
Права доступа, участие в голосе и лимиты зрителей/битрейта сохраняются. Отзыв
прав одного участника завершает его показ, удаление канала — все его потоки.
P2P-сигналы и восстановление не переходят между чужими потоками.

Новые тесты проверяют изоляцию состояния и реальных RTP-пакетов через
ICE/DTLS/SRTP, продолжение второго потока после завершения первого и отбрасывание
ошибок старой медиасессии. Сначала обновляется backend, затем frontend;
новых миграций и HTTP API нет.
