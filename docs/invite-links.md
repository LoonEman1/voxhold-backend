# Managing invitation links

Owners and current server administrators can inspect and revoke links created
by any manager of their server. Membership and role are checked inside the
same transaction as the list or revocation operation. Ordinary members and
non-members receive 403. A demoted creator has no special management access.

- `GET /api/v1/servers/{serverID}/invite-links?limit=50&before_id=<id>` returns
  `{ "invite_links": [...], "pagination": { "next_before_id": null, "has_more": false } }`.
  The default page size is 50, maximum 100, with descending ID pagination.
  Only non-revoked, unexpired links with uses remaining are returned. Metadata
  includes ID, creator ID/name, creation/expiration time, use count/limit and
  whether registration is enabled. Responses use `Cache-Control: no-store`.
- `DELETE /api/v1/servers/{serverID}/invite-links/{linkID}` returns 204 and sets
  the existing `revoked_at` field. Repeating a valid revocation is idempotent.
  An unknown ID returns 404 after authorization. The link must belong to the
  specified server; caller permissions are never inferred from link ownership.

No new migration is required. Resolution, new membership acceptance and new
account registration already reject `revoked_at` links. Revocation is atomic
with respect to invite consumption. Existing memberships and sessions are not
removed. A registration committed before revocation remains valid.

The complete URL is issued once at creation. The database retains only the
token hash, so the management list deliberately exposes metadata rather than
recoverable bearer tokens. Existing links can be managed immediately, without
reissuing them or changing token storage.

## Русский

Владелец и действующие администраторы видят активные ссылки всех создателей
своего сервера и могут закрывать их заранее. Участникам и посторонним доступ
закрыт. Создание ссылки в прошлом не сохраняет права управления после
понижения роли.

Список исключает истёкшие, отозванные и исчерпанные ссылки. Поля: номер ссылки,
создатель, время создания и окончания, число использований и лимит, разрешение
регистрации. Пагинация — по ID, 50 ссылок по умолчанию, максимум 100.

Отзыв сразу блокирует новые вступления и регистрации по ссылке. Он не удаляет
участников, которые уже вступили. Повторное закрытие безопасно. Проверка роли
и изменение ссылки выполняются в одной транзакции.

Новая миграция не нужна. Полный адрес по-прежнему выдаётся при создании;
в базе хранится только хеш токена. Поэтому в списке отображаются номера и
метаданные, а не восстанавливаемые полные URL старых ссылок.
