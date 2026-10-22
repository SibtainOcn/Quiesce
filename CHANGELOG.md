# Changelog

All notable changes to Quiesce are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Direct run commands that skip the menu, e.g. `qc temp-1`, `qc ram`, `qc bin`, `qc deep`. Every step has one: `temp` (both temp folders), `temp-1`, `temp-2`, `prefetch`, `errors`, `delivery`, `win-update` (or `wu`), `logs`, `installer`, `dns`, `ram` (all 4 sub-options), `ram-1` to `ram-4`, `bin`, `deep`.
- `qc all` runs every step that is ON in the saved settings.
- Commands chain: `qc temp-1 deep bin` runs just those steps, in menu order.
- `qc --help` lists every command.
- From a normal terminal, a direct command asks for admin (UAC) and then runs in that same terminal, with no second window. P, Q and Ctrl+C still work. If it can't share the terminal, it opens its own window as before.
- Direct runs list only the chosen steps, both while running and in the summary, numbered 1, 2, 3... The item totals show only when a folder-cleaning step ran.
- Self-update: `qc -u` (or `qc update`) downloads the latest release, checks it against the SHA-256 GitHub publishes, test-runs it, then swaps it in, rolling back if any step fails. Steps show with the same spinner as cleaning, followed by the release notes. Admin is asked for only when the `qc.exe` folder needs it.
- Daily update check: at most once a day, in the background, Quiesce checks for a new release and shows `Update available ... run qc -u` at the top of the menu and above direct-run summaries. Nothing installs on its own.
- Clear messages when updating fails: no internet, timeout, dropped download, GitHub rate limit or outage, blocked secure connection, full disk. Brief GitHub errors are retried, a stalled download stops after 30 seconds, and the current version is always left in place.
- When GitHub's API rate limit is hit, `qc -u` falls back to the release page and its `SHA256SUMS.txt`; the daily check never uses the API.
- Releases are built and published by GitHub Actions when a `v*` tag is pushed: tests, `qc-64bit.exe` and `qc-32bit.exe` with the version stamped in, `SHA256SUMS.txt`, and release notes from this file.
- `qc env` adds the folder holding `qc.exe` to the user PATH (no admin needed), or says it is already there; new terminals pick it up at once. First run offers it too, with example commands. A temporary folder is refused, and another `qc.exe` earlier on PATH is pointed out.
- `qc repo` (or `qc github`) opens the Quiesce GitHub page in the default browser.
- `qc --help` is grouped into cleaning commands, tools and examples.
- Updates use the proxy set in Windows' Internet Options when `HTTPS_PROXY` is not set.
- First run asks whether to turn the update check on (`Y` by default). Change it later with **Auto update check** in settings (`AUTO_UPDATE` in `cleaner_config.dat`).

### Changed

- Commands are checked before elevation, so a typo never asks for Admin; unknown commands exit with code 2.
- Direct runs never change the saved settings. Exit code 1 means the run was aborted or the admin prompt was declined.
- A window opened just for a run waits for ENTER; a shared terminal returns to the prompt.
- The end-of-run summary no longer prints the GitHub URL (`qc --version` still shows it).

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
