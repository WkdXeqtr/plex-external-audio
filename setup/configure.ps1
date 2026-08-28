<#
.SYNOPSIS
    Configures the system after the installer has laid down the files.

.DESCRIPTION
    The installer (Inno Setup) copies the files, creates the Start menu shortcut
    and the entry in Apps and features. What is left is everything that changes
    the system beyond files: swapping the Plex transcoder, registering the
    scheduled tasks, and starting the tray icon at sign-in.

    That work lives here, as readable text, rather than inside an executable -
    so it can be inspected, and so antivirus behaviour analysis does not mistake
    the program for malware digging itself into the system.

    Run by the installer with administrator rights.

.PARAMETER Dest
    The directory the installer put the files in.

.PARAMETER Uninstall
    Do the reverse: remove the tasks, restore the original transcoder, drop the
    autostart entry. The files and registry entries are removed by Inno itself.

.PARAMETER CleanDb
    Uninstall only: also remove the external-track rows from the Plex database.
    The uninstaller asks the user about this and passes the answer through.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string] $Dest,
    [switch] $Uninstall,
    [switch] $CleanDb
)

$ErrorActionPreference = 'Continue'

$appDirName = 'Plex External Audio'
$stateDir   = Join-Path $env:ProgramData  $appDirName
$userDir    = Join-Path $env:LOCALAPPDATA $appDirName

$mapperExe  = 'Plex External Audio Mapper.exe'
$wrapperExe = 'Plex External Audio Transcoder.exe'
$guardExe   = 'Plex External Audio Guard.exe'
$trayExe    = 'Plex External Audio Tray.exe'
$runValue   = 'PlexExternalAudioTray'

# The marker the guard uses to tell our wrapper from the real Plex transcoder.
# NEVER change it. It still spells the project's former name on purpose: a blind
# search-and-replace across the repository would break wrapper identification,
# and a guard that cannot recognise its own wrapper deletes the real transcoder.
# That has happened once already.
$marker = 'PLEX-CUSTOM-AUDIO-WRAPPER-MARKER-e9f1c0a4-DO-NOT-PARK-THIS-FILE'

function Say($t, $c = 'Gray') { Write-Host $t -ForegroundColor $c }

# --- locating things -------------------------------------------------------
function Get-PlexDir {
    foreach ($k in @('HKLM:\SOFTWARE\Plex, Inc.\Plex Media Server',
                     'HKLM:\SOFTWARE\WOW6432Node\Plex, Inc.\Plex Media Server',
                     'HKCU:\Software\Plex, Inc.\Plex Media Server')) {
        if (Test-Path $k) {
            $v = (Get-ItemProperty $k -ErrorAction SilentlyContinue).InstallFolder
            if ($v -and (Test-Path -LiteralPath $v)) { return $v }
        }
    }
    $guess = Join-Path $env:ProgramFiles 'Plex\Plex Media Server'
    if (Test-Path -LiteralPath $guess) { return $guess }
    return $null
}

function Get-DbPath {
    $root = (Get-ItemProperty 'HKCU:\Software\Plex, Inc.\Plex Media Server' -ErrorAction SilentlyContinue).LocalAppDataPath
    if (-not $root) { $root = $env:LOCALAPPDATA }
    Join-Path $root 'Plex Media Server\Plug-in Support\Databases\com.plexapp.plugins.library.db'
}

function Test-IsOurs($path) {
    if (-not (Test-Path -LiteralPath $path)) { return $false }
    $bytes = [System.IO.File]::ReadAllBytes($path)
    return ([System.Text.Encoding]::ASCII.GetString($bytes)).Contains($marker)
}

function Stop-Ours {
    foreach ($n in @($trayExe, $guardExe, $mapperExe, 'Plex Transcoder.exe', 'Plex Transcoder_org.exe')) {
        $procName = [System.IO.Path]::GetFileNameWithoutExtension($n)
        Get-Process -Name $procName -ErrorAction SilentlyContinue |
            Stop-Process -Force -ErrorAction SilentlyContinue
    }
}

$plexDir = Get-PlexDir
if (-not $plexDir) { throw 'the Plex install directory could not be found' }
$live   = Join-Path $plexDir 'Plex Transcoder.exe'
$parked = Join-Path $plexDir 'Plex Transcoder_org.exe'
$tasks  = Join-Path $Dest 'setup\tasks.ps1'

# ===========================================================================
if ($Uninstall) {
    Say '=== removing configuration ===' Cyan

    if (Test-Path -LiteralPath $tasks) { & $tasks -Action Remove }

    $runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
    if (Get-ItemProperty -Path $runKey -Name $runValue -ErrorAction SilentlyContinue) {
        Remove-ItemProperty -Path $runKey -Name $runValue
        Say '  autostart entry removed'
    }

    Stop-Ours
    Start-Sleep -Seconds 2

    # Restore the real transcoder. The check matters: if one of our wrappers
    # somehow ended up parked as _org, moving it back would leave Plex with a
    # wrapper on both sides and no transcoder at all.
    if (Test-Path -LiteralPath $parked) {
        if (Test-IsOurs $parked) {
            Say '  WARNING: _org holds a wrapper, not the original - not touching the transcoder, reinstall Plex' Red
        } else {
            Remove-Item -LiteralPath $live -Force -ErrorAction SilentlyContinue
            Rename-Item -LiteralPath $parked -NewName 'Plex Transcoder.exe'
            Say '  original transcoder restored'
        }
    } else {
        Say '  nothing parked - the transcoder looks original already' Yellow
    }

    if ($CleanDb) {
        $mapper = Join-Path $Dest $mapperExe
        if (Test-Path -LiteralPath $mapper) {
            & $mapper -dbPath (Get-DbPath) -clean 2>&1 |
                Where-Object { $_ -match 'Removed' } | ForEach-Object { Say "  $_" }
        } else {
            Say '  mapper is already gone - clear the rows with a library re-analysis in Plex' Yellow
        }
    }

    # Per-user state: settings, the chosen language, the "already greeted" and
    # "paused" flags. Logs in ProgramData are left behind on purpose - they are
    # the only record of what went wrong, and someone uninstalling in anger is
    # exactly the person who will need them.
    if (Test-Path -LiteralPath $userDir) {
        Remove-Item -LiteralPath $userDir -Recurse -Force -ErrorAction SilentlyContinue
        Say '  user settings removed'
    }

    Say "  logs kept in $stateDir"
    Say 'Done.' Green
    return
}

# ===========================================================================
Say '=== configuring ===' Cyan

# 1. Swap the transcoder. If ours is already live there is nothing to do - and
#    crucially nothing to park, or we would park our own wrapper on top of the
#    saved original.
if (Test-IsOurs $live) {
    Say '  wrapper already installed'
} else {
    if (Test-Path -LiteralPath $parked) {
        # Something is parked already. Replace it only if it is one of ours;
        # a genuine original must never be overwritten.
        if (Test-IsOurs $parked) { Remove-Item -LiteralPath $parked -Force }
    }
    if (-not (Test-Path -LiteralPath $parked)) {
        Move-Item -LiteralPath $live -Destination $parked
    } else {
        Remove-Item -LiteralPath $live -Force
    }
    Copy-Item -LiteralPath (Join-Path $Dest $wrapperExe) -Destination $live -Force
    Say '  transcoder swapped, original kept as Plex Transcoder_org.exe'
}

# 2. config.json for the guard
$wrapperHash = (Get-FileHash -LiteralPath (Join-Path $Dest $wrapperExe) -Algorithm SHA256).Hash

# ffprobe has to be reasonably recent: old builds (3.x, the kind bundled inside
# random software) misread the metadata of HDR and multi-track files. So we do
# not take the first one we find - we check the major version.
function Test-FfprobeOk($path) {
    if (-not $path -or -not (Test-Path -LiteralPath $path)) { return $false }
    try {
        $v = & $path -version 2>$null | Select-Object -First 1
        if ($v -match 'ffprobe version n?(\d+)') { return [int]$Matches[1] -ge 5 }
    } catch {}
    return $false
}

# Deliberately a BOUNDED list of places to look. An earlier version fell back to
# a recursive scan of the whole C: drive, and since the installer waits for this
# script to finish, that showed up to the user as "the installer has hung".
$ffprobe = $null
$candidates = @(
    (Get-Command ffprobe.exe -ErrorAction SilentlyContinue).Source,
    (Join-Path $Dest 'ffmpeg\ffprobe.exe'),
    (Join-Path $env:ProgramFiles 'ffmpeg\bin\ffprobe.exe'),
    (Join-Path ${env:ProgramFiles(x86)} 'ffmpeg\bin\ffprobe.exe'),
    'C:\ProgramData\chocolatey\bin\ffprobe.exe'
)
# C:\ffmpeg*, the layout you get from unpacking the official zip by hand
$candidates += Get-ChildItem -Path 'C:\' -Filter 'ffmpeg*' -Directory -ErrorAction SilentlyContinue |
               ForEach-Object { Join-Path $_.FullName 'bin\ffprobe.exe' }
# winget drops it under a versioned package directory
$wingetRoot = Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Packages'
if (Test-Path $wingetRoot) {
    $candidates += Get-ChildItem $wingetRoot -Filter 'ffprobe.exe' -Recurse -Depth 3 -ErrorAction SilentlyContinue |
                   ForEach-Object { $_.FullName }
}

foreach ($c in $candidates) {
    if (Test-FfprobeOk $c) { $ffprobe = $c; break }
}

$cfg = [ordered]@{
    plexDir       = $plexDir
    plexExe       = Join-Path $plexDir 'Plex Media Server.exe'
    wrapperSrc    = Join-Path $Dest $wrapperExe
    wrapperSha256 = $wrapperHash
    mapperExe     = Join-Path $Dest $mapperExe
    ffprobe       = $ffprobe
    dbPath        = Get-DbPath
    stateDir      = $stateDir
    scanRoots     = @()
}
New-Item -ItemType Directory -Force -Path $stateDir | Out-Null
# UTF-8 WITHOUT a BOM: Go's JSON decoder refuses a BOM outright, and Windows
# PowerShell's Out-File -Encoding utf8 writes one.
[System.IO.File]::WriteAllText((Join-Path $Dest 'config.json'),
    ($cfg | ConvertTo-Json), (New-Object System.Text.UTF8Encoding $false))
Say '  config.json written'

if (-not $ffprobe) {
    Say '  ffprobe NOT FOUND - without it nothing can read the audio files' Yellow
    # Worth interrupting the user for: without ffprobe the program cannot work at
    # all, and a line of text in a hidden console would never be seen.
    Add-Type -AssemblyName System.Windows.Forms
    [System.Windows.Forms.MessageBox]::Show(
        ("ffprobe (part of ffmpeg) was not found." + [Environment]::NewLine + [Environment]::NewLine +
         "Plex External Audio needs it to read external audio files. Install ffmpeg, then put the " +
         "full path to ffprobe.exe into the ""ffprobe"" field of:" + [Environment]::NewLine +
         (Join-Path $Dest 'config.json')),
        'Plex External Audio', 'OK', 'Warning') | Out-Null
}

# 3. scheduled tasks
if (Test-Path -LiteralPath $tasks) {
    & $tasks -Action Install -GuardExe (Join-Path $Dest $guardExe)
}

# A fresh install must never start out paused. The flag survives an uninstall
# that left the user directory behind, and the guard would then silently do
# nothing at all, which looks exactly like a broken install.
Remove-Item -LiteralPath (Join-Path $userDir 'paused') -Force -ErrorAction SilentlyContinue

# Filling the database is deliberately NOT done here. The mapper scans the whole
# library for minutes, and the installer waits for this script - which reads as a
# hung installer. The tray icon does it in the background right after it starts.

# 4. autostart and first launch
$tray = Join-Path $Dest $trayExe
if (Test-Path -LiteralPath $tray) {
    Set-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' `
        -Name $runValue -Value ('"{0}"' -f $tray)

    $procName = [System.IO.Path]::GetFileNameWithoutExtension($trayExe)
    Get-Process -Name $procName -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 300

    # Start it THROUGH explorer: this script runs as administrator, and a plain
    # Start-Process would hand the tray icon administrator rights as well. An
    # ordinary user could then not close it ("access denied"), and it has no use
    # for those rights anyway.
    Start-Process 'explorer.exe' -ArgumentList "`"$tray`""
    Say '  tray icon registered for sign-in and started (as the user)'
}

Say 'Done.' Green
