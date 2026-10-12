# Builds both versions of KeyLayoutPin:
#   keylayoutpin.exe    - console build, for diagnostics (-watch, -probe, -list, -v)
#   keylayoutpin_bg.exe - GUI build without a console window, for everyday use and autostart
#
# Usage:  .\build.ps1                    build; the version is the git commit
#         .\build.ps1 -Test              run the tests first
#         .\build.ps1 -Version 1.0.0     a release build with an explicit version

param(
    [switch]$Test,
    [string]$Version
)

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

# -trimpath keeps local paths (C:\Users\..., the project folder) out of the binaries.
$versionFlag = if ($Version) { "-X main.version=$Version" } else { $null }

function Build($out, $ldflags) {
    # Arguments as an array: PowerShell 5.1 drops empty strings when calling go.
    $buildArgs = @('build', '-trimpath')
    $ldflags = @($ldflags | Where-Object { $_ })
    if ($ldflags) { $buildArgs += @('-ldflags', ($ldflags -join ' ')) }
    go @buildArgs -o $out .
    if ($LASTEXITCODE -ne 0) { throw "build of $out failed" }
}

Build keylayoutpin.exe @($versionFlag)
Build keylayoutpin_bg.exe @('-H=windowsgui', $versionFlag)

Get-Item keylayoutpin.exe, keylayoutpin_bg.exe | ForEach-Object {
    '{0,-20} {1,6:N1} MB' -f $_.Name, ($_.Length / 1MB)
}
& .\keylayoutpin.exe -version
