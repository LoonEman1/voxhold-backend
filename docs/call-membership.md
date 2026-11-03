# Leaving a conversation / Выход из беседы

Deleting conversation membership already removes its active call lease through
the existing foreign key. The endpoint now immediately publishes the resulting
call state to the remaining conversation members and the leaving user's tabs.
Peers can close the corresponding media connections without waiting for the
next heartbeat. Existing revisions keep out-of-order snapshots from restoring
removed participants.

An idempotent request by a nonmember does not publish or disclose call state.
No new migration or API shape is required. Regression coverage verifies lease
removal, a newer revision, notification recipients and denied rejoin.

## Русский

Выход из беседы уже удалял участие в звонке благодаря внешнему ключу. Теперь
сервер сразу рассылает обновлённый состав звонка оставшимся участникам и вкладкам
вышедшего пользователя. Соединения закрываются без ожидания следующего heartbeat.
Номер ревизии защищает от восстановления участников устаревшим событием.

Повторный запрос от пользователя вне беседы не раскрывает и не рассылает её
состояние. Миграции и формат API не меняются. Тест проверяет удаление участия,
рост ревизии, получателей события и запрет повторного входа.
