# Community 2.3 final review

The cumulative release was reviewed across backend, frontend and deployment
instructions. New behavior is documented in [the contract](community-2.3.md).

| Feature | Independent regression evidence |
| --- | --- |
| Attachments and disk management | `internal/attachments`: access, staged-file privacy, image limits/compression, retry, quota rejection, immutable cleanup preview, tombstones. `internal/community`: transactional private-file binding and replay. |
| Direct/group conversations | `internal/community`: common-community default, privacy changes, bilateral blocks, membership revocation, scoped read cursors, idempotency conflict. |
| Inbox | `internal/community`: accessible message links and independent thread unread after channel read; indexes cover notification/subscription candidates. |
| Custom roles | `internal/community`, channel/message tests: read/write/voice enforcement, moderation, atomic assignment rollback. `internal/realtime`: retain allowed media, stop only revoked stream/voice. |
| Text discussions | Preserve ordinary quotes; explicit same-channel roots; comment pagination; root revision/counts; retry keys; reading versus explicit unsubscribe. |

Review fixes included: compatibility with existing message-key constraints;
owner cleanup of other members' recordings; independent thread read state;
preservation of explicit unsubscribe; publish checks on old-message edits;
active media revocation without interrupting unrelated calls; batched attention
refreshes; authenticated media objects with URL disposal; queued uploads and
no bubbling of file drops from a closed discussion into the main channel;
direct-message retry keys and scroll-aware read receipts; 15% overall reserve
in addition to category limits; inclusion of recording previews in cleanup bytes.
The final path review additionally rejects case variants of attachment IDs so
distinct SQLite keys cannot alias a file on a case-insensitive filesystem; the
cross-uploader HTTP regression checks the rejection before any file mutation.
Joint-storage initialization also retains a recording quota supplied by the
operator's environment; a regression verifies persisted and effective ceilings.

The frontend has dedicated regression tests for lost send responses, file retry,
cleanup-preview invalidation and permanent deleted-file placeholders. An isolated
fullstack browser scenario verifies a text discussion, image compression/preview,
cleanup/tombstone, role creation/preview, personal text/file exchange, inbox
navigation and blocking. Screenshots were inspected in the light theme; new
components use the existing theme tokens and responsive modal spacing.

Release gates: backend full tests/vet, Linux race detector and goroutine-leak gate;
frontend typecheck/lint, 271 unit tests, production build, 36 UI and 14 performance/
reliability browser scenarios, plus the new fullstack scenario. CI must also pass
on feature PRs and on the dev-to-main release PRs; no protected checks are bypassed.

Migration rollback/reapplication is tested through the real migration chain.
Backups include SQLite and media. Removing the last assigned custom role restores
the ordinary-member default; the UI and contract explain this. Cleanup previews
expire after five minutes and survive no server restart. Existing recordings
retain 24-hour retention. Publishing the release does not update a live server.
