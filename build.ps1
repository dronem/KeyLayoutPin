# Builds both versions of change_lang:
#   change_lang.exe    - console build, for diagnostics (-watch, -probe, -list, -v)
#   change_lang_bg.exe - GUI build without a console window, for everyday use and autostart
#
# Usage:  .\build.ps1          build
#         .\build.ps1 -Test    run the tests first

param([switch]$Test)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

if ($Test) {
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'tests failed' }
}

$running = Get-Process change_lang, change_lang_bg -ErrorAction SilentlyContinue
if ($running) {
    throw "change_lang is running (PID $($running.Id -join ', ')) - exit it first: tray icon -> Exit"
}

go build -o change_lang.exe .
if ($LASTEXITCODE -ne 0) { throw 'console build failed' }

go build -ldflags '-H=windowsgui' -o change_lang_bg.exe .
if ($LASTEXITCODE -ne 0) { throw 'GUI build failed' }

Get-Item change_lang.exe, change_lang_bg.exe | ForEach-Object {
    '{0,-20} {1,6:N1} MB' -f $_.Name, ($_.Length / 1MB)
}
