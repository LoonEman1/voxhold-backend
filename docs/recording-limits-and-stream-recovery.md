# Recording limits and stream viewer recovery

`GET /api/v1/servers/{serverID}/recordings` now includes `quota_bytes`, the
instance owner's effective configured storage budget. The existing
`max_file_bytes` and `retention_seconds` fields retain their meanings. The
frontend shows the storage quota independently from the per-file upload cap.
The list still requires membership and exposes no owner-only usage or settings
controls. Changes to quota management permissions are not required.

`stream.leave` is idempotent: an already removed viewer receives `stream.left`
with the original request ID and zero server/channel IDs. This acknowledges
that leaving has completed when closing a failed peer races its leave request.
The client can then issue `stream.watch` without reloading the page. Active
publishers and viewers retain their existing stop/leave behavior.

## Русский

Список записей возвращает `quota_bytes` — действующий лимит хранилища,
указанный владельцем инстанса. Поля `max_file_bytes` и `retention_seconds`
сохраняют прежний смысл. Интерфейс показывает общую квоту отдельно от лимита
одного файла. Права чтения списка и изменения настроек остаются прежними.

Повторный `stream.leave` для уже удалённой сессии подтверждается событием
`stream.left` с исходным ID запроса и нулевыми ID сервера/канала. Это позволяет
продолжить переподключение, если сервер успел удалить старый просмотр до
обработки выхода. Поведение активных трансляций и зрителей остаётся прежним.
