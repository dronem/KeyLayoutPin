---
title: Troubleshooting
nav_order: 5
---

# Troubleshooting
{: .no_toc }

1. TOC
{:toc}

---

## Nothing switches at all

- **Is the app running?** Look for the blue **Aa** icon in the notification area (it may be
  hidden under the ^ arrow). The diagnostic mode `-watch` doesn't switch anything — only
  `keylayoutpin_bg.exe` without diagnostic options does.
- **Is the window running as administrator?** Windows doesn't let a normal program send
  such windows requests. The log says `window rejected the request … (running as
  administrator?)`. To cover them, run KeyLayoutPin as administrator too.

## The hotkey cycles through more layouts than I have

`keylayoutpin.exe -list` shows the layouts Windows has loaded. If there is one you never
added in Settings — often an extra **English (US)** — it is a "phantom" layout that some
program loaded.

A common source is **WSLg**, which shows Linux GUI apps and starts with WSL — for example
with Docker Desktop. It loads the US layout. If you don't run Linux GUI apps, turn it off:

1. Create `%USERPROFILE%\.wslconfig` with:

   ```ini
   [wsl2]
   guiApplications=false
   ```

2. Quit Docker Desktop and run `wsl --shutdown` (this stops all containers), then start
   Docker again.

To remove a phantom that is already loaded, add that keyboard in *Settings → Time &
language → Language & region* and remove it right away — or sign out and in again.

Also check the layouts of the sign-in screen: *Language & region → Administrative language
settings → Copy settings → Welcome screen and system accounts*.

## A field doesn't keep its layout

A field is recognized by its name. If the name changes, each variant is a new field and gets
the default layout. If two places have the same name, they share one layout. Find out which
it is with [`-watch`](#diagnostics): the `key=` of the same place must stay the same.

Known cases:

- **VS Code: the code editor and the commit message box** report the same name, so they
  share one layout.
- **Fields whose name includes changing text** — "Message to John", "Reply to Anna" — are a
  new field for each name.
- **The first focus in a freshly opened Chrome/Electron window** may count as the whole
  window; the next ones are recognized properly.
- **Console windows** (conhost, Windows Terminal) report the layout unreliably.

## The log says an app is not responding

`app X is not responding — skipping it for 30s` means the app didn't answer for 2 seconds.
Its fields are skipped for 30 seconds; everything else keeps working. That is normal for an
app that is busy or hung, and it recovers by itself.

## VS Code switches to screen reader mode

With `editor.accessibilitySupport: auto`, VS Code may decide a screen reader is running,
because KeyLayoutPin asks it what has focus. If that gets in the way, set
`"editor.accessibilitySupport": "off"` in VS Code settings — fields are still told apart.

## Diagnostics

Keep the app running and start the console build next to it — `-watch` only observes, it
never switches anything:

```powershell
.\keylayoutpin.exe -watch 3m -log watch.txt
```

Click through the places that misbehave. For every focus change it logs what the app
received and what it decided:

```text
13:36:47 FOCUS Code.exe hwnd=Chrome_RenderWidgetHostHWND obj=-4 child=-734637 layout=ja-JP
    event:   ok=true text "Terminal 3, powershell Run the command: …" editable=true
    walk:    ok=true text "Terminal 3, powershell Run the command: …" editable=true
    result:  field key=Code.exe|Chrome_WidgetWin_1|Chrome_WidgetWin_1|42|Terminal #
13:36:51 LAYOUT en-US in Code.exe (after hotkey: true)
```

- `result` is the decision: `field`, `window` (the whole window is one field) or
  `not a field`;
- `key` identifies the field — the same place must always give the same key;
- `LAYOUT … (after hotkey: true)` is your own switch, `false` — a switch by Windows or an app.

To see what the running app itself decided, restart it with a detailed log instead:

```powershell
.\keylayoutpin_bg.exe -v -log app.txt
```

{: .warning }
> These logs contain field names and window titles. Look through them before sharing.
