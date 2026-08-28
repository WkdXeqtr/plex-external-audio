#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Brings back a Plex transcoder that has gone missing.

.DESCRIPTION
    The one failure mode of this program that Plex itself cannot shrug off: the
    real Plex Transcoder.exe is gone and only wrappers are left. It happened once
    for real, back when the guard identified its own wrapper by file hash - after
    a rebuild it no longer recognised its previous build, "parked" it as if it
    were the original, and the genuine transcoder was lost. Identification is by
    an embedded marker now, so this should not recur, but the recovery path is
    worth keeping.

    What it does: stops our tasks, works out what is actually sitting in the Plex
    directory, and if the original is truly gone, reinstalls Plex over itself
    from the update package Plex has already downloaded. That is a repair
    install - the library, the database and the settings are not touched.

    Nothing here is specific to one machine: the Plex directory comes from the
    registry and the installer is whichever is newest under Updates.
#>
$ErrorActionPreference = 'Stop'

# NEVER change this marker - see the note in configure.ps1.
$marker = 'PLEX-CUSTOM-AUDIO-WRAPPER-MARKER-e9f1c0a4-DO-NOT-PARK-THIS-FILE'

function Say($t, $c = 'Gray') { Write-Host $t -ForegroundColor $c }
function Test-IsOurs($path) {
    if (-not (Test-Path -LiteralPath $path)) { return $false }
    $bytes = [System.IO.File]::ReadAllBytes($path)
    return ([System.Text.Encoding]::ASCII.GetString($bytes)).Contains($marker)
}

Say '=== recovering the Plex transcoder ===' Cyan

$plexDir = $null
foreach ($k in @('HKLM:\SOFTWARE\Plex, Inc.\Plex Media Server',
                 'HKLM:\SOFTWARE\WOW6432Node\Plex, Inc.\Plex Media Server',
                 'HKCU:\Software\Plex, Inc.\Plex Media Server')) {
    if (Test-Path $k) {
        $v = (Get-ItemProperty $k -ErrorAction SilentlyContinue).InstallFolder
        if ($v -and (Test-Path -LiteralPath $v)) { $plexDir = $v; break }
    }
}
if (-not $plexDir) { $plexDir = Join-Path $env:ProgramFiles 'Plex\Plex Media Server' }
$live   = Join-Path $plexDir 'Plex Transcoder.exe'
$parked = Join-Path $plexDir 'Plex Transcoder_org.exe'
Say "  Plex directory: $plexDir"

# 1. Stop our scheduled tasks first, or the guard will step in halfway through
#    and put its wrapper back while we are still sorting the directory out.
foreach ($t in @('Plex External Audio', 'Plex External Audio (logon)',
                 'PlexCustomAudio Guard', 'PlexCustomAudio Guard (logon)')) {
    if (Get-ScheduledTask -TaskName $t -ErrorAction SilentlyContinue) {
        Disable-ScheduledTask -TaskName $t | Out-Null
        Say "  task disabled: $t" Yellow
    }
}

# 2. Stop Plex and everything it spawned
foreach ($n in @('Plex Media Server', 'Plex Transcoder', 'Plex Transcoder_org',
                 'Plex External Audio Tray', 'Plex External Audio Guard', 'pca-tray', 'pca-guard')) {
    Get-Process -Name $n -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Seconds 3

# 3. If a genuine original is parked, this is a two-second job.
if ((Test-Path -LiteralPath $parked) -and -not (Test-IsOurs $parked)) {
    Remove-Item -LiteralPath $live -Force -ErrorAction SilentlyContinue
    Rename-Item -LiteralPath $parked -NewName 'Plex Transcoder.exe'
    Say '  the parked original was genuine - restored, no reinstall needed' Green
    Say ''
    Say 'Re-enable the tasks from the tray menu, or just start the program.' Cyan
    exit 0
}

# 4. Otherwise clear out whatever wrappers are lying around. None of them is the
#    original - we just established that - so there is nothing here to lose, and
#    the Plex installer wants a clean spot.
foreach ($f in @($parked, $live)) {
    if ((Test-Path -LiteralPath $f) -and (Test-IsOurs $f)) {
        Remove-Item -LiteralPath $f -Force
        Say "  wrapper removed: $(Split-Path $f -Leaf)" Yellow
    }
}

if (Test-Path -LiteralPath $live) {
    Say ''
    Say 'Plex Transcoder.exe is present and is not one of ours - nothing to do.' Green
    exit 0
}

# 5. Repair install from the newest package Plex has already downloaded.
$updates = Join-Path $env:LOCALAPPDATA 'Plex Media Server\Updates'
$setup = $null
if (Test-Path $updates) {
    $setup = Get-ChildItem $updates -Filter '*.exe' -Recurse -ErrorAction SilentlyContinue |
             Sort-Object LastWriteTime -Descending | Select-Object -First 1
}
if (-not $setup) {
    Say ''
    Say 'The original transcoder is gone and no Plex update package was found.' Red
    Say "Download Plex Media Server from plex.tv and install it over the top -" Red
    Say 'that is a repair install, your library and settings are safe.' Red
    exit 1
}

Say ''
Say "  running the Plex installer: $($setup.Name)"
Say '  this installs over the top; the library and settings are not touched' Yellow
Start-Process -FilePath $setup.FullName -Wait

if (Test-Path -LiteralPath $live) {
    Say ''
    Say '  Plex Transcoder.exe is back' Green
    Say ''
    Say 'Now reinstall Plex External Audio, or press Check in the tray menu.' Cyan
} else {
    Say ''
    Say '  the transcoder did not reappear - reinstall Plex by hand' Red
}
