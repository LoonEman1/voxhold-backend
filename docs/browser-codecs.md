# Firefox stream codec checks / Проверка кодеков Firefox

Follow-up for 2.6.3, 2026-11-03. Tests used the current frontend WebRTC service,
the production stream Manager and an isolated official Firefox 159.0 on Windows
against Chromium 151. Test media was an animated canvas and synthetic Opus audio;
no user accounts, screen capture or existing Firefox profiles were used.

Firefox advertised H.264, VP8, VP9 and AV1 in both sender and receiver
capabilities. Voxhold's role-aware codec list also exposed all four. VP9 was
profile 0; this does not imply HDR/profile-2 support.

| Codec | Chromium → SFU → Firefox | Firefox → SFU → Chromium |
| --- | --- | --- |
| VP8 | Passed: 72 decoded/presented frames, 1920×1080 | Passed: 47 frames, 1920×1080 |
| VP9, profile 0 | Passed: 74 frames, 1920×1080 | Passed: 47 frames, 1920×1080 |
| H.264, packetization mode 1 | Passed: 71 frames, 1920×1080 | Negotiated, but the test Firefox encoder produced zero frames |
| AV1, Main | Passed: 72 frames, 1920×1080 | Passed: 47 frames; sender adapted to 1374×773 |

These are short functional probes, not performance benchmarks. Passing requires
the requested codec in RTP stats, increasing decoded frames, presented frames
and no negotiation error. A capability entry alone is not a playback check.

Before the follow-up fix, six VP8/VP9/H.264 cases failed despite available
capabilities. The SFU relay compared entire FMTP strings, rejecting Firefox's
VP8 receive limits, omitted VP9 profile 0 and reordered H.264 parameters.
Firefox VP9 publishing was also rejected by rendition validation because the
default profile was omitted. These failures are corrected generically, without
user-agent checks or alternate codecs.

Format matching now follows codec semantics: VP8 receive limits do not identify
a different format; VP9 defaults to profile 0; H.264 keeps profile bytes and
packetization mode while negotiating levels separately. AV1 profile checks,
MIME types, clock rates, channel counts and registered primary/RTX pairs remain
protected. Unit tests reject VP9 profile 2 versus 0, different H.264 profiles,
different packetization modes and malformed H.264 profile values. Pion
negotiation tests use the actual Firefox-style parameters for all four codecs.

References: [VP8 RFC 7741](https://datatracker.ietf.org/doc/html/rfc7741),
[VP9 RFC 9628](https://datatracker.ietf.org/doc/html/rfc9628),
[H.264 RFC 6184 §8.2.2](https://datatracker.ietf.org/doc/html/rfc6184#section-8.2.2).

## H.264 publishing limitation

The same isolated Firefox also produced zero encoded/presented H.264 frames in
a native Firefox-to-Firefox RTCPeerConnection loopback, with no Voxhold code or
SFU. Reducing capture to 640×360 did not resolve it. Firefox's log showed its
Windows platform encoder path. The test profile could not obtain standard
OpenH264: Cisco's URL from Mozilla's manifest returned HTTP 403. This is an
environment/browser encoder limitation in this test, separate from the repaired
SDP interoperability. H.264 publishing is therefore **not certified** by this
run. No browser flags or user profiles were modified to conceal the failure.

See Mozilla's [codec diagnostics](https://firefox-source-docs.mozilla.org/contributing/debugging/debugging_webrtc_calls.html)
and [OpenH264 explanation](https://support.mozilla.org/en-US/kb/open-h264-plugin-firefox).

## Русский

Firefox 159 видит все четыре кодека и в списке браузера, и в Voxhold. Исправлены
общие ошибки SFU: буквальное сравнение FMTP, непризнанный профиль VP9 по
умолчанию и потеря зарегистрированной пары primary/RTX. Профили и режимы
упаковки по-прежнему проверяются; разные форматы не смешиваются. Это общее
исправление согласования, без специальных веток для Firefox.

После исправлений VP8 и VP9 передают реальные кадры в обе стороны; Firefox
принимает H.264 и AV1 в 1080p. AV1 из Firefox также декодируется в Chromium,
при этом отправитель адаптирует разрешение под нагрузку.

Отправку H.264 из тестового Firefox подтвердить не удалось: кадры отсутствуют
даже в нативном loopback без Voxhold, в том числе при 640×360. Стандартный
OpenH264 не удалось скачать из-за HTTP 403 на CDN Cisco. Причина на стороне
конкретного браузера/его окружения требует отдельной проверки; успешное
согласование не выдаётся за успешное воспроизведение.
