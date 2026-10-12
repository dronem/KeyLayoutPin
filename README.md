# KeyLayoutPin

[![CI](https://github.com/dronem/keylayoutpin/actions/workflows/ci.yml/badge.svg)](https://github.com/dronem/keylayoutpin/actions/workflows/ci.yml)

Pins the keyboard layout **to each input field separately**, in any Windows program.

Switch the layout (Ctrl+Shift / Alt+Shift / Win+Space) to English in a terminal and to
Japanese in a chat box — from then on the layout switches by itself as you move between
those fields. Fields where the layout was never changed get the default layout from
Windows settings.

It works across Windows apps, not just one kind: classic desktop programs, browsers
(Chrome, Edge, Firefox), Electron apps (VS Code, Slack, Discord), Qt apps (Telegram) and
more. Where an app tells Windows which field has focus, every field gets its own layout;
where it doesn't (games, some custom-drawn apps), the layout is remembered for the whole
window. Any installed layouts and any number of them can be used.

## Install

Download the zip from [Releases](https://github.com/dronem/keylayoutpin/releases),
unpack it anywhere and run `keylayoutpin_bg.exe`. `SHA256SUMS.txt` next to the zip lets
you check the download: `Get-FileHash KeyLayoutPin-*.zip`.

Or with Go installed:

```powershell
go install -ldflags=-H=windowsgui github.com/dronem/keylayoutpin@latest
```

## Build and run

```powershell
.\build.ps1          # or .\build.ps1 -Test to run the tests first
.\keylayoutpin_bg.exe
```

`build.ps1` builds two versions:

- `keylayoutpin_bg.exe` — without a console window (`-ldflags -H=windowsgui`), for everyday
  use and autostart. Errors are logged to `%LOCALAPPDATA%\KeyLayoutPin\log.txt`
  (add `-log` for the full log);
- `keylayoutpin.exe` — console build for diagnostics: `-watch`, `-probe`, `-list`, `-v`.

Both are built with `-trimpath`, so no local paths end up in the binaries. The version
(`-version`, the first log line, the tray tooltip) is the git commit the build was made
from, e.g. `431a020 (2026-10-12)`, with `-dirty` for uncommitted changes. A release build
sets it explicitly: `.\build.ps1 -Version 1.0.0`.

If scripts are blocked by the execution policy:
`powershell -ExecutionPolicy Bypass -File build.ps1`.


## Tray and autostart

The app lives in the notification area (the blue "Aa" icon). Click it for the menu:

- **Start with Windows** — checked when the app starts at login. On the very first run
  autostart is turned on automatically; uncheck it to turn it off, and it stays off.
  The entry (`HKCU\Software\Microsoft\Windows\CurrentVersion\Run\KeyLayoutPin`) starts
  the GUI build — this exe if it is one, otherwise `keylayoutpin_bg.exe` next to it — so
  no console window opens at login. Only persistent flags are kept (`-db`, `-default`,
  `-chromium`, `-interval`, `-log`); `-v` and diagnostic flags never get there.
- **Exit**.

Only one copy runs at a time: a second start just shows a message and exits.

## Flags

| flag        | default                              | what it does |
|-------------|--------------------------------------|--------------|
| `-default`  | `system`                             | layout for new fields: `system` (from Windows settings), `keep` (leave as is) or a language (`en-US`, `ja`) |
| `-db`       | `%LOCALAPPDATA%\KeyLayoutPin\layouts.json` | where the memory is stored |
| `-interval` | `100ms`                              | how often to check the current layout (10ms to 5s) |
| `-log`      | `%LOCALAPPDATA%\KeyLayoutPin\log.txt` | log file. Without `-log` only errors (`ERROR …`) are written to the default file; with `-log` everything is: `-log` alone means the default file, `-log path` / `-log=path` another file. `-log=off` disables the file. `%VAR%` and `$env:VAR` are expanded, so the same command works from cmd, PowerShell and shortcuts. Diagnostic modes (`-watch`, `-probe`) write to a file only when `-log` is given explicitly |
| `-chromium` | `true`                               | enable accessibility in Chrome/Edge/VS Code etc. (`-chromium=false` disables it, but then fields inside pages can't be told apart) |
| `-v`        | —                                    | verbose log: every focus change with role and key |
| `-list`     | —                                    | list installed layouts and exit |
| `-reset`    | —                                    | forget everything and exit (refuses while the app is running, since it would write the memory back) |
| `-watch`    | —                                    | diagnostics: e.g. `-watch 3m` logs every focus event and layout change without switching anything |
| `-probe`    | —                                    | diagnostics: e.g. `-probe 30s` prints what has focus once a second |
| `-version`  | —                                    | print the version and exit |

A wrong flag stops the app with a message — on stderr, and in a message box for the
GUI build, which has no console.

## How it works

1. `SetWinEventHook(EVENT_OBJECT_FOCUS)` reports every focus change system-wide.
2. Via MSAA (`IAccessible`) the app learns the role and name of what received focus:
   - **input field** (text, editable combo box or document) — key
     `app.exe | window class | control class[#ID] | role | field name`.
     This tells fields apart even inside a single Chrome, Electron, WPF or Qt window.
     Digits in names become `#`, so "Terminal 2" and "Terminal 3" are one field;
   - **app without MSAA** (games, custom-drawn controls) — one key for the whole window;
   - **button, menu, list, tab, taskbar** — not a field: the layout is left alone
     and the last field stays current.

   Chrome, Edge, VS Code, Slack and other Chromium/Electron apps don't expose page
   fields by default. The app wakes each such window once (`WM_GETOBJECT` to
   `Chrome_RenderWidgetHostHWND`) and finds the focused element via `get_accFocus`.
3. On entering a field: if there is an entry for it, `WM_INPUTLANGCHANGEREQUEST` is sent
   to the focused window. Otherwise the default layout is applied.
4. A timer (every 100 ms) watches the foreground window's layout. Every change is one
   of three kinds ([internal/engine](internal/engine/engine.go)):
   - **ours**: the app requested it. If the request was for the previous field and
     landed in the new one, the right layout is re-applied;
   - **the user's**: within 1.5 s after Ctrl+Shift / Alt+Shift / Win+Space / `
     (keystrokes are caught by a keyboard hook on its own thread, [hotkey.go](hotkey.go)).
     It is remembered for the current field right away;
   - **foreign**: Windows restored the window's layout, the app changed it itself, etc.
     It is not remembered immediately, and during the first second after entering the
     field it is reverted.
5. On leaving a field, the layout it had is remembered. So on return you get "the one
   from last time", even if Ctrl+Shift was never pressed in that field.

Fields are resolved on a separate thread ([tracker.go](tracker.go), [resolver.go](resolver.go)). If an app hangs
and doesn't answer MSAA calls for more than 2 seconds, the thread is abandoned and a new
one is started, while layout switching keeps working. The log is written in the
background: selecting text in the console window (QuickEdit) doesn't stall the app.

The logic is covered by tests: `go test ./...`

### Code layout

| | |
|---|---|
| [internal/engine](internal/engine/engine.go) | when to switch and what to remember — no Windows dependencies |
| [internal/store](internal/store/store.go) | the memory file (hashing, size cap) — no Windows dependencies |
| [internal/kbd](internal/kbd/kbd.go) | the keyboard layout handle type |
| [field.go](field.go) | what has focus: MSAA calls in `detector`, the decisions in pure `classify` |
| [tracker.go](tracker.go), [resolver.go](resolver.go) | focus events → resolver threads (with the hung-app watchdog) → engine |
| [window.go](window.go), [tray.go](tray.go) | the hidden message window and the tray icon |
| [hotkey.go](hotkey.go), [autostart.go](autostart.go) | the keyboard hook, the Run key |
| [flags.go](flags.go), [logging.go](logging.go), [paths.go](paths.go) | command line, log, path expansion |
| [probe.go](probe.go) | `-probe` and `-watch` (the same tracker with an observer instead of the engine) |
| [main.go](main.go) | wiring |

No external dependencies — only the Go standard library and WinAPI.

## Privacy

The app sees which field of which program you are in, and the keyboard hook receives
every keystroke. So here is what it stores and what it doesn't:

- **Keystrokes are never stored or sent anywhere.** The hook only checks whether a
  layout switch hotkey is pressed (Ctrl/Alt+Shift, Win+Space, `) and records just the
  moment of the press ([hotkey.go](hotkey.go)). The app doesn't read what you type:
  it takes only a field's role and label, never its value.
- **Memory** (`%LOCALAPPDATA%\KeyLayoutPin\layouts.json`) holds only the salted SHA-256
  of the field key and the layout. No field labels, app names or timestamps. The salt
  is random per installation. Hashing doesn't stop someone who has the file from testing
  a specific guess ("was there field X in app Y?"), but they can't list your fields.
- The default layout is not stored: an entry exists only if a field has a different
  one. At most 1000 entries are kept; the oldest are dropped.
- Files live in `Local`, not `Roaming`, so they are not synced to servers with the user
  profile. Only your account has access.
- **The log** without `-v` contains only the app name and layout
  ("[chrome.exe] field: en-US → ja-JP"). With `-v`, and in `-watch` and `-probe` modes, the
  output includes field labels and window/page titles — that is personal data, enable it
  for diagnostics only. The log file (`-log`) is capped at 1 MB: on startup a large log
  is renamed to `.old` and the previous `.old` is deleted.
- In Chrome/Electron the app enables accessibility. After that, page content is available
  via MSAA to other programs running as you as well (although any such program could
  enable it itself). If you don't want that, use `-chromium=false`.
- The app doesn't use the network.

## Limitations

- Windows running **as administrator** don't accept messages from a normal process (UIPI).
  To cover them, run KeyLayoutPin as administrator.
- A field is identified by its name (label/placeholder). Fields with the same name in one
  window share one entry. If a field's name changes (e.g. "Message to John"), each variant
  gets its own memory.
- VS Code with `editor.accessibilitySupport: auto` may decide a screen reader is active and
  switch to screen reader mode. If that gets in the way, set
  `"editor.accessibilitySupport": "off"` in VS Code. Field detection still works.
- Only hotkey switches (Ctrl+Shift, Alt+Shift, Win+Space, `) are remembered immediately.
  A switch with the mouse via the taskbar language icon is remembered when you leave the field.
- Console windows (conhost, Windows Terminal) report the layout unreliably.
- Chrome/Electron turn on accessibility on the first MSAA access, so the first event in such
  a window may be recognized as the "window" rather than a specific field.

## License

[MIT](LICENSE)
