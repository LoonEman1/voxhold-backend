# Личные функции и организация сообщества

Voxhold **2.4.0** требует миграции **000031** и соответствующего frontend. Сначала
обновляется backend со схемой базы, затем клиент. Обновление расширяет существующие
каналы и беседы; голосовые каналы продолжают работать через прежний SFU.

Перед обновлением сохраните согласованную копию SQLite и каталога вложений.

## Избранное и пересылка

«Избранное» находится в левой выдвижной панели. Это отдельная беседа с единственным
участником — владельцем. В неё можно писать заметки, загружать файлы и пересылать
сообщения из каналов, ЛС и групп. К каждой сохранённой записи можно добавить
заметку. Поиск охватывает текст, заметки и имена файлов, включая кириллицу.

- `GET /api/v1/me/favorites` создаёт или возвращает личное избранное.
- `GET /api/v1/me/forward-destinations` возвращает доступные назначения.
- `POST /api/v1/me/forward` принимает `source: {kind,id}`, `destination: {kind,id}`
  и стабильный `client_message_id`. `kind` — `channel` или `conversation`; ID
  источника — ID сообщения, ID назначения — ID канала/беседы.
- `PATCH /api/v1/conversations/{conversationID}/messages/{messageID}/note`:
  `{note}`. Доступно только владельцу избранного.
- `DELETE /api/v1/conversations/{conversationID}/messages/{messageID}` удаляет
  сохранённую запись. Обычные ЛС этим endpoint не удаляются.

Пересылка сохраняет автора и время оригинала. Идентификаторы приватного канала
или беседы не экспортируются получателю. Вложения копируются независимо,
учитываются в квотах и остаются доступны в назначении после удаления оригинала.
Права источника и назначения проверяет сервер; для ЛС действуют блокировки и
настройки входящих сообщений.

## Реакции и уведомления в ЛС

У сообщений в личных и групповых беседах доступны то же меню и каталог реакций,
что у сообщений каналов. Повторное добавление реакции идемпотентно. Поле `me`
вычисляется отдельно для текущего пользователя; список участников реакции
страничный и доступен только участникам беседы.

- `GET/PUT/DELETE /api/v1/conversations/{ID}/messages/{messageID}/reactions/{emoji}`.
- `GET/PUT /api/v1/conversations/{ID}/notifications`, тело PUT: `{muted}`.

Отключение уведомлений сохраняется на сервере для каждого участника отдельно.
Сообщения, непрочитанные счётчики и история при этом обновляются. Звук использует
настройки громкости и устройства вывода Voxhold. Разрешение системных уведомлений
запрашивается кнопкой в личных настройках. Они работают, пока приложение открыто,
включая фоновую вкладку; Web Push для полностью закрытого браузера не добавлен.

## Отложенная отправка

Под полем ввода в каналах, ЛС, группах и избранном есть «Отложить сообщение».
В левой панели находится список с изменением времени/текста и отменой.

- `GET/POST /api/v1/me/scheduled-messages`.
- `PUT/DELETE /api/v1/me/scheduled-messages/{ID}`.
- Тело POST/PUT: `{destination:{kind,id},payload:{content,attachment_ids?,poll?,client_message_id?},send_at}`.

`send_at` — Unix-время в секундах, до года вперёд; клиент показывает местное время.
Стабильный ключ в payload предотвращает создание второго задания при повторе
POST. Задания сохраняются в SQLite и отправляются при работающем сервере даже
после закрытия клиента. После перезапуска просроченные ожидающие задания
обрабатываются автоматически. Стабильные ключи публикации исключают дублирование
при прерывании между отправкой и подтверждением задания. Смена прав, выход из
беседы или блокировка получателя могут перевести задание в `failed` с возможностью
исправить его. Неиспользованные вложения ожидающих заданий защищены от очистки.

## Опросы

Обычная отправка сообщения принимает необязательное поле `poll`:

```json
{"question":"Когда играем?","options":["Сегодня","Завтра"],"multiple":false,"anonymous":true,"closes_at":1800000000}
```

Разрешено 2–10 различных вариантов; вопрос — до 300 символов, вариант — до 120.
Создание сообщения и опроса происходит в одной транзакции. Участник отправляет
полный текущий выбор через `PUT /api/v1/polls/{ID}/votes`:
`{option_ids:[...]}`; пустой список снимает голос. После `closes_at` изменения
отклоняются. `GET /api/v1/polls/{ID}` возвращает актуальное состояние. У анонимного
опроса личности голосовавших не выдаются API; собственный выбор видит только
сам пользователь. Опросы работают в каналах и личных/групповых беседах.

## События

Раздел «События сервера» содержит создание и изменение карточек, отмену и ответы
«Пойду»/«Возможно». Время хранится как Unix-время, исходный часовой пояс проверяется
по базе IANA. Отображение использует часовой пояс браузера, включая переходы DST.

- `GET/POST /api/v1/servers/{serverID}/events`.
- `PUT/DELETE /api/v1/servers/{serverID}/events/{eventID}`.
- Тело создания: `{title,description,starts_at,timezone}`.
- `PUT/DELETE /api/v1/servers/{serverID}/events/{eventID}/response`;
  тело PUT: `{response:"going"|"maybe"}`.

Создавать события могут участники с правом публикации, изменять — автор или
администрация. За 15 минут сервер посылает записавшимся участникам напоминание;
звуковое/системное уведомление требует открытого клиента. Отменённые события и
пользователи без доступа к сообществу исключаются из рассылки.

## Журнал администрации

Раздел доступен владельцу и администраторам. Записи создаются в той же транзакции,
что изменение: создание/изменение/удаление канала, права канала и участника,
изменение/удаление роли, бан и удаление сообщения модератором. Сохраняются имя
действовавшего пользователя, цель, время и параметры изменения. Текст удаляемых
сообщений в журнал не копируется.

`GET /api/v1/servers/{serverID}/audit?actor_id={userID}&action={action}&before={id}`
возвращает до 50 записей и `has_more`. Через приложение журнал нельзя редактировать
или удалять. Имена действовавших пользователей сохраняются независимо от их
текущего участия в сообществе.

## Звонки в ЛС и группах

Кнопка «Позвонить / присоединиться» открывает звонок с микрофоном, отключением
звука и показом экрана. Панель звонка остаётся при переключении разделов.
Присоединение явно запрашивает микрофон; входящее приглашение само его не включает.

- `GET/PUT/DELETE /api/v1/conversations/{ID}/call`.
- PUT/DELETE используют случайный `session_id` экземпляра клиента. Явный вход с
  `replace:true` переносит звонок этой беседы в новое окно; heartbeat без этого
  флага не может вернуть старую сессию.
- `POST /api/v1/conversations/{ID}/call/signal` передаёт SDP, ICE или состояние показа (`media:{screen,revision}`) текущему
  участнику, с проверкой исходной и целевой сессии.
- Состояние включает монотонную `revision`: запоздавший HTTP-ответ или событие
  не должно заменять более свежий список участников.
- События: `conversation.call`, `conversation.call-signal`.

Медиа бесед передаётся напрямую между участниками через WebRTC; конфигурация
STUN/TURN берётся из существующего runtime API. Для сетей со строгим NAT нужен
доступный TURN. В группах используется mesh: исходящий трафик и число соединений
растут с количеством участников. Это отдельный транспорт от SFU голосовых
каналов. Можно находиться в одном звонке беседы; при переходе в голосовой канал
клиент завершает личный звонок. Сервер проверяет участие, блокировки и настройки
входящих, а сессии без heartbeat перестают учитываться через 45 секунд. Сигнализация использует
отдельный ограниченный бюджет `CALL_SIGNAL_RATE_LIMIT_RPS=120` и
`CALL_SIGNAL_RATE_LIMIT_BURST=240` на IP; обычные лимиты HTTP остаются прежними.

## Проверки

Новые backend-тесты проверяют приватность избранного, сохранность копий файлов,
квоты и очистку, ключи повторов, публикацию после прерывания, отзыв прав,
пользовательские поля реакций, анонимность/срок опросов, RSVP и сигнализацию звонков.
Сквозной `community-features.spec.ts` проверяет интерфейс с настоящим backend и
двумя и тремя браузерными аккаунтами, включая соединение WebRTC, получение аудио,
запуск/остановку показа экрана и возвращение в звонок после обновления страницы.

Для повторения: создайте отдельную пустую QA-базу командой
`QA_DIRECTORY=<отдельный каталог> go run ./scripts/qa-community`, запустите backend
с этой базой на порту 18083, затем в frontend выполните `npm run test:e2e:features`.
Учётные данные в QA-скрипте публичные и предназначены только для тестов.
Адрес тестового API можно изменить переменной `VOXHOLD_COMMUNITY_API`.

При многократных запусках с одного IP для QA можно задать
`WS_CONNECT_RATE_BURST=30` и `WS_CONNECT_RATE_PER_MINUTE=60`, чтобы создание и
перезагрузка тестовых окон не расходовали стандартный бюджет подключений.

## English overview

Voxhold **2.4.0** requires migration **000031** and the matching frontend. Back up the
SQLite database and attachment directory together; deploy the backend before
the client. The migration's downgrade removes the new feature data.

- The left personal rail contains private Favorites with notes and Unicode
  search, scheduled messages, server events and the administrator audit log.
- Messages can be forwarded between channels, direct/group conversations and
  Favorites. Attachments are copied independently and count toward storage quotas.
  Forwarding preserves attribution without exporting private source identifiers.
- Reactions, anonymous or public polls with a closing time, and scheduled sends
  work in direct and group conversations as well as channels. Scheduled work is
  durable, rechecks current permissions and uses stable publication keys to
  recover interrupted sends without duplicates. Deleting an account removes its
  Favorites, scheduled work, active call session, votes and RSVP data.
- Conversation mute settings are stored per member and preserve unread counts.
  Sound follows the existing volume/output settings. Desktop notifications need
  explicit browser permission and an open client, including a background tab;
  closed-browser Web Push is not implemented.
- Event cards show browser-local time, Going/Maybe responses and a reminder
  15 minutes before the start. Only current members with read access receive it.
- Administrative changes are recorded atomically with their mutations. Only
  owners and administrators can read the log, filtered by actor and action.
- Direct/group calls support microphone mute, deafen and screen sharing. Media
  uses WebRTC peer connections with the existing runtime STUN/TURN configuration;
  restrictive networks require TURN. Group calls use mesh, so bandwidth and peer
  count grow with participants. Channel voice keeps its existing SFU transport.
  Session identifiers and monotonically increasing membership revisions prevent
  old windows and delayed responses from taking over a newer call state.

API routes and payload examples are documented above. To run the full-stack
browser suite, seed a fresh disposable database with `scripts/qa-community`,
start its backend on port 18083, then run `npm run test:e2e:features` in the
frontend. Set `VOXHOLD_COMMUNITY_API` to change the API URL. For repeated QA runs
behind one IP, use the test server's `WS_CONNECT_RATE_BURST=30` and
`WS_CONNECT_RATE_PER_MINUTE=60`; production defaults remain unchanged.

Для плотной серии тестов можно также увеличить только на QA-сервере
`HTTP_RATE_LIMIT_RPS=100`, `HTTP_RATE_LIMIT_BURST=200`,
`AUTH_RATE_LIMIT_PER_MINUTE=120`, `AUTH_RATE_LIMIT_BURST=30`. / For dense QA runs,
these HTTP/auth overrides avoid throttling many test browsers behind one IP.

## Release review / Ревью релиза 2.4.0

Additional regressions cover closing/replacing a pending join without leaking
heartbeats, brief network failures versus revoked call access, concurrent screen
capture, immutable schedule creation keys and editing cancelled events. The
release must pass the full Go test/vet/race/leak gates, frontend unit/type/lint/
build checks, existing UI/performance suites and the real-backend two/three-user
browser scenario before production deployment.

Перед production-обновлением сохраняется согласованный бэкап SQLite и файлов.
Сначала применяется миграция 000031 и обновляется backend, затем frontend.
Проверяются health, версии образов, `integrity_check` и `foreign_key_check` базы.
