---
title: Privacy
nav_order: 6
---

# Privacy

The app sees which field of which program you are in, and its keyboard hook receives every
keystroke. So here is exactly what it stores and what it doesn't.

## Keystrokes

**Never stored or sent anywhere.** The hook only checks whether a layout switch hotkey is
pressed (Ctrl/Alt+Shift, Win+Space, <kbd>`</kbd>) and records just the moment of the press.
The app doesn't read what you type: it takes only a field's role and label, never its value.

## The memory

`%LOCALAPPDATA%\KeyLayoutPin\layouts.json` holds, for each field, only:

- the salted SHA-256 hash of the field's key;
- the layout.

No field names, app names or timestamps. The salt is random per installation. Hashing
doesn't stop someone who has the file from testing a specific guess ("was there field X in
app Y?"), but they can't list your fields.

The default layout is not stored: an entry exists only if a field has a different one. At
most 1000 entries are kept; the oldest are dropped.

`settings.json` next to it holds only the choices from the Settings window.

The files live in `Local`, not `Roaming`, so they are not synced to servers with your user
profile. Only your account has access.

## The log

Without `-v` the log contains only app names and layouts:

```text
[chrome.exe] field: en-US → ja-JP
```

With `-v`, and in the `-watch` and `-probe` modes, it includes field names and window or page
titles — that is personal data. Enable them for diagnostics only.

The log file is capped at 1 MB.

## Chrome and Electron apps

To tell apart fields inside pages, the app turns on accessibility in Chromium-based apps.
After that, page content is available through Windows accessibility to other programs
running as you too (although any such program could turn it on itself). If you don't want
that, use `-chromium=false`.

## Network

The app doesn't use the network.
