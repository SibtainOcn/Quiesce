# Using Quiesce

Everything you can do with Quiesce, from the menu or by typing commands.

- [Install](#install)
- [The menu](#the-menu)
- [Commands](#commands)
- [Tools](#tools)
- [Updating](#updating)
- [Uninstalling](#uninstalling)
- [Settings](#settings)
- [Language](#language)
- [Is my copy official?](#is-my-copy-official)

---

## Install

**One line** - open PowerShell and paste:

```powershell
irm https://raw.githubusercontent.com/SibtainOcn/Quiesce/main/install.ps1 | iex
```

It picks the right file for your PC, downloads it from this repository's
releases, checks its SHA-256 and installs it. If the checksum doesn't match,
the file is deleted and nothing is installed.

**By hand:**

1. Download from the [latest release](https://github.com/SibtainOcn/Quiesce/releases/latest):
   - **`qc-64bit.exe`** for almost every PC
   - **`qc-32bit.exe`** only for 32-bit Windows
2. [Verify it](../README.md#verify-your-download).
3. Double-click it. Quiesce asks two questions:
   - **Check for updates automatically?** It looks once a day and tells you. It never installs on its own.
   - **Install Quiesce?** Say yes. It copies itself to
     `%LOCALAPPDATA%\Programs\Quiesce\qc.exe` and adds that folder to your PATH.
     You can then delete the download.

Then open a new terminal and type `qc`.

> Said no? Run `qc env` from the downloaded file any time.
>
> Already keep `qc.exe` in a folder of your own, like `C:\Tools`? Then
> `qc env` simply adds that folder to your PATH instead.

Quiesce needs administrator rights to clean. When it asks, approve the Windows
prompt (UAC).

---

## The menu

Type `qc` (or double-click `qc.exe`) to open the menu.

| Key | Does |
|---|---|
| `ENTER` | Clean, using your saved settings |
| `F` | Open settings |

While cleaning:

| Key | Does |
|---|---|
| `P` | Pause / resume |
| `Q` or `Ctrl+C` | Stop safely. Anything Quiesce paused is started again. |

---

## Commands

Skip the menu and clean straight from a terminal. Chain as many as you like.

```bash
qc all              # everything that is ON in your settings
qc temp ram         # temp files and memory
qc temp-1 deep bin  # any mix of steps
```

| Command | Cleans |
|---|---|
| `all` | Every step that is ON in your settings |
| `temp` | Both temp folders (`temp-1` Windows, `temp-2` yours) |
| `prefetch` | App launch cache |
| `errors` (or `wer`) | Crash reports |
| `delivery` | Windows update sharing cache |
| `win-update` (or `wu`) | Leftover Windows update files |
| `logs` | Windows log files |
| `installer` | Leftover installer files |
| `dns` | Saved website addresses (refreshes your connection) |
| `ram` | Frees memory, all 4 methods |
| `ram-1` ... `ram-4` | One memory method: 1 save pending changes, 2 free cached memory, 3 free file cache, 4 trim app memory |
| `bin` (or `recycle`) | Empties the Recycle Bin |
| `deep` | Full Windows Disk Cleanup |
| `no-ram` | Everything except RAM, Recycle Bin and Deep Cleanup (steps 1-9) |

Good to know:

- Commands only choose what runs this time. Your saved settings don't change.
- From a normal terminal, approve the admin prompt and the run shows in that
  same window.
- Steps always run in the menu's order, whatever order you type them.
- A typo is caught before anything runs: `qc --help` lists every command.

---

## Tools

| Command | Does |
|---|---|
| `qc -u` (or `qc update`) | Update Quiesce |
| `qc env` | Install Quiesce / add it to your PATH, so `qc` works in any terminal |
| `qc repo` (or `qc github`) | Open this project's page in your browser |
| `qc uninstall` | Remove Quiesce from this PC ([details](#uninstalling)) |
| `qc -v` | Show version and checksum |
| `qc -h` | Show every command, with examples |

---

## Updating

```bash
qc -u
```

Quiesce downloads the newest release, checks it is the exact file published
on GitHub, makes sure it starts, then replaces itself. If anything goes wrong -
no internet, a dropped connection, a file that doesn't match - your current
version stays as it is and you get a short message saying why.

With update checks on, `Update available ... run qc -u` appears at the top of
the menu when there is something new. Turn checks on or off with
**Auto update check** in settings.

---

## Uninstalling

```bash
qc uninstall
```

Answer `y` and Quiesce removes:

- `qc.exe` and the `%LOCALAPPDATA%\Programs\Quiesce` folder
- your settings (`cleaner_config.dat`), the update check file and run logs
- its folder from your user PATH. Nothing else on PATH is touched.

Any other Quiesce window that is open is closed first. A window running as
administrator can only be closed by you, or by running `qc uninstall` from an
admin terminal. No admin is needed otherwise.

`qc uninstall -y` skips the question, for scripts. Open a new terminal
afterwards: the one you used still has the old PATH.

If you keep `qc.exe` in a folder of your own, like `C:\Tools`, only Quiesce's
files are deleted. The folder stays, and it leaves your PATH only if nothing
else is in it.

---

## Settings

Press `F` in the menu.

| Key | Does |
|---|---|
| `W` / `S` | Move up / down |
| `D` / `A` | Turn ON / OFF |
| `L` | Choose language |
| `E` | Save and go back |

Besides the 12 cleaning steps:

- **Output style** - *Minimal* (default) shows a short checklist. *Verbose*
  lists every file removed.
- **Save a log file of each run** - keeps a full record in a `logs` folder next
  to `qc.exe`. Off by default.
- **Auto update check** - look for new versions once a day.

Settings live in `cleaner_config.dat` next to `qc.exe`.

---

## Language

Quiesce follows your Windows language and falls back to English. To pick one
yourself, press `F`, then `L`, choose a number and press `E`.

Available: English, Español. Adding a language takes one file - see
[TRANSLATING.md](TRANSLATING.md).

---

## Is my copy official?

```bash
qc -v
```

This shows the version and a checksum (SHA-256) of your `qc.exe`, without
asking for admin. Compare the checksum with the one on the
[releases page](https://github.com/SibtainOcn/Quiesce/releases). If they
differ, your copy is not an official build.
