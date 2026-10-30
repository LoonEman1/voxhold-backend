# Link previews / Предпросмотр ссылок

Available in the matching frontend/backend **2.5.1**. No new database migration;
the current schema remains **000031**. Deploy backend before frontend.

Public HTTPS pages can provide a title, description and image through Open Graph,
Twitter Card or ordinary HTML metadata. YouTube watch/shorts/live links additionally
use its fixed oEmbed endpoint. The feature is not limited to an allowlist of sites.
Pages requiring cookies, JavaScript rendering, authentication or bot challenges may
return no preview; the clickable link remains usable.

Authenticated API:

- `GET /api/v1/link-preview?url=<encoded HTTPS URL>` returns
  `{url,title,description?,site_name,image_path?}`, or `null` if unavailable.
- `GET /api/v1/link-preview/images/{key}` returns an authenticated JPEG thumbnail
  for cached metadata. It never accepts an arbitrary image URL from a client.

Requests run on the server, without user Authorization, cookies or environment
proxies. Only HTTPS on port 443 is fetched. Every connection resolves and checks
all DNS answers, then dials a checked public IP directly, preserving TLS hostname
verification. Private, loopback, link-local, mapped, reserved and metadata-service
addresses are rejected; redirects are validated too. HTML and image downloads
have deadlines and size limits. Images are decoded with a pixel limit and
re-encoded to JPEG at most 640×480; external HTML/iframe content is never embedded.

There are at most four concurrent fetches, 256 metadata cache entries, a ten-minute
positive cache and one-minute failure cache. The client requests cards/images
near the viewport and shows at most two cards per message. Images are fetched
through the authenticated API as blobs, so the reader's browser does not contact
the publisher/CDN automatically. Existing CSP rules remain sufficient.

Conversation listing remains an array of up to 200 rows for compatibility.
Continue with `before_activity=<last_message_id>&before_id=<id>` using the last
row. The matching client follows pages and deduplicates them. Open a specific
conversation using `GET /api/v1/conversations/{conversationID}`; membership is
checked independently of the current list page. Conversation members are loaded
in one batch per page.

Личные сообщения при восстановлении связи загружаются постранично до начала
уже открытого диапазона, после чего история обновляется целиком. Это также
освежает старые реакции, заметки и удаления. Запросы отменяются при смене
разговора/поиска; более новые локальные отправки и ревизии сохраняются.

Текстовые черновики frontend сохраняет локально отдельно для инстанса,
пользователя, канала, обсуждения и личной беседы. Файлы в черновик не входят.
Успешная отправка очищает соответствующий текст; сбой локального хранилища
не мешает вводить сообщение. Черновики не синхронизируются между устройствами.

References: [Open Graph](https://ogp.me/), [oEmbed](https://oembed.com/),
[OWASP SSRF prevention](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html).
