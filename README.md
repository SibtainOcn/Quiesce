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

## Get started

1. Download **`qc-64bit.exe`** from the [latest release](https://github.com/SibtainOcn/Quiesce/releases/latest) (`qc-32bit.exe` for 32-bit Windows).
2. Rename it to `qc.exe` and put it in a folder that stays put, like `C:\Tools`.
3. Run it once and say **yes** to adding it to your PATH.

Now type `qc` in any terminal:

```bash
qc            # open the menu
qc all        # clean with your saved settings
qc temp ram   # just temp files and memory
qc -u         # update Quiesce
```

**[Full guide: every command, key and setting →](docs/USAGE.md)**

---

## What it cleans

| # | Step | What it removes | Default |
|---|---|---|---|
| 1 | Windows Temp | Temporary files Windows leaves behind | ON |
| 2 | User Temp | Temporary files your apps leave behind | ON |
| 3 | Prefetch | App launch cache | ON |
| 4 | Error Reports | Crash reports | ON |
| 5 | Delivery Optimization | Windows update sharing cache | ON |
| 6 | Windows Update Cache | Leftover update files | ON |
| 7 | Log Files | Windows activity logs | ON |
| 8 | Installer Temp | Leftover installer files | ON |
| 9 | DNS Cache | Saved website addresses, to refresh your connection | ON |
| 10 | RAM | Frees memory that is held but not in use | ON |
| 11 | Recycle Bin | Empties it | OFF |
| 12 | Deep Cleanup | Full Windows Disk Cleanup, then turns itself off | OFF |

Change any of these in the menu settings (`F`). Your choices are saved.

---

## Why Quiesce

- **Frees real memory** - asks Windows itself to release memory that is held but unused, and shows how much was freed.
- **Safe by default** - the Recycle Bin is left alone unless you turn it on, and Deep Cleanup switches itself off after one run. Windows services paused for cleaning are always started again, even if you close the window.
- **Fast from the terminal** - run any step directly (`qc temp ram`), chain them, and see live progress you can pause or stop.
- **One small file** - no installer. Updates itself with `qc -u`, and checks every download before using it.
- **Your language** - follows your Windows language. English and Spanish today; [adding one](docs/TRANSLATING.md) takes a single file.

---

## Guides

- **[Using Quiesce](docs/USAGE.md)** - commands, menu keys, settings, updating
- **[Translating](docs/TRANSLATING.md)** - add your language
- **[Developing](docs/DEVELOPING.md)** - build, test and release

---

## Requirements

Windows 10 or 11. Cleaning needs administrator rights; Quiesce asks when it needs them.

---

## License

[GPL-3.0-or-later](LICENSE) - free to use, share and change.
