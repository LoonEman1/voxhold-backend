English | [Русский](CONTRIBUTING.ru.md)

# Contributing to Voxhold backend

All changes, including maintainer changes, follow the same workflow:

```text
issue or task → dedicated branch → pull request → successful CI → merge
```

Do not develop directly on `main` and do not merge a pull request while any
required check is pending, failing or cancelled.

## Development workflow

1. Update local `main` and create a dedicated branch:

   ```bash
   git switch main
   git pull --ff-only
   git switch -c feat/short-description
   ```

2. Make one focused change. Add or update tests and documentation where the
   behavior or operator workflow changes.
3. Run the local checks:

   ```bash
   go test ./...
   go vet ./...
   ```

4. Commit and push the branch, then open a pull request into `main`.
5. Wait for every required GitHub Actions check to succeed. Fix failures on the
   same branch and wait for the new run.
6. Review the final diff and merge only after CI is green. Prefer squash merge
   for a focused pull request, then delete the merged branch.

Recommended branch prefixes are `feat/`, `fix/`, `docs/`, `refactor/`,
`test/`, `chore/` and `security/`.

## Pull request checklist

- the change has a clear purpose and contains no unrelated edits;
- tests cover new or corrected behavior where practical;
- both `go test ./...` and `go vet ./...` pass;
- public behavior, configuration and deployment changes are documented in
  English and Russian;
- no credentials, `.env` files, databases, logs with secrets or private user
  data are committed;
- the PR targets `main` and is not merged before CI succeeds.

Security vulnerabilities must be reported through [SECURITY.md](SECURITY.md),
not through a public pull request before coordinated disclosure.

## Releases

Voxhold backend uses Semantic Versioning tags:

- `v0.1.1` for a backward-compatible fix;
- `v0.2.0` for a backward-compatible feature during the `0.x` phase;
- `v1.0.0` for the first stable API contract;
- `v0.2.0-rc.1` for a pre-release.

A release tag must point to a reviewed commit already merged into `main`. Never
move or reuse a published tag. Create an annotated tag and push it:

```bash
git switch main
git pull --ff-only
git tag -a v0.1.1 -m "Voxhold backend v0.1.1"
git push origin v0.1.1
```

The tag workflow runs tests first, publishes the multi-platform GHCR image and
then creates a GitHub Release with generated notes. Stable `v0.1.1` produces
image tags `0.1.1`, `0.1` and `latest`; a pre-release does not replace the
stable `latest` image.

Before changing a deployed backend version, review the release notes, create a
backup and use the matching exact version:

```bash
./backup.sh
./update.sh --backend-version 0.1.1
```
