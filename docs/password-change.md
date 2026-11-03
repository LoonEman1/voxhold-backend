# Atomic password changes / Атомарная смена пароля

Password replacement and revocation of other sessions now share one SQLite
transaction. If deleting sessions or committing fails, the old password and
sessions are retained. Live WebSocket sessions are revoked only after commit.

The update also compares the password hash that was verified before hashing the
new password. A concurrent password change invalidates that comparison and is
reported through the existing incorrect-current-password response.

Bcrypt verification and hashing stay outside the writer transaction. HTTP routes,
request/response formats, database schema and release version remain compatible
with 2.6.2.

`TestPasswordChangeAndSessionManagement` injects a session deletion failure and
checks rollback, live-session preservation, successful retry, rejection of stale
credentials, and isolation from another user's session.

## Русский

Смена пароля и завершение остальных сессий теперь выполняются одной транзакцией
SQLite. При ошибке удаления сессий или фиксации сохраняются прежний пароль и
сессии. Активные WebSocket-соединения закрываются после успешной фиксации.

Обновление сравнивает хеш пароля, проверенный перед вычислением нового хеша.
Параллельная смена пароля отклоняет устаревшее подтверждение через существующий
ответ о неверном текущем пароле. Проверка и вычисление bcrypt выполняются до
начала транзакции и не удерживают блокировку записи.

API, схема базы и номер версии совместимы с 2.6.2. Регрессионный тест проверяет
откат при искусственной ошибке БД, сохранность сессий, успешный повтор, отклонение
устаревших данных и изоляцию сессий другого пользователя.
