---
title: How it works
nav_order: 3
---

# How it works
{: .no_toc }

1. TOC
{:toc}

---

## The idea

Every time focus moves, KeyLayoutPin asks Windows what received it. If it is an input field,
the app looks the field up in its memory and switches to the layout the field had last
time. When you switch the layout yourself, the app remembers it for the field you are in.

## What counts as a field

| Focus on | What happens |
|---|---|
| **An input field** — a text box, an editable combo box, a document in Word or WordPad | it gets its own layout |
| **An app that doesn't say what has focus** — games, custom-drawn controls | the whole window counts as one field |
| **A button, menu, list, tab or the taskbar** | not a field: the layout is left alone, and the last field stays current |

So clicking a toolbar button or opening a menu doesn't change anything: when you get back to
typing, you are still "in" the same field.

A field is recognized by its app, its window and its **name** — the label or placeholder
the app reports, such as "Search" or "Message input". Numbers in names are ignored, so
"Tab 2" and "Tab 3" are one field.

### Example: VS Code

| Place | Name VS Code reports | Result |
|---|---|---|
| Chat box | `Message input` | its own field |
| Any terminal | `Terminal 1, powershell …`, `Terminal 3, node …` | **one field for all terminals** — the title after the number changes with what runs in it, so it is ignored |
| Code editor and the commit message box | `The editor is not accessible at this time …` | **one shared field** — VS Code gives every editor the same name |

## What gets remembered

- **A switch with a hotkey** — Ctrl+Shift, Alt+Shift, Win+Space or <kbd>`</kbd> — is
  remembered for the current field right away.
- **The layout a field had when you left it** is remembered too. So on return you get "the
  one from last time", even if you switched it with the mouse.
- **The default layout is not stored.** A field has an entry only if its layout differs from
  the default. Switching a field back to the default removes its entry.

## What the default layout is

Fields you never switched get the default layout. By default it is the one from Windows
settings (*Settings → Time & language → Typing → Advanced keyboard settings*). The
[`-default`](options.md) option changes that: a specific language, or `keep` to leave new
fields alone.

## Changes made by Windows or apps

Windows itself sometimes switches the layout — for example, when it restores the one a
window had before — and some apps do too. Such a change in the first second after you enter
a field is undone, so the field gets the layout it should have. Later changes are left
alone.

## Browsers, VS Code and other Chromium/Electron apps

Chrome, Edge, VS Code, Slack, Discord and other apps built on Chromium don't tell Windows
which field inside a page has focus — until something asks them for it. KeyLayoutPin asks
each such window once, and from then on its fields are told apart. Because of that, the very
first focus in a freshly opened window may count as "the whole window".

You can turn this off with [`-chromium=false`](options.md), but then all fields inside such
an app share one layout.

## Hung apps

The app asks other programs what has focus on a separate thread. If a program hangs and
doesn't answer for 2 seconds, it is skipped for 30 seconds, while switching keeps working
everywhere else. The log then says `app X is not responding — skipping it for 30s`.
