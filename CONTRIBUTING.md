English | [Русский](CONTRIBUTING.ru.md)

# Contributing to Voxhold

Both repositories use the shared [release workflow](docs/release-workflow.md).
Create feature/fix branches from `dev`, open a PR into `dev`, and wait for every
required check before squash-merging. Never push product changes directly to
`main` or `dev`. Both branches are protected; do not bypass failed checks.

Release PRs go from `dev` into `main` and use merge commits to preserve ancestry.
Update `VERSION`, frontend package metadata and `RELEASE_NOTES.md` together.
From 2.0.0, frontend and backend releases share one major.minor.patch version.
The successful main workflow publishes images, an annotated tag and the release.
Already published versions/tags are immutable. Manual release tags must match
`VERSION` and identify a reviewed main commit.

Backend checks: `go test ./...`, `go vet ./...`, `go test -race ./...`, and
`VOXHOLD_CHECK_GOROUTINE_LEAKS=1 go test -count=1 ./...` on Go 1.27+.
Frontend checks: `npm run typecheck`, `npm run lint`, `npm test`, `npm run build`,
and the UI/performance browser suites. Add regression coverage for behavior.

Keep changes focused, document public behavior in English and Russian, and do
not commit credentials, databases, private logs or user data. Report security
vulnerabilities privately as described in `SECURITY.md` where available.

Deploy backend migrations before the matching frontend, retain a verified
backup, and check health/data integrity before deleting older backups.
