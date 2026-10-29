---
title: Getting started
nav_order: 2
---

# Getting started
{: .no_toc }

1. TOC
{:toc}

---

## Install

Download `KeyLayoutPin-vX.Y.Z-windows-amd64.zip` from
[Releases](https://github.com/dronem/keylayoutpin/releases) and unpack it anywhere, for
example into `%LOCALAPPDATA%\Programs\KeyLayoutPin`. `SHA256SUMS.txt` next to the zip lets
you check the download:

```powershell
Get-FileHash KeyLayoutPin-*.zip
```

The zip has two programs:

| | |
|---|---|
| `keylayoutpin_bg.exe` | the everyday one: no console window. Start this one. |
| `keylayoutpin.exe` | the same app with a console, for [diagnostics](troubleshooting.md#diagnostics) |

Or, with Go installed:

```powershell
go install -ldflags=-H=windowsgui github.com/dronem/keylayoutpin@latest
```

## First run

Run `keylayoutpin_bg.exe`. A blue **Aa** icon appears in the notification area — the app is
working. On the very first run it also turns on starting with Windows.

Now just work as usual. In each field, switch the layout once with your usual hotkey
(Ctrl+Shift, Alt+Shift or Win+Space); the field keeps it from then on. Fields you never
switched get the default layout from Windows settings.

Only one copy runs at a time: starting it again just shows a message.

## The tray menu

Click the **Aa** icon (left or right button):

| Item | What it does |
|---|---|
| **KeyLayoutPin** | the title; opens About |
| **Start with Windows** | checked when the app starts at login — click to toggle |
| **About** | a short description, the version and the project link |
| **Help** | the main principles and all command-line options, with a link to these pages |
| **Exit** | stops the app |

## Starting with Windows

On the first run autostart is turned on automatically. Uncheck **Start with Windows** to
turn it off — it stays off.

The entry lives in `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\KeyLayoutPin`. It
always starts the console-less `keylayoutpin_bg.exe`, so no window flashes at login. The
[options](options.md) you started the app with are kept there too — but only the lasting
ones (`-db`, `-default`, `-chromium`, `-interval`, `-log`); `-v` and the diagnostic flags
never get there.

## Updating

Exit the app from the tray menu, replace both exe files with the new ones and start
`keylayoutpin_bg.exe` again. The memory of your fields is kept: it lives separately, in
`%LOCALAPPDATA%\KeyLayoutPin`.

## Removing

1. Uncheck **Start with Windows** in the tray menu, then choose **Exit**.
2. Delete the folder with the exe files.
3. Delete `%LOCALAPPDATA%\KeyLayoutPin` — the memory and the log.

Nothing else is left on the system.
