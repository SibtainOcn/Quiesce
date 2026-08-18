# Quiesce

<img align="right" src="winres/icon.png" width="85" alt="Quiesce logo">

A fast, lightweight Windows system cleaner and RAM optimizer. Single executable, zero installation, no heavy GUI framework — just a clean terminal interface with 12 customizable cleaning steps to free up disk space and speed up your PC safely.

---

<p align="center">
  <img src="docs/ss.png" alt="Quiesce main menu" width="580">
</p>

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
- **Single File & Portable**: No installer required. Just run `qc.exe` anywhere.
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

Settings Controls:
  W / S    Move selection Up / Down
  D / A    Toggle step ON / OFF
  L        Choose language
  E        Save & return to main menu
```

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
go build -ldflags "-X main.Version=2.4.0" -o qc.exe
```

---

## Requirements

- **OS**: Windows 10 or 11 (64-bit)
- **Privileges**: Administrator (will prompt via UAC when you run a clean)

### Download

**[⬇ Download qc.exe](https://github.com/SibtainOcn/Quiesce/releases/latest/download/qc.exe)** — always the latest release.

Put it anywhere on your `PATH` (e.g. `C:\Users\<you>\bin`) to run `qc` from any terminal.

---

## Support & License

- **License**: [GNU General Public License v3.0 or later](LICENSE) (`GPL-3.0-or-later`)

  Quiesce is free software. You may use, study, share, and modify it. If you
  distribute a modified version, it must also be free software under the GPL,
  with source available — so every user of a fork keeps the same freedoms you
  have here. This matters for a tool that runs as Administrator: nobody should
  have to trust a version of it they cannot read.

  Quiesce comes with ABSOLUTELY NO WARRANTY. See [LICENSE](LICENSE) for the
  full terms.
- **Support**: If Quiesce saved your pc from clutter free junks, optimize and boost performance, feel free to support the project:

<a href="https://buymeacoffee.com/sibtainocn"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" height="50"></a>
