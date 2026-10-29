---
title: Home
nav_order: 1
permalink: /
---

# KeyLayoutPin

Pins the keyboard layout **to each input field separately**, in any Windows program.
{: .fs-6 .fw-300 }

[Download](https://github.com/dronem/keylayoutpin/releases){: .btn .btn-primary .mr-2 }
[Getting started](getting-started.md){: .btn }

---

Switch the layout to English in a terminal and to Japanese in a chat box — from then on
the layout switches by itself as you move between those fields. Fields where you never
switched get the default layout from Windows settings.

It works across Windows apps, not just one kind:

- classic desktop programs (Notepad, Office, WinForms, WPF, MFC);
- browsers — Chrome, Edge, Firefox;
- Electron apps — VS Code, Slack, Discord;
- Qt apps — Telegram;
- and more.

Where an app tells Windows which field has focus, every field gets its own layout. Where it
doesn't (games, some custom-drawn apps), the layout is remembered for the whole window.
Any installed layouts can be used, and any number of them.

## In 30 seconds

1. [Download](https://github.com/dronem/keylayoutpin/releases) the zip, unpack it anywhere
   and run `keylayoutpin_bg.exe`. A blue **Aa** icon appears in the notification area.
2. Click into a field and switch the layout with your usual hotkey — Ctrl+Shift,
   Alt+Shift or Win+Space.
3. That's it: the field keeps that layout. Come back to it from anywhere, and the layout
   comes back too.

The app starts with Windows from then on (you can turn that off in the tray menu). It needs
no installation, no administrator rights and no network.

## Where to next

| | |
|---|---|
| [Getting started](getting-started.md) | install, the tray menu, autostart, updating and removing |
| [How it works](how-it-works.md) | what counts as a field and what gets remembered |
| [Command-line options](options.md) | every flag, with examples |
| [Troubleshooting](troubleshooting.md) | when a field doesn't keep its layout — and how to find out why |
| [Privacy](privacy.md) | what the app sees, stores and never stores |
| [Development](development.md) | building from source, internals, code layout |

---

Made for my Japanese friend Satoshi (さとしさん), who switches keyboard layouts many times a
day at work. :-)
