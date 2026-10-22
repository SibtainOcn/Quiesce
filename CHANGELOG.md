# Changelog

All notable changes to Quiesce are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Direct run commands that skip the menu, e.g. `qc temp-1`, `qc ram`, `qc bin`, `qc deep`. Every step has one: `temp` (both temp folders), `temp-1`, `temp-2`, `prefetch`, `errors`, `delivery`, `update`, `logs`, `installer`, `dns`, `ram` (all 4 sub-options), `ram-1` to `ram-4`, `bin`, `deep`.
- `qc all` runs every step that is ON in the saved settings.
- Commands chain: `qc temp-1 deep bin` runs just those steps, in menu order.
- `qc --help` lists every command.
- From a normal terminal, a direct command asks for admin (UAC) and then runs in that same terminal, with no second window. P, Q and Ctrl+C still work. If it can't share the terminal, it opens its own window as before.
- Direct runs list only the chosen steps, both while running and in the summary. The item totals show only when a folder-cleaning step ran.

### Changed

- Commands are checked before elevation, so a typo never asks for Admin; unknown commands exit with code 2.
- Direct runs never change the saved settings. Exit code 1 means the run was aborted or the admin prompt was declined.
- A window opened just for a run waits for ENTER; a shared terminal returns to the prompt.

### Fixed

- The progress bar and CPU/RAM line no longer stay on screen above the summary once a run ends, in both output styles.

## [2.5.0] - 2026-10-22

### Added

- Live progress while cleaning, in two styles set by `OUTPUT_STYLE`: Minimal (default) step checklist with the current file, or Verbose file log with a pinned status line.
- Status shows step, progress bar, files, space freed, elapsed time and live CPU/RAM.
- `P` pauses/resumes and `Q` or Ctrl+C aborts safely; unrun steps show as `not run`, cut-short steps as partial.
- Closing the window mid-run restarts any stopped service and saves the run log.
- Optional run log (`KEEP_LOG`, off by default) in `logs\` next to `qc.exe`.

### Changed

- New coloured logo for the exe icon and README.
- Cleaning runs through a reusable task runner and shared file helpers; a crashing step is isolated and the run continues.
- New strings are English-only for now; Spanish shows them in English.

### Fixed

- Missing translations fall back to English instead of showing the raw key.
- A folder root that is a link or junction is no longer cleaned through.

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
