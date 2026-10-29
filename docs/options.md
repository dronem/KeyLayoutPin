---
title: Command-line options
nav_order: 4
---

# Command-line options
{: .no_toc }

1. TOC
{:toc}

---

## Everyday options

| Option | Default | What it does |
|---|---|---|
| `-default` | `system` | layout for new fields: `system` (from Windows settings), `keep` (leave as is) or a language — `en-US`, `ja` |
| `-db` | `%LOCALAPPDATA%\KeyLayoutPin\layouts.json` | where the memory is stored |
| `-interval` | `100ms` | how often to check the current layout (10ms to 5s) |
| `-log` | `%LOCALAPPDATA%\KeyLayoutPin\log.txt` | the log file — see [Logging](#logging) |
| `-chromium` | `true` | let Chrome, Edge, VS Code and other Chromium/Electron apps tell apart fields inside pages; `-chromium=false` turns it off |

These are the lasting options: when you check **Start with Windows**, they are saved with
the autostart entry.

## Diagnostics and maintenance

| Option | What it does |
|---|---|
| `-v` | verbose log: every focus change with the field's name and key |
| `-watch 3m` | for the given time, log every focus change and layout change **without switching anything** |
| `-probe 30s` | for the given time, print once a second what has focus |
| `-list` | list the installed layouts and exit |
| `-reset` | forget all fields and exit (refuses while the app is running) |
| `-version` | print the version and exit |
| `-h` | print all options with their defaults |

Use them with the console build, `keylayoutpin.exe`, to see the output.
See [Troubleshooting](troubleshooting.md#diagnostics) for how to read it.

{: .warning }
> `-v`, `-watch` and `-probe` output field names and window titles — that can be personal
> data. Use them for diagnostics only.

## Logging

| How you start | What goes to the file |
|---|---|
| no `-log` | errors only (`ERROR …`), to `%LOCALAPPDATA%\KeyLayoutPin\log.txt` |
| `-log` | everything, to the default file |
| `-log app.txt` or `-log=C:\path\log.txt` | everything, to that file (a relative path is relative to the current folder) |
| `-log=off` | no file at all |

The console build also prints everything to the console. `-watch` and `-probe` write to a
file only when `-log` is given explicitly.

The file is capped at 1 MB: when it grows past that, it is renamed to `log.old.txt` (the
previous one is deleted) and a new one is started.

## Examples

Start with English as the layout for every new field:

```powershell
.\keylayoutpin_bg.exe -default en-US
```

Keep the memory next to the program (e.g. on a USB stick):

```powershell
.\keylayoutpin_bg.exe -db .\layouts.json
```

Write a detailed log into the current folder, for a bug report:

```powershell
.\keylayoutpin_bg.exe -v -log app.txt
```

Paths may use `%VAR%` and `$env:VAR` — both are expanded, so the same command works from
cmd, PowerShell and shortcuts.

A wrong option stops the app with a message (in a message box for `keylayoutpin_bg.exe`,
which has no console).
