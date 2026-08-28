<#
.SYNOPSIS
    Manages the automatic check (the scheduled tasks).

.DESCRIPTION
    Everything that modifies the system lives in scripts like this one rather
    than inside an executable. That keeps it readable, and it keeps antivirus
    heuristics calm: creating scheduled tasks and writing autostart entries from
    a compiled binary looks exactly like malware digging itself in. Doing the
    same thing from a visible PowerShell script does not.

    The tray runs this elevated, so the user sees one UAC prompt per action
    instead of one per check.

.PARAMETER Action
    Install - create the tasks, Remove - delete them,
    Enable / Disable - turn them on or off without deleting,
    Status - report the current state (the only action that needs no admin).

.PARAMETER GuardExe
    Full path to the guard executable the tasks should run.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('Install', 'Remove', 'Enable', 'Disable', 'Status')]
    [string] $Action,

    [string] $GuardExe = (Join-Path $env:ProgramFiles 'Plex External Audio\Plex External Audio Guard.exe')
)

$ErrorActionPreference = 'Stop'

# These names must match the constants in cmd/guard/settings.go and
# cmd/tray/main_windows.go. Nothing checks that at build time - a mismatch just
# quietly means the tray can no longer find the task it registered.
$quick = 'Plex External Audio'
$full  = 'Plex External Audio (logon)'

# The task wakes the guard on a fixed, frequent schedule; how often it actually
# does any work is decided by the settings file in the user's profile. That way
# changing the interval does not mean re-registering the task, which would need
# administrator rights every single time.
$everyMinutes = 5

function Assert-Admin {
    $ok = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()
          ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $ok) { throw 'administrator rights are required' }
}

function Get-State($name) {
    $t = Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
    if (-not $t) { return 'absent' }
    return $t.State.ToString()
}

switch ($Action) {

    'Status' {
        [pscustomobject]@{
            Quick = Get-State $quick
            Full  = Get-State $full
        } | Format-List | Out-String | Write-Host
    }

    'Install' {
        Assert-Admin
        if (-not (Test-Path -LiteralPath $GuardExe)) { throw "not found: $GuardExe" }

        # Run as the CURRENT USER, elevated - not as SYSTEM. The guard and the
        # tray exchange state through %LOCALAPPDATA%, and under SYSTEM that
        # resolves to a different directory: the pause flag written by the
        # tray's Exit would never be seen, and the check interval would be
        # ignored. This is load-bearing, do not "simplify" it to SYSTEM.
        $me        = "$env:USERDOMAIN\$env:USERNAME"
        $principal = New-ScheduledTaskPrincipal -UserId $me -LogonType Interactive -RunLevel Highest
        $settings  = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
                        -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Hours 2)

        Register-ScheduledTask -TaskName $quick -Force -Principal $principal -Settings $settings `
            -Action  (New-ScheduledTaskAction -Execute $GuardExe) `
            -Trigger (New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) `
                        -RepetitionInterval (New-TimeSpan -Minutes $everyMinutes)) `
            -Description 'Restores the transcoder wrapper after a Plex update' | Out-Null
        Write-Host "  task created: $quick (every $everyMinutes min)" -ForegroundColor Green

        # The full run maps the whole library and wants Plex stopped, so it only
        # happens at sign-in. The delay lets the desktop finish coming up first.
        $tFull = New-ScheduledTaskTrigger -AtLogOn -User $me
        $tFull.Delay = 'PT3M'
        Register-ScheduledTask -TaskName $full -Force -Principal $principal -Settings $settings `
            -Action (New-ScheduledTaskAction -Execute $GuardExe -Argument '-full') -Trigger $tFull `
            -Description 'Restores external audio tracks in the Plex database' | Out-Null
        Write-Host "  task created: $full (at sign-in, +3 min)" -ForegroundColor Green
    }

    'Remove' {
        Assert-Admin
        foreach ($n in @($quick, $full)) {
            if (Get-ScheduledTask -TaskName $n -ErrorAction SilentlyContinue) {
                Unregister-ScheduledTask -TaskName $n -Confirm:$false
                Write-Host "  task removed: $n" -ForegroundColor Yellow
            }
        }
    }

    'Enable' {
        Assert-Admin
        foreach ($n in @($quick, $full)) {
            if (Get-ScheduledTask -TaskName $n -ErrorAction SilentlyContinue) {
                Enable-ScheduledTask -TaskName $n | Out-Null
                Write-Host "  enabled: $n" -ForegroundColor Green
            } else {
                Write-Host "  no such task: $n - run Install first" -ForegroundColor Yellow
            }
        }
    }

    'Disable' {
        Assert-Admin
        foreach ($n in @($quick, $full)) {
            if (Get-ScheduledTask -TaskName $n -ErrorAction SilentlyContinue) {
                Disable-ScheduledTask -TaskName $n | Out-Null
                Write-Host "  disabled: $n" -ForegroundColor Yellow
            }
        }
    }
}
