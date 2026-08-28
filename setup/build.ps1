<#
.SYNOPSIS
    Builds the four binaries and, optionally, the installer.

.DESCRIPTION
    Nothing here needs administrator rights: it only writes into bin\ and
    installer\Output\.

    Two things are deliberate and worth keeping:

      -trimpath     strips absolute source paths out of the binaries. Without it
                    every published exe carries the developer's home directory
                    around inside it.
      CGO_ENABLED=0 the whole project is pure Go on purpose - no gcc, no MinGW,
                    no Docker, nothing to install before building. The SQLite
                    driver is modernc.org/sqlite, which is pure Go as well.

.PARAMETER GoRoot
    Where the Go toolchain lives. Defaults to the copy kept next to the project,
    then falls back to whatever "go" is on PATH.

.PARAMETER Installer
    Also compile the Inno Setup installer.

.PARAMETER Iscc
    Path to ISCC.exe (the Inno Setup compiler).
#>
[CmdletBinding()]
param(
    [string] $GoRoot    = 'C:\Users\horro\Desktop\Claude\go-toolchain\go',
    [switch] $Installer,
    [string] $Iscc      = 'C:\Users\horro\Desktop\Claude\inno-tools\is6\ISCC.exe'
)

$ErrorActionPreference = 'Stop'

$repo    = Split-Path -Parent $PSScriptRoot
$binDir  = Join-Path $repo 'bin'
$iconIco = Join-Path $repo 'installer\icon.ico'

$productName = 'Plex External Audio'
$version     = '1.0.0'

function Say($t, $c = 'Gray') { Write-Host $t -ForegroundColor $c }

# --- toolchain -------------------------------------------------------------
if (Test-Path -LiteralPath $GoRoot) {
    $env:GOROOT  = $GoRoot
    $goPath      = Split-Path -Parent $GoRoot
    $env:GOPATH  = Join-Path $goPath 'gopath'
    $env:GOCACHE = Join-Path $goPath 'gocache'
    $env:PATH    = "$env:GOROOT\bin;$env:GOPATH\bin;$env:PATH"

    # Keep the linker's scratch space next to the toolchain instead of in
    # %TEMP%. Antivirus software tends to inspect a freshly linked executable
    # the moment it appears, and blocking that write fails the build with a bare
    # "Access is denied" on a path like %TEMP%\go-build1263967565\b001\exe.
    # That directory is named anew on every build, so it cannot be excluded by
    # hand - whereas the toolchain directory can be, once.
    $env:GOTMPDIR = Join-Path $goPath 'gotmp'
    New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
}
$env:CGO_ENABLED = '0'
$env:GOOS        = 'windows'
$env:GOARCH      = 'amd64'

Say "=== $productName $version ===" Cyan
Say ("  go: " + (& go version))

New-Item -ItemType Directory -Force -Path $binDir | Out-Null

# --- what to build ---------------------------------------------------------
# manifest: "gui" for the two that must never flash a console window,
#           "cli" for the two that are console programs by nature.
$targets = @(
    @{ Pkg = './cmd/mapper';     Exe = "$productName Mapper.exe";     Gui = $false
       Desc = 'Writes external audio tracks into the Plex database' },
    @{ Pkg = './cmd/transcoder'; Exe = "$productName Transcoder.exe"; Gui = $false
       Desc = 'Transcoder wrapper that feeds Plex the external audio file' },
    @{ Pkg = './cmd/guard';      Exe = "$productName Guard.exe";      Gui = $true
       Desc = 'Restores the wrapper and the tracks after Plex updates' },
    @{ Pkg = './cmd/tray';       Exe = "$productName Tray.exe";       Gui = $true
       Desc = 'Tray icon: status, checks and settings' }
)

Push-Location $repo
try {
    foreach ($t in $targets) {
        $pkgDir = Join-Path $repo ($t.Pkg -replace '^\./', '' -replace '/', '\')

        # Version info, so the exe is identifiable in Task Manager and in the
        # file properties dialog instead of showing a blank Description column.
        if (Test-Path -LiteralPath $iconIco) {
            $manifest = if ($t.Gui) { 'gui' } else { 'cli' }
            Push-Location $pkgDir
            try {
                & go-winres simply `
                    --arch amd64 `
                    --out rsrc `
                    --icon $iconIco `
                    --manifest $manifest `
                    --product-name $productName `
                    --file-description $t.Desc `
                    --original-filename $t.Exe `
                    --product-version $version `
                    --file-version $version `
                    --copyright 'Fork of Saoneth/plex-custom-audio' | Out-Null
                if ($LASTEXITCODE -ne 0) { throw "go-winres failed for $($t.Pkg)" }
            } finally { Pop-Location }
        } else {
            Say "  no icon at $iconIco - building without version info" Yellow
        }

        # Deliberately NOT stripped. Adding "-s -w" shaves about a third off the
        # size, and it also makes Kaspersky delete the mapper on sight and refuse
        # to even let the linker write it ("Access is denied" on the temporary
        # a.out.exe). Stripped Go binaries are how a great deal of Go malware
        # ships, precisely to frustrate analysis, so the heuristic is not being
        # unreasonable. The same thing would happen on the machine of anyone who
        # downloads the installer, which is a far worse problem than 3 MB.
        # Built as an argument list rather than a command line: PowerShell drops
        # an empty string argument silently, so "-ldflags $ldflags" with no
        # flags would hand "-o" to -ldflags and shift everything after it.
        $out = Join-Path $binDir $t.Exe
        $goArgs = @('build', '-trimpath')
        if ($t.Gui) { $goArgs += @('-ldflags', '-H=windowsgui') }
        $goArgs += @('-o', $out, $t.Pkg)

        & go @goArgs
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $($t.Pkg)" }

        $kb = [math]::Round((Get-Item -LiteralPath $out).Length / 1KB)
        Say ("  built {0,-45} {1,7} KB" -f $t.Exe, $kb) Green
    }
} finally { Pop-Location }

# --- installer -------------------------------------------------------------
if ($Installer) {
    if (-not (Test-Path -LiteralPath $Iscc)) { throw "ISCC.exe not found: $Iscc" }
    $iss = Join-Path $repo 'installer\plex-external-audio.iss'
    Say ''
    Say '  compiling the installer...'
    & $Iscc $iss | Where-Object { $_ -match 'Successful|error|Error' } | ForEach-Object { Say "  $_" }
    if ($LASTEXITCODE -ne 0) { throw 'ISCC failed' }

    $setup = Get-ChildItem (Join-Path $repo 'installer\Output') -Filter '*.exe' |
             Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if ($setup) {
        Say ("  installer: {0} ({1:N1} MB)" -f $setup.FullName, ($setup.Length / 1MB)) Green
    }
}

Say ''
Say 'Done.' Green
