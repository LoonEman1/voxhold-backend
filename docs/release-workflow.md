# Shared Voxhold release workflow / Единый выпуск Voxhold

Both repositories follow the same flow:

1. Create a `codex/...` feature or fix branch from `dev`.
2. Open a PR into `dev`; all required checks must pass. Squash feature PRs.
3. Prepare the next release on `dev`: update `VERSION`, the frontend's
   `package.json`/lockfile, and `RELEASE_NOTES.md`. Frontend and backend share the
   same version starting with **2.0.0**.
4. Open `dev` → `main` release PRs in both repositories. Review the full release
   and require green checks. Use a merge commit for release PRs so `dev` remains
   in `main`'s ancestry and later releases contain only new work.
5. The successful main pipeline publishes immutable images, creates annotated
   `vX.Y.Z` tags and publishes GitHub Releases. No separate manual tag is needed.
   An unchanged/released `VERSION` never overwrites an existing release.
6. Deploy the backend and its migrations before the matching frontend. Verify
   health, database integrity and the application before removing old backups.
7. After a release, synchronize `main` back into `dev` through a reviewed PR.
   Use a merge commit for this maintenance PR too: strict branch checks must see
   the released main commit in dev's ancestry. This PR does not publish a release.

`main` is stable; `dev` collects reviewed changes and does not publish releases.
Both branches require PRs and successful CI. Force pushes and branch deletion are
disabled. Release tags must point to commits already merged into `main`.
Hotfixes start from `main`, bump the patch version and use reviewed PRs into
`main`; merge them back into `dev` before preparing the next release.

## Правила версий

Формат: **X.Y.Z**. Оба компонента получают одинаковый номер релиза, даже если
часть изменений относится только к одному из них.

- **X — мажорная версия**: крупное изменение продукта, архитектуры или
  несовместимое изменение API/миграций. Не увеличивается для обычных функций.
- **Y — минорная версия**: новые совместимые функции и заметные улучшения.
  При увеличении сбрасывается Z: `2.3.4` → `2.4.0`.
- **Z — исправления**: баги и небольшие безопасные доработки текущей минорной
  версии: `2.4.0` → `2.4.1`.

Версии `0.12.0` бэкенда и `1.14.0` фронтенда остаются историческими. Начиная
с `2.0.0` версии синхронизированы. Опубликованные теги не перемещаются; неудачный
старый тег также не переиспользуется.

Для выпуска: изменения проходят через `dev`, затем релизные PR в `main` с новым
`VERSION`. После зелёного CI образы, теги и релизы создаются автоматически.
Промежуточные слияния в `dev` ничего не публикуют.

После выпуска синхронизируйте `main` обратно в `dev` отдельным PR с merge commit.
Служебную синхронизацию не squash-мержить: это сохраняет историю релиза и позволяет
следующим PR проходить строгую проверку актуальности ветки.

## Runtime goroutine leak checks (backend)

CI runs the entire Go test suite with `VOXHOLD_CHECK_GOROUTINE_LEAKS=1`.
Each test package writes Go 1.27's `goroutineleak` profile after `TestMain` has
completed test cleanups. A nonempty leak profile fails CI and retains a stack
report. A subprocess fixture verifies that an intentionally blocked, unreachable
goroutine fails this gate. Ordinary test runs keep their normal behavior.

This complements `go vet` and `go test -race`. The GC-based detector cannot find
every leak, including some synchronization objects retained by global/runnable
state. See [Go 1.27 release notes](https://go.dev/doc/go1.27#runtime).
