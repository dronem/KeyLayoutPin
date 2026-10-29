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

Made for my Japanese friend Satoshi (さとしさん), who switches keyboard layouts many
times a day at work. :-)

## Documentation

**[dronem.github.io/keylayoutpin](https://dronem.github.io/keylayoutpin/)** — or read it
right here in [`docs/`](docs/index.md):

- [Getting started](docs/getting-started.md) — install, the tray menu, autostart, updating
- [How it works](docs/how-it-works.md) — what counts as a field and what gets remembered
- [Command-line options](docs/options.md)
- [Troubleshooting](docs/troubleshooting.md) — when a field doesn't keep its layout
- [Privacy](docs/privacy.md) — what the app sees, stores and never stores
- [Development](docs/development.md) — building, internals, code layout

## Install

Download the zip from [Releases](https://github.com/dronem/keylayoutpin/releases),
unpack it anywhere and run `keylayoutpin_bg.exe`. `SHA256SUMS.txt` next to the zip lets
you check the download: `Get-FileHash KeyLayoutPin-*.zip`.

Or with Go installed:

```powershell
go install -ldflags=-H=windowsgui github.com/dronem/keylayoutpin@latest
```

## Build

```powershell
.\build.ps1          # or .\build.ps1 -Test to run the tests first
.\keylayoutpin_bg.exe
```

See [Development](docs/development.md) for details.

## License

[MIT](LICENSE)
