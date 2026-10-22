# Changelog

All notable changes to Quiesce are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Live status while cleaning.** The classic file log now has a status line
  pinned at the bottom: spinner, step, progress bar, files, space freed,
  elapsed time, live CPU/RAM and the pause/abort keys. Falls back to plain
  output where the console can't draw it.
- **Output style setting** (`OUTPUT_STYLE`): Verbose (default) lists every
  file as before; Minimal shows only a checklist of steps with file counts
  and times.
- **Pause and abort.** `P` pauses or resumes; `Q` aborts after the current
  item. Services stopped for a step are always restarted, and the summary
  marks steps that never ran as `not run`.
- **Safe close.** Closing the window, Ctrl+C, logoff or shutdown mid-run
  restarts any service stopped for cleaning and saves the run log. Ctrl+C
  during a run acts like Q.
- **Space freed** is now measured and shown, not just item counts.
- **Optional run log** (`KEEP_LOG`, off by default): writes each run to
  `logs\quiesce_<date>_<time>.log` next to `qc.exe`.

### Changed

- **New coloured logo** replaces the old black-and-white one, in the exe icon
  and the README. Source in `assets/quiesce-logo-source.png`;
  `tools/make-icons.py` crops it, removes the black background and writes
  `assets/quiesce-logo.png` and `winres/icon/<size>.png` (16-256 px), then
  `go-winres make --arch amd64,386` rebuilds the `.syso` resources.
- Cleaning steps now run through a reusable task runner (`tasks.go`), live
  screen (`liveview.go`) and shared file-deletion helpers (`fileops.go`), so
  future features such as browser/app cache cleaning reuse the same screen.
- A step that crashes is now stopped and marked failed, and the run carries
  on with the next step instead of ending.
- New strings are English-only for now; Spanish shows them in English.

### Fixed

- Text missing from a translation now falls back to English. It used to
  show the raw key (e.g. `live.keys_run`), because the translation library
  reports a fallback as an error alongside the English text.

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
- The console window title now shows the version, e.g. `Quiesce v2.4.0`.
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

- **Clicking the console window no longer freezes Quiesce.** Windows enables
  QuickEdit by default, so a click put the console into selection mode, which
  blocks every write until Enter or Esc is pressed - the app appeared to hang
  and could not print an explanation, because printing was what was blocked.
  QuickEdit is now switched off at startup and restored on exit, so a
  terminal you already had open is left as you found it. Mouse drag-select is
  unavailable while Quiesce runs; right-click → Mark still works.
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
