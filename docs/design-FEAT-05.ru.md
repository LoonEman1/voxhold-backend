# Дизайн FEAT-05: расширенные права и переопределения на канал

Статус: черновик на утверждение. Код — только после одобрения владельца.

## Сейчас

Пять битов в `internal/permissions/permissions.go`: Read, Publish, Voice, Stream, ManageMessages.
Остальные действия жёстко привязаны к ролям owner/admin. Итоговые права считает view
`member_permissions` (миграции 000027, 000038).

## Предложение

- Новые биты: ManageChannels, ManageRoles, ModerateMembers (бан и тайм-аут), MentionEveryone,
  AttachFiles, AddReactions, ManageInvites, ManageWebhooks.
- Переопределения на канал: `channel_role_overrides(channel_id, role_id, allow, deny)`;
  итог = права сервера, затем deny, затем allow канала.
- Миграция сохраняет поведение: owner/admin получают все новые биты, member — AttachFiles и
  AddReactions, остальное нет. Существующие проверки роли owner/admin заменяются по одной,
  каждая в отдельном PR с тестом на неизменность поведения.
- SEC-04 (упоминание ролей) переходит с проверки роли на бит MentionEveryone.

## Риски

Много мест с проверкой роли (поиск по `'owner','admin'`); ошибка открывает действия не тем людям.
Нужен тест-таблица «роль × действие» до и после. View `member_permissions` — горячий путь,
замерить стоимость.

## Вопросы владельцу

Нужны ли переопределения на отдельных участников, или достаточно ролей? Какой набор новых битов
обязателен для первой версии?
