# Webcams / Вебкамеры

Webcams replace the participant card in voice channels and conversation calls.
The participant's name remains below the video. A click expands the camera;
fullscreen and Picture in Picture are available where supported. Microphone,
camera and screen sharing have independent controls and can run simultaneously.
Capture starts only after the user selects a preset and grants browser access.
Leaving, session replacement and device removal release the camera.

Three client presets set capture constraints and sender encoding limits:

| Preset | Maximum resolution | FPS | Target video bitrate |
| --- | --- | --- | --- |
| Low | 640 × 360 | 15 | 400 Kbit/s |
| Medium | 1280 × 720 | 24 | 1.2 Mbit/s |
| High | 1920 × 1080 | 30 | 2.5 Mbit/s |

The actual format depends on the camera and browser. Quality changes apply to
the existing track without rejoining. The preset is saved locally; permission
errors leave the microphone and screen intact.

## Voice channels

The voice SFU offers one additional receive video m-section (VP8/RTX) for every
participant. Clients reserve its sender direction when answering, then use
`replaceTrack` to start and stop the webcam. Relayed video uses track labels
`camera-<connection_id>`; audio retains `audio-<connection_id>`. Ownership must
be resolved from SDP m-section `mid` / `msid`, including reused transceivers.
Each participant can publish one audio and one video track. Webcam RTP has a
4 Mbit/s abuse guard with the same 150% headroom / four-second burst allowance
as microphone RTP. PLI/FIR requests are forwarded with a 500 ms cooldown.

`voice.state_update` accepts optional `self_video`; snapshots and participant
events include `self_video: true` while enabled. Omitting it preserves camera
state, so older microphone/deafen controls stay compatible. Moving or leaving
the voice channel clears camera state. Muting or deafening affects audio only.
Screen sharing retains its separate stream connection and UDP socket. Camera
video uses the voice connection and does not consume a screen publisher slot.
Audio-only clients can reject the additional video m-section.

## Conversation calls

The existing authenticated call signal `media` carries optional `camera` and
`camera_stream_id` alongside `screen` and the common monotonic `revision`.
An enabled camera requires a nonempty stream ID of at most 128 bytes. Signals
remain scoped to current source and target call leases. The client associates
remote video with its MediaStream ID from SDP, keeping camera and screen
separate even when both are active or a transceiver is reused. Camera stop
retains its stream ID in metadata so a remaining screen cannot adopt it.

WebRTC encrypts camera transport with DTLS-SRTP. As with screen video, webcams
are not covered by the voice end-to-end encryption scheme; the UI states this
in encrypted rooms and calls. Insertable-stream browsers attach the existing
video passthrough transforms before sending or receiving video.

## Русский

Камера появляется **вместо карточки** участника, имя остаётся снизу. Нажатие
раскрывает видео; доступны полный экран и картинка в картинке. Перед включением
пользователь выбирает низкое, среднее или высокое качество. Пресет можно менять
во время звонка; браузер изменяет разрешение, FPS и битрейт без переподключения.
Камера, микрофон и показ экрана работают независимо и одновременно.

В голосовом канале камера передаётся через SFU вместе с голосом отдельной VP8
дорожкой, без использования слота трансляции экрана. В личном звонке дорожки
камеры и экрана различаются по MediaStream ID и подтверждённым сессиям звонка.
Выход, замена сессии, отключение устройства и отменённый запрос разрешения
освобождают устройство. Передача камеры шифруется WebRTC, но не использует
сквозное шифрование голоса; интерфейс показывает это в зашифрованных звонках.

Regression coverage: real SFU camera/audio RTP, audio mute/deafen isolation,
room cleanup, lease validation, pending permission cancellation, SDP ownership,
card replacement and real Chromium camera/screen decoding, quality switching,
expansion and repeated camera start.
