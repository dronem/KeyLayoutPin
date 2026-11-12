---
title: Development
nav_order: 7
---

# Development
{: .no_toc }

1. TOC
{:toc}

---

## Build

Requires Go (see `go.mod` for the version). No external dependencies — only the Go standard
library and WinAPI.

```powershell
.\build.ps1          # or .\build.ps1 -Test to run the tests first
.\keylayoutpin_bg.exe
```

`build.ps1` builds two versions:

- `keylayoutpin_bg.exe` — without a console window (`-ldflags -H=windowsgui`), for everyday
  use and autostart;
- `keylayoutpin.exe` — the console build for diagnostics.

Both are built with `-trimpath`, so no local paths end up in the binaries. The version
(`-version`, the first log line, the tray tooltip, About) is the git commit the build was
made from, e.g. `431a020 (2026-10-12)`, with `-dirty` for uncommitted changes. A release
build sets it explicitly: `.\build.ps1 -Version 1.0.0`.

If scripts are blocked by the execution policy:
`powershell -ExecutionPolicy Bypass -File build.ps1`.

Tests: `go test ./...`

## Internals

1. `SetWinEventHook(EVENT_OBJECT_FOCUS)` reports every focus change system-wide.
2. Via MSAA (`IAccessible`) the app learns the role and name of what received focus:
   - **input field** (text, editable combo box or document) — key
     `app.exe | window class | control class[#ID] | role | field name`.
     Digits in names become `#`; VS Code terminal names are cut to `Terminal #`;
   - **app without MSAA** — one key for the whole window;
   - **button, menu, list, tab, taskbar** — not a field.

   Chromium/Electron apps don't expose page fields by default. The app wakes each such
   window (`WM_GETOBJECT` to `Chrome_RenderWidgetHostHWND`) and finds the focused
   element via `get_accFocus`. Chromium turns accessibility off again after a while, so
   when only the whole page is seen in focus, the window is woken again (at most every
   10 s) and the focus is looked up a few more times, 100 ms apart. It goes off
   after ~30 s of user input with no accessibility calls; then focus moves *inside* the
   window send no events at all. So while a Chromium window is in front and nothing asked
   it for 10 s, the resolver pings it (wake + `get_accFocus`); the answer is dropped.
3. On entering a field, `WM_INPUTLANGCHANGEREQUEST` with the remembered (or default) layout
   is sent to the focused window.
4. A timer (every 100 ms) watches the foreground window's layout. Every change is one of
   three kinds:
   - **ours**: the app requested it. If the request was for the previous field and landed
     in the new one, the right layout is re-applied;
   - **the user's**: within 1.5 s after a layout hotkey (caught by a low-level keyboard
     hook on its own thread). It is remembered right away;
   - **foreign**: Windows or the app changed it. Not remembered immediately, and reverted
     during the first second after entering the field.
5. On leaving a field, the layout it had is remembered.

Fields are resolved on a separate thread. If an app doesn't answer MSAA calls for more than
2 seconds, the thread is abandoned and a new one started, while layout switching keeps
working. The log is written in the background, so selecting text in the console window
(QuickEdit) doesn't stall the app.

## Code layout

| | |
|---|---|
| [internal/engine](https://github.com/dronem/keylayoutpin/tree/main/internal/engine) | when to switch and what to remember — no Windows dependencies |
| [internal/store](https://github.com/dronem/keylayoutpin/tree/main/internal/store) | the memory file (hashing, size cap) — no Windows dependencies |
| [internal/kbd](https://github.com/dronem/keylayoutpin/tree/main/internal/kbd) | the keyboard layout handle type |
| [field.go](https://github.com/dronem/keylayoutpin/blob/main/field.go) | what has focus: MSAA calls in `detector`, the decisions in pure `classify` |
| [tracker.go](https://github.com/dronem/keylayoutpin/blob/main/tracker.go), [resolver.go](https://github.com/dronem/keylayoutpin/blob/main/resolver.go) | focus events → resolver threads (with the hung-app watchdog) → engine |
| [window.go](https://github.com/dronem/keylayoutpin/blob/main/window.go), [tray.go](https://github.com/dronem/keylayoutpin/blob/main/tray.go), [about.go](https://github.com/dronem/keylayoutpin/blob/main/about.go) | the hidden message window, the tray icon and its About/Help texts |
| [settings.go](https://github.com/dronem/keylayoutpin/blob/main/settings.go), [settingsui.go](https://github.com/dronem/keylayoutpin/blob/main/settingsui.go) | `settings.json` and the Settings window (plain Win32; common controls 6 via a runtime activation context, per-monitor DPI for that window only) |
| [hotkey.go](https://github.com/dronem/keylayoutpin/blob/main/hotkey.go), [autostart.go](https://github.com/dronem/keylayoutpin/blob/main/autostart.go) | the keyboard hook, the Run key |
| [flags.go](https://github.com/dronem/keylayoutpin/blob/main/flags.go), [logging.go](https://github.com/dronem/keylayoutpin/blob/main/logging.go), [paths.go](https://github.com/dronem/keylayoutpin/blob/main/paths.go) | command line, log, path expansion |
| [probe.go](https://github.com/dronem/keylayoutpin/blob/main/probe.go) | `-probe` and `-watch` (the same tracker with an observer instead of the engine) |
| [main.go](https://github.com/dronem/keylayoutpin/blob/main/main.go) | wiring |

## This documentation

These pages live in [`docs/`](https://github.com/dronem/keylayoutpin/tree/main/docs) and are
published with GitHub Pages (*Settings → Pages → Deploy from a branch → `main`, `/docs`*),
using the [Just the Docs](https://just-the-docs.com) theme.
