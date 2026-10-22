# Quiesce
[![Go Version](https://img.shields.io/github/go-mod/go-version/SibtainOcn/Quiesce?color=00ADD8&logo=go&logoColor=white)](https://github.com/SibtainOcn/Quiesce/blob/main/go.mod)
[![License: GPL v3+](https://img.shields.io/badge/License-GPLv3%2B-blue.svg)](https://github.com/SibtainOcn/Quiesce/blob/main/LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0078D6?logo=windows&logoColor=white)](https://github.com/SibtainOcn/Quiesce)
[![Latest Release](https://img.shields.io/github/v/release/SibtainOcn/Quiesce?color=success&label=latest)](https://github.com/SibtainOcn/Quiesce/releases/latest)
[![Total Downloads](https://img.shields.io/github/downloads/SibtainOcn/Quiesce/total?color=brightgreen)](https://github.com/SibtainOcn/Quiesce/releases)
[![Stars](https://img.shields.io/github/stars/SibtainOcn/Quiesce?style=social)](https://github.com/SibtainOcn/Quiesce/stargazers)
[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-FFDD00?style=flat&logo=buy-me-a-coffee&logoColor=black)](https://buymeacoffee.com/sibtainocean)
<img align="right" src="assets/quiesce-logo.png" width="85" alt="Quiesce logo">

A fast, lightweight Windows system cleaner and RAM optimizer. Single executable, zero installation, no heavy GUI framework, just a clean terminal interface with 12 customizable cleaning steps to free up disk space and speed up your PC safely.

---


## What It Cleans

| # | Cleaning Step | Description | Default |
|---|---|---|---|
| **1** | Windows Temp | System temporary files created by Windows | **ON** |
| **2** | User Temp | Temporary files created by applications you run | **ON** |
| **3** | Prefetch | Windows application launch cache | **ON** |
| **4** | Windows Error Reports | Crash dumps and error log files | **ON** |
| **5** | Delivery Optimization Cache | Downloaded Windows update distribution files | **ON** |
| **6** | Windows Update Cache | Leftover update installation files | **ON** |
| **7** | Windows Log Files | System and application activity logs | **ON** |
| **8** | Installer Cache | Temporary Windows Installer patch cache files | **ON** |
| **9** | DNS Resolver Cache | Flushes cached DNS records to refresh connection | **ON** |
| **10** | RAM Optimization | Clears standby memory & dirty page lists | **ON** |
| | ├ Flush modified list | Writes dirty pages to disk so they can be freed | **ON** |
| | ├ Purge standby list | Frees the cached/standby page list | **ON** |
| | ├ System file cache | Drops the kernel's cached file data | **OFF** (Opt-in) |
| | └ Trim working sets | Pages out live app memory — frees the most, but apps re-read it from disk afterwards | **OFF** (Opt-in) |
| **11** | Recycle Bin | Empties the Windows Recycle Bin | **OFF** (Opt-in) |
| **12** | Deep Cleanup | Full Windows Disk Cleanup (cleanmgr, all handlers) | **OFF** (Resets after run) |

Every step can be customized in the menu settings, and your choices are saved automatically.

---

## Key Features

- **Genuine RAM Optimization**: Unlike fake memory cleaners that just force apps into page files, Quiesce directly asks the Windows kernel to flush modified memory and purge standby RAM list. It reports the real memory freed.
- **Smart Service Handling**: Background Windows services (like Windows Update & Delivery Optimization) are safely stopped before cleaning their cache files and automatically restarted afterward.
- **Safe by Default**: Risky options like emptying the Recycle Bin are disabled by default. Deep system cleanup automatically resets back to OFF after every run so it cannot repeat accidentally.
- **Single File & Portable**: No installer required. Just run `qc.exe` anywhere; `qc env` puts it on your PATH.
- **Command Line Too**: Run any step directly (`qc temp ram`, `qc all`), update in place with `qc -u`, verified by SHA-256.
- **Speaks Your Language**: The interface follows your Windows display language, falling back to English. English and Spanish ship today; adding a language takes one file and no code.

---

## Language

Quiesce reads your Windows display language at startup. To change it, press
**F** for settings, then **L**, pick a language by number, and press **E** to
save. Your choice is remembered.

You can also set it by hand in `cleaner_config.dat` (next to `qc.exe`):

```text
LANGUAGE=es
```

| Code | Language |
|---|---|
| `en` | English |
| `es` | Español |

Want your language? It takes one file, no Go code — see
[docs/TRANSLATING.md](docs/TRANSLATING.md). Pull requests welcome.

---

## How to Use

Launch **`qc.exe`** as Administrator:

```text
  ENTER    Run cleaner with current settings
  F        Open settings menu

While Cleaning:
  P        Pause / resume
  Q        Abort (stops safely, restarts any stopped services)
  Ctrl+C   Same as Q

Settings Controls:
  W / S    Move selection Up / Down
  D / A    Toggle step ON / OFF
  L        Choose language
  E        Save & return to main menu
```

### Direct commands

Skip the menu and run steps straight from a terminal:

```bash
qc all              # every step ON in your saved settings
qc temp             # Windows Temp + User Temp
qc temp-1 deep bin  # chain any steps together
```

| Command | Step |
|---|---|
| `temp-1`, `temp-2`, `temp` | Windows Temp, User Temp, both |
| `prefetch` | Prefetch folder |
| `errors` (`wer`) | Windows Error Reports |
| `delivery` | Delivery Optimization |
| `win-update` (`wu`) | Windows Update Cache |
| `logs` | Windows Log Files |
| `installer` | Windows Installer Temp |
| `dns` | DNS Cache Flush |
| `ram` | RAM Optimization, all 4 sub-options |
| `ram-1` … `ram-4` | Flush modified, purge standby, file cache, trim working sets |
| `bin` (`recycle`) | Empty Recycle Bin |
| `deep` | Deep Cleanup |

Commands only pick what runs this time; saved settings are never changed.
Output style and the log file option still apply. Only the steps you chose
are listed while it runs. `qc --help` shows the list.

From a normal (non-admin) terminal, approve the UAC prompt and the run shows
in that same terminal - no second window. `P`, `Q` and `Ctrl+C` work as usual.
If the run can't share your terminal (for example, an admin account other than
yours was used at the UAC prompt), it opens its own window instead.

Exit codes: `0` done, `1` aborted or admin declined, `2` unknown command.

### Tools

| Command | What it does |
|---|---|
| `qc -u` (`qc update`) | Update Quiesce to the latest release - see [Updating](#updating) |
| `qc env` | Add Quiesce to your PATH, so `qc` works in any terminal |
| `qc repo` (`qc github`) | Open the Quiesce page on GitHub in your browser |
| `qc -v` (`--version`) | Version, author, license and the exe's own SHA-256 |
| `qc -h` (`--help`) | Every command, with examples |

### Run `qc` from anywhere

On first run Quiesce offers to add itself to your PATH. You can also do it any
time:

```bash
qc env
```

It adds the folder holding `qc.exe` to your **user** PATH (no admin needed),
or says so if it is already there. Open a new terminal afterwards, then:

```bash
qc            # open the menu
qc temp ram   # clean temp files and free RAM
qc -u         # update Quiesce
```

Keep `qc.exe` in a permanent folder such as `C:\Tools` first: a temporary
folder is refused, since cleaning empties it. Until then, PowerShell needs
`.\qc` inside the Quiesce folder, as it doesn't run programs from the current
folder by name.

Pick how progress is shown with **Output style** in settings:

- **Minimal** (default): a checklist of steps that ticks over with each step's
  file count and time, plus the file being removed right now.
- **Verbose**: every removed file is listed, with a status line pinned at the
  bottom (step, progress, files, space freed, time, live CPU/RAM and the
  pause/abort keys).

To keep the full list of what was removed, switch on **Save a log file of each
run**: each run is written to a `logs` folder next to `qc.exe` (off by
default). Closing the window mid-run still restarts any Windows service that
was stopped for cleaning.

---

## Updating

```bash
qc -u        # or: qc update
```

Quiesce checks the latest [GitHub release](https://github.com/SibtainOcn/Quiesce/releases),
downloads it, verifies it against the SHA-256 checksum GitHub publishes,
test-runs the new copy, then swaps it in. If any step fails, your current
version stays as it was. It asks for admin only when the folder holding
`qc.exe` needs it.

On first run Quiesce asks whether to check for updates automatically. When
on, it looks for a new release at most once a day, in the background, and
shows `Update available ... run qc -u` at the top of the menu. It never
installs anything on its own. Toggle it with **Auto update check** in
settings, or `AUTO_UPDATE=0` / `1` in `cleaner_config.dat`.

No internet, a dropped Wi-Fi link or a slow GitHub each give a plain message
and leave your copy untouched; the daily check just tries again later.
GitHub's API allows 60 requests an hour per network, which shared networks
can use up. The daily check doesn't use the API at all, and `qc -u` falls back
to the release page and its `SHA256SUMS.txt` when the API refuses. Nothing is
installed without a checksum match.

### Publishing a release

Releases are built by GitHub Actions ([release.yml](.github/workflows/release.yml))
when a version tag is pushed:

1. Set `Version` in `version.go` and add a `## [x.y.z]` section to `CHANGELOG.md`.
2. Commit, then tag and push:

```bash
git tag v2.6.0
git push origin v2.6.0
```

The workflow tests the code, builds `qc-64bit.exe` and `qc-32bit.exe` with the
version stamped in, checks that the exe runs, and publishes them with
`SHA256SUMS.txt` and the CHANGELOG section as release notes. A tag such as
`v2.6.0-rc1` becomes a pre-release, which `qc -u` skips.

---

## Verifying Your Copy

Quiesce runs with Administrator rights, so it's worth confirming the binary you
have is the official one. Every build carries its own identity:

```bash
qc --version
```

This prints the version, author, repository, license and the executable's own
SHA-256 — and it does **not** require Administrator. Compare that hash against
the checksum published with the [official release](https://github.com/SibtainOcn/Quiesce/releases).
A mismatch means the binary is not an official build.

---

## Building from Source

Requires Go 1.23+ on Windows:

```bash
go build -o qc.exe
```

The commit hash and build time are stamped in automatically from git. To set an
explicit version string:

```bash
go build -trimpath -ldflags "-s -w -X main.Version=2.5.0" -o qc.exe
```

---

## Requirements

- **OS**: Windows 10 or 11 (64-bit)
- **Privileges**: Administrator (will prompt via UAC when you run a clean)

### Download

From the [latest release](https://github.com/SibtainOcn/Quiesce/releases/latest):

- **`qc-64bit.exe`** - almost every Windows PC
- **`qc-32bit.exe`** - 32-bit Windows only

Save it as `qc.exe` in a permanent folder (e.g. `C:\Tools`), run it once and
answer **Y** to "Add Quiesce to your PATH", or run `qc env` later. Updates
keep the `qc.exe` name.

---

## License

- **License**: [GPL-3.0-or-later](LICENSE)
