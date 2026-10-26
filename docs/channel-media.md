# Voice-channel chat, recordings and avatars

## Upgrade and storage

Apply migrations **000015** (optimized avatar BLOBs), **000016** (recording metadata), **000017** (owner recording quota) and **000018** (recording previews, required for v0.9.2+) before starting this API. The existing migration runner/deployment process still owns migration execution. The matching frontend is required for the new controls. Go is now 1.27.3; the Pion and crypto upgrades are included in this branch.

Recordings use a directory on the persistent data volume, separate from SQLite. Defaults:

| Setting | Default | Purpose |
| --- | --- | --- |
| `RECORDING_DIRECTORY` | `data/recordings` | Private partial/final media files; never expose as static files |
| `RECORDING_STORAGE_QUOTA_BYTES` | `6442450944` | 6 GiB default total of reserved uploads and ready files; the instance owner can set 1 GiB-1 TiB in the server settings (`GET`/`PUT /api/v1/instance/recording-storage`), which overrides this value. Lowering it affects new uploads only; existing files expire after 24 hours |
| `RECORDING_DOWNLOAD_BYTES_PER_SECOND` | `2097152` | Shared 2 MiB/s recording download budget |

The per-file server limit is **2,147,483,648 bytes, inclusive**. Larger files stay in the recorder's browser. No server encoder, ffmpeg process, or media decoding is started. Live WebRTC bitrate/resolution is unchanged. Limits bound recording work, but do not promise isolation from every CPU/disk/network bottleneck; size the instance for concurrent calls and downloads.

## Recording lifecycle and access

1. A connected voice participant starts a recording. One active recorder per channel and per user, at most 20 globally; partial unique indexes settle concurrent starts. `recording.updated` broadcasts full metadata to current server clients, including the recorder name. Clients show a red dot and identify who records.
2. The browser encodes screen video plus the microphone, remote voices and stream audio. It saves chunks locally, then automatically initiates an attachment download on stop. Recording is scoped to the recorder's active channel and stops on leave/change. Browser download permissions may require the user to press **Скачать** in the recording list.
3. `POST /recordings/{id}/stop` publishes the completed/local-only state. Files within the limit reserve quota before upload. A full quota returns 507; the client releases the active indicator as local-only and retains its local file for retry. The client defers server uploads while it is in a voice call.
4. `GET /recordings/{id}/upload` returns the actual file offset. `PATCH /recordings/{id}/content?offset=N` accepts up to 1 MiB (the client uses 256 KiB); mismatching offsets return 409. A lost response is recovered by querying the offset on retry. Four upload handlers bound buffered input. Writes are rooted with `os.Root`; names derive solely from random 128-bit IDs.
5. `POST /recordings/{id}/complete` checks exact size and the declared WebM/MP4 container signature, syncs and renames the partial file, and commits `ready`. This is a container signature check, not a full media parser. Repeated completion is idempotent, including recovery after rename succeeded but the metadata commit failed. Monotonic per-record `updated_at` prevents older HTTP snapshots from replacing newer WebSocket state.
6. `GET /servers/{serverID}/recordings` lists up to 100 current records. All current server members can download ready server copies. Only the owner can mutate an upload. A ticket request is authenticated; the resulting signed bearer URL lasts five minutes, rechecks current membership/deletion/ban and retention when opened, and serves an attachment with native HTTP range support. A running download may outlive its ticket. A new range request after expiry needs a new ticket. Restart invalidates old tickets. Avoid logging or sharing these short-lived URLs.

Server copies expire 24 hours after completion; queued uploads/local-only metadata also expire after 24 hours. A minute cleanup job removes expired files before deleting rows, cleans orphan files after FK cascades, and marks active recordings failed after a two-minute heartbeat lease. Heartbeats are sent every 30 seconds. Startup clears abandoned active indicators. Local browser copies remain until explicitly removed; they are account/instance scoped and can survive reloads. Clearing browser site data deletes them. An interrupted recording exposes the saved portion, which may need recovery by an external media tool depending on its container.

The API and filesystem are intended for **one API process per SQLite database/storage directory**. Coordination/quota locks and ticket keys are process-local; shared multi-replica recording storage needs a different design.

## Recording previews

The browser extracts a frame near the beginning from at most the first four local chunks (capped at 8 MiB), one video at a time with a five-second decode limit. It stores the 320×180 JPEG in local recording metadata. Opening the list can backfill older local copies. A preview is optional: unsupported or interrupted files retain a placeholder and can still be downloaded. Remote videos are not downloaded to generate their cards.

After the video is uploaded, its owner can `PUT /api/v1/recordings/{id}/preview` with JPEG/PNG/WebP up to 128 KiB and 1280×720. Two processing slots bound decode work. The server re-encodes to a metadata-free 320×180 JPEG, at most 64 KiB, and counts its bytes towards the instance recording quota. Preview failure does not undo a ready video. Uploads, including backfill, wait until the owner leaves the voice call.

`preview_version` is optional in recording metadata and `recording.updated` events. Authenticated `GET /api/v1/recordings/{id}/preview` checks current membership, bans and expiry before reading or returning conditional 304. Private revalidation caches use ETags. The preview shares the recording's 24-hour retention and is removed by a foreign-key cascade on recording, channel, server or account deletion. Existing server-only recordings without a preview keep the placeholder until their owner supplies one from a local copy.

## Avatars

Authenticated raw `PUT /api/v1/me/avatar` accepts JPEG/PNG/WebP/GIF up to 5 MiB and 16 million decoded pixels. Optional `crop_x`, `crop_y`, and `crop_size` query parameters must be supplied together as integer pixels defining a square inside the EXIF-oriented image. Without them the crop is centered. Invalid, duplicate, or out-of-bounds coordinates return 400 without changing the existing avatar. Configuration is checked before decode; two processing slots limit parallel image allocations.

The server applies JPEG EXIF orientation, crops the selected region, scales to 256×256 and erases alpha **and RGB** outside the circle. Only this transparent PNG derivative is stored. High-entropy images use palette quantization if necessary to fit the existing 128 KiB database limit. Metadata, animation, the source image and pixels outside the selected circle are discarded. GIF uses the first frame. Existing JPEG avatars remain readable with their original content type; no migration is required.

`GET /api/v1/users/{id}/avatar` uses existing profile visibility, including before conditional 304 responses. The response has a private ETag cache; the frontend revalidates HTTP cache on invalidation and shares lazily loaded Blob URLs among visible avatars. `DELETE /api/v1/me/avatar` restores initials. `avatar.updated` invalidates clients after save/removal. Auth/session caches were not introduced, so membership and session revocation keep their existing behavior.

## Voice-channel messages

Messages, FTS search, pins, context windows, read cursors and notifications reuse the existing tables and channel ID. Both `text` and `voice` kinds are accepted; server membership/channel checks remain in each repository path. No voice connection is required to read or write that channel's text chat. The frontend loads/subscribes to a voice chat when its chat panel is visible.

## Validation and review focus

Real migration-chain tests cover upload ownership, exclusive starts, competing starts, offsets, incomplete/corrupt completion, quota, the >2 GiB local-only path, signed attachment/range downloads, membership revocation, ticket expiry, lease/retention cleanup, orphan removal, and rename/commit recovery. Avatar tests verify crop/format/size, access before ETag, and removal. Voice chat tests verify history/search/read cursors and denied nonmembers.

Chromium browser tests in the frontend cover actual encoded output, automatic/repeat downloads, IndexedDB recovery, source replacement without ending live tracks, and deferred upload through call controls. These do not substitute for a real Yandex/Firefox/Safari call or a ten-participant load test. See the frontend documentation for recording compatibility and performance changes.
