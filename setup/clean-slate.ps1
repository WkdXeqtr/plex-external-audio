#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Wipes every trace of the program so a fresh install can start clean.

.DESCRIPTION
    The emergency exit. Removes scheduled tasks, shortcuts, registry entries and
    the autostart value, restores the original Plex transcoder and deletes the
    install directory. Your audio files and the rows in the Plex database are
    NOT touched.

    It knows both the current name and the old "PlexCustomAudio" one, because
    the point of this script is to clear up after installs that are already
    broken - including ones made before the rename.

    Normal removal is Apps and features, or the Uninstall item in the tray menu.
    Use this only when that has failed.
#>
$ErrorActionPreference = 'Continue'

# NEVER change this marker - see the note in configure.ps1.
$marker = 'PLEX-CUSTOM-AUDIO-WRAPPER-MARKER-e9f1c0a4-DO-NOT-PARK-THIS-FILE'

$installDirs = @(
    (Join-Path $env:ProgramFiles 'Plex External Audio'),
    (Join-Path $env:ProgramFiles 'PlexCustomAudio'),
    (Join-Path ${env:ProgramFiles(x86)} 'PlexCustomAudio')
)
$taskNames = @(
    'Plex External Audio', 'Plex External Audio (logon)',
    'PlexCustomAudio Guard', 'PlexCustomAudio Guard (logon)'
)
$procNames = @(
    'Plex External Audio Tray', 'Plex External Audio Guard', 'Plex External Audio Mapper',
    'pca-tray', 'pca-guard',
    'Plex Transcoder', 'Plex Transcoder_org'
)
$runValues = @('PlexExternalAudioTray', 'PlexCustomAudioTray', 'PlexCustomAudio')
$stateDirs = @(
    (Join-Path $env:ProgramData  'Plex External Audio'),
    (Join-Path $env:LOCALAPPDATA 'Plex External Audio'),
    (Join-Path $env:ProgramData  'PlexCustomAudio'),
    (Join-Path $env:LOCALAPPDATA 'PlexCustomAudio')
)

function Say($t, $c = 'Gray') { Write-Host $t -ForegroundColor $c }
Say '=== clearing up before a clean install ===' Cyan

# 1. scheduled tasks
foreach ($t in $taskNames) {
    if (Get-ScheduledTask -TaskName $t -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $t -Confirm:$false
        Say "  task removed: $t" Yellow
    }
}

# 2. running processes
foreach ($n in $procNames) {
    Get-Process -Name $n -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Seconds 2

# 3. put the original transcoder back
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

if (Test-Path -LiteralPath $parked) {
    $bytes = [System.IO.File]::ReadAllBytes($parked)
    $parkedIsOurs = ([System.Text.Encoding]::ASCII.GetString($bytes)).Contains($marker)
    if ($parkedIsOurs) {
        Say '  WARNING: _org holds a wrapper, not the original - not touching it, reinstall Plex' Red
        Say '           (see recover.ps1, it can do that for you)' Red
    } else {
        Remove-Item -LiteralPath $live -Force -ErrorAction SilentlyContinue
        Rename-Item -LiteralPath $parked -NewName 'Plex Transcoder.exe'
        Say '  original transcoder restored' Green
    }
} else {
    Say '  nothing parked - the transcoder is already the original'
}

# 4. autostart
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
foreach ($name in $runValues) {
    if (Get-ItemProperty -Path $runKey -Name $name -ErrorAction SilentlyContinue) {
        Remove-ItemProperty -Path $runKey -Name $name
        Say "  autostart entry removed: $name" Yellow
    }
}

# 5. entries in Apps and features (both the Inno one and any hand-made one)
foreach ($base in @('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall',
                    'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall')) {
    Get-ChildItem $base -ErrorAction SilentlyContinue | ForEach-Object {
        $p = Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue
        if ($p.DisplayName -like '*Plex External Audio*' -or $p.DisplayName -like '*PlexCustomAudio*') {
            Remove-Item $_.PSPath -Recurse -Force
            Say "  entry removed from Apps and features: $($p.DisplayName)" Yellow
        }
    }
}

# 6. Start menu shortcuts (both the all-users and the per-user branch)
foreach ($root in @("$env:ProgramData\Microsoft\Windows\Start Menu\Programs",
                    "$env:APPDATA\Microsoft\Windows\Start Menu\Programs")) {
    foreach ($leaf in @('Plex External Audio', 'PlexCustomAudio')) {
        $sm = Join-Path $root $leaf
        if (Test-Path $sm) {
            Remove-Item $sm -Recurse -Force
            Say "  shortcuts removed: $sm" Yellow
        }
        $lnk = "$sm.lnk"
        if (Test-Path $lnk) {
            Remove-Item $lnk -Force
            Say "  shortcut removed: $lnk" Yellow
        }
    }
}

# 7. leftover install directories
foreach ($d in $installDirs) {
    if (Test-Path -LiteralPath $d) {
        Remove-Item -LiteralPath $d -Recurse -Force -ErrorAction SilentlyContinue
        Say "  directory removed: $d" Yellow
    }
}

# 8. state and settings
foreach ($d in $stateDirs) {
    if (Test-Path -LiteralPath $d) {
        Remove-Item -LiteralPath $d -Recurse -Force -ErrorAction SilentlyContinue
        Say "  state removed: $d" Yellow
    }
}

Say ''
Say 'Clean. You can run the installer now.' Green
