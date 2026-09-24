# Builds both versions of KeyLayoutPin:
#   keylayoutpin.exe    - console build, for diagnostics (-watch, -probe, -list, -v)
#   keylayoutpin_bg.exe - GUI build without a console window, for everyday use and autostart
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

$running = Get-Process keylayoutpin, keylayoutpin_bg -ErrorAction SilentlyContinue
if ($running) {
    throw "KeyLayoutPin is running (PID $($running.Id -join ', ')) - exit it first: tray icon -> Exit"
}

go build -o keylayoutpin.exe .
if ($LASTEXITCODE -ne 0) { throw 'console build failed' }

go build -ldflags '-H=windowsgui' -o keylayoutpin_bg.exe .
if ($LASTEXITCODE -ne 0) { throw 'GUI build failed' }

Get-Item keylayoutpin.exe, keylayoutpin_bg.exe | ForEach-Object {
    '{0,-20} {1,6:N1} MB' -f $_.Name, ($_.Length / 1MB)
}
