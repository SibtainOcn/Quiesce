# Developing Quiesce

## Build

Needs Go 1.23 or newer, on Windows.

```bash
go build -o qc.exe
```

The commit is stamped in automatically. To set the version yourself:

```bash
go build -trimpath -ldflags "-s -w -X main.Version=2.5.1" -o qc.exe
```

## Test

```bash
go vet ./...
go test ./...
```

Every push and pull request runs the same checks on GitHub
([ci.yml](../.github/workflows/ci.yml)).

## Release

Releases are built and published by GitHub Actions
([release.yml](../.github/workflows/release.yml)) when a version tag is pushed.

1. Set `Version` in `version.go`.
2. Add a `## [2.5.1]` section to `CHANGELOG.md` - it becomes the release notes.
3. Commit, then tag and push:

```bash
git tag v2.5.1
git push origin v2.5.1
```

The workflow tests the code, builds `qc-64bit.exe` and `qc-32bit.exe`, checks
that they start, and publishes them with `SHA256SUMS.txt`. A tag such as
`v2.5.1-rc1` becomes a pre-release, which `qc -u` ignores.

`qc -u` finds the right file by these names, so keep them exactly as they are.

## Translations

See [TRANSLATING.md](TRANSLATING.md).
