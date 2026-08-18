# Changelog

All notable changes to Quiesce are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.4.0] - 2026-08-19

### Added

- **Multi-language support.** The UI now follows the Windows display
  language, falling back to English for any language without a translation.
- **Spanish translation** (`locales/active.es.toml`), the first language
  besides English.
- **In-app language picker.** Press `L` on the settings screen for a numbered
  list of languages; the active one is ticked. Press the number to switch and
  `E` to save.
- **`LANGUAGE=` config override.** Add `LANGUAGE=en` or `LANGUAGE=es` to
  `cleaner_config.dat` to force a language regardless of Windows settings.
  The value is preserved when settings are saved.
- **UTF-8 console output**, so accented characters render correctly instead
  of as mojibake.
- **Test suite** covering translation completeness, format-verb consistency,
  menu alignment, config round-tripping, and language fallback.
- **CI on every push and pull request**: build and test on Go 1.23 and
  stable, `go vet`, `gofmt`, a `go mod tidy` check, and cross-builds for
  both `amd64` and `386`.

### Changed

- Menu and summary columns are now measured at runtime instead of relying on
  hand-typed trailing spaces, so the `ON`/`OFF` column stays aligned in any
  language.
- Translations live in `locales/*.toml` and are embedded into the binary.
  Adding a language needs no code change - see [docs/TRANSLATING.md].

### Fixed

- Saving settings no longer erases a `LANGUAGE=` line from the config file.
- The `[12] Deep Cleanup` summary row now lines up with every other row; it
  was one column off in English and further off in longer languages.

## [2.3.0] - 2026-08-16

### Changed

- Relicensed from QSAL v1.0 to GPL-3.0-or-later.

### Added

- Build identity baked into the binary: `qc --version` prints the version,
  author, repository, license and the executable's own SHA-256, without
  requiring Administrator.

## [2.2.0]

### Added

- Opt-in aggressive RAM reclaim: system file cache flush and working-set
  trimming, both OFF by default.

### Removed

- DISM from Deep Cleanup.

[docs/TRANSLATING.md]: docs/TRANSLATING.md
