# Firefox AV1 interoperability / Совместимость AV1 с Firefox

Version 2.6.3, based on dev 2.6.2. Deploy the backend fix to an instance using
server-mode streams; changing the frontend alone does not repair negotiation.

The SFU's custom relay compared the publisher and viewer FMTP strings literally.
Chromium advertises `level-idx=5;profile=0;tier=0`; Firefox 159 answers with
`profile=0;level-idx=9;tier=0`, and Firefox 153 can omit default AV1 parameters.
These answers were rejected while binding the outgoing track, before playback.
Opus receiver preferences also differ between the browsers.

The relay now matches AV1 by profile, with missing profile inferred as Main (0),
consistent with the [AV1 RTP specification](https://aomediacodec.github.io/av1-rtp-spec/)
and Pion's AV1 matcher. MIME type, clock rate and channel count must still match;
different AV1 profiles remain incompatible. Opus receiver preferences do not
define different RTP payload formats. Existing H.264/VP9 profile checks remain.
Viewer codec preferences retain the registered AV1 primary/RTX payload pair when
the publisher includes FMTP parameters.

Regression coverage exercises relay binding, incompatible profiles, RTX pairing
and real Pion negotiation with a Firefox-style AV1 + Opus answer. A local browser
probe also sent actual Chromium AV1 video through the production SFU into an
isolated, officially signed Firefox 159.0: 640×360, 338 decoded frames and zero
dropped frames in the measured run. Firefox 153 was checked separately. The test
uses a generated canvas and tone, without user accounts or captured screen data.
A second run received 1920×1080 AV1 in Firefox 159: 315 decoded frames, zero
dropped frames. Firefox-to-Chromium publishing also decoded AV1 successfully;
the sender adapted resolution/frame rate under load. This does not certify
every GPU driver or hardware decoder.

AV1 is preserved. No browser flags, transcoding or silent codec downgrade are
required for the reproduced negotiation failure. There are no new dependencies,
database migrations or HTTP API changes.

## Русский

Исправление 2.6.3 подготовлено от dev 2.6.2. Для серверных трансляций нужно обновить
backend: одного обновления страницы или frontend недостаточно.

Причина ошибки — буквальное сравнение параметров AV1 на сервере. Chromium
отправляет одну строку FMTP, Firefox 159 возвращает другой порядок параметров и
свой уровень декодера. Firefox 153 может опускать параметры профиля по умолчанию.
Сервер отклонял совместимый ответ до начала воспроизведения. Дополнительно
различаются параметры приёма Opus.

Теперь совместимость AV1 определяется профилем; отсутствующий профиль означает
Main (0). Проверки типа кодека, частоты и каналов сохранены; разные профили AV1
не смешиваются. Сохранена пара AV1/RTX для повторной передачи потерянных пакетов.
Проверки профилей H.264 и VP9 не ослаблены.

Исправление проверено регрессионными тестами и реальной передачей AV1 через SFU
Voxhold в официальный Firefox 159.0 с отдельным временным профилем. В контрольном
прогоне 640×360 декодировано 338 кадров, отброшенных кадров — 0. Firefox 153
проверен отдельно. Использованы синтетические видео и звук; пользовательские
аккаунты, экран и содержимое диагностики не попадают в репозиторий.

Это исправляет воспроизведённую ошибку согласования, сохраняя AV1. Менять кодек
или настройки Firefox для этого исправления не требуется. Совместимость всех
видеодрайверов и аппаратных декодеров отдельно не гарантируется.

Дополнительный прогон просмотра в Firefox 159: AV1 1920×1080, 315 декодированных
кадров, отброшенных кадров — 0. Отправка AV1 из Firefox в Chromium также
проверена; отправитель адаптировал разрешение и частоту под нагрузку.
