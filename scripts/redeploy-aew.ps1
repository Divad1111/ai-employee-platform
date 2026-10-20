param(
    [string]$ServiceName = 'AIEmployeeWorkstation'
)

$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$taskSource = Join-Path $taskRoot 'bin\aew-new.exe'
$taskTarget = Join-Path $taskRoot 'workstation\bin\aew.exe'
$taskCLI = Join-Path $taskRoot 'bin\aew.exe'
$taskBackup = Join-Path $taskRoot 'bin\aew-before-redeploy.exe'
$taskResult = Join-Path $taskRoot 'bin\aew-redeploy-result.json'
$taskName = 'AIEmployeeWorkstation-UserSession'
$taskLauncher = Join-Path $taskRoot 'scripts\start-aew-user-session.ps1'
$taskUserSession = $false
$taskWasStopped = $false
$taskReplaced = $false
$taskReport = @{ success = $false; service = $ServiceName; target = $taskTarget; backup = $taskBackup }

function Get-WorkspaceDaemon {
    @(Get-CimInstance Win32_Process -Filter "Name='aew.exe'" | Where-Object {
        $_.SessionId -gt 0 -and $_.ExecutablePath -and
        [StringComparer]::OrdinalIgnoreCase.Equals($_.ExecutablePath, $taskTarget) -and
        $_.CommandLine -match '(?i)(?:^|\s)daemon(?:\s|$)'
    })
}

function Stop-WorkspaceDaemon {
    if ($taskUserSession) {
        Stop-ScheduledTask -TaskName $taskName
        foreach ($taskDaemon in (Get-WorkspaceDaemon)) {
            Stop-Process -Id $taskDaemon.ProcessId -Force -ErrorAction SilentlyContinue
            Wait-Process -Id $taskDaemon.ProcessId -Timeout 10 -ErrorAction SilentlyContinue
        }
        if (@(Get-WorkspaceDaemon).Count -gt 0) { throw 'Workspace daemon did not stop' }
    } else {
        Stop-Service -Name $ServiceName
        (Get-Service -Name $ServiceName).WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
}

function Start-WorkspaceDaemon {
    if ($taskUserSession) {
        Start-ScheduledTask -TaskName $taskName
        $taskDeadline = [DateTime]::UtcNow.AddSeconds(15)
        do {
            if ((Get-ScheduledTask -TaskName $taskName).State -eq 'Running' -and @(Get-WorkspaceDaemon).Count -eq 1) { return }
            Start-Sleep -Milliseconds 250
        } while ([DateTime]::UtcNow -lt $taskDeadline)
        throw 'AEW did not start in the logged-in user session'
    } else {
        Start-Service -Name $ServiceName
        (Get-Service -Name $ServiceName).WaitForStatus([ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30))
    }
}

try {
    if (-not (Test-Path -LiteralPath $taskSource -PathType Leaf)) { throw "Build missing: $taskSource" }
    $taskService = Get-CimInstance Win32_Service -Filter "Name='$ServiceName'"
    if ($null -eq $taskService) { throw "Service not found: $ServiceName" }
    if ($taskService.PathName -notmatch '^(?:"(?<exe>[^"]+)"|(?<exe>.*?\.exe))(?:\s|$)') { throw 'Cannot resolve service executable' }
    $taskRegisteredPath = [IO.Path]::GetFullPath($Matches['exe'])
    if (-not [StringComparer]::OrdinalIgnoreCase.Equals($taskRegisteredPath, $taskTarget)) {
        throw "Service executable does not match the verified workspace target: $taskRegisteredPath"
    }
    $taskScheduled = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($taskScheduled) {
        $taskArguments = "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$taskLauncher`""
        if (@($taskScheduled.Actions).Count -ne 1 -or
            $taskScheduled.Actions.Arguments -ne $taskArguments -or
            $taskScheduled.Actions.WorkingDirectory -ne $taskRoot -or
            $taskScheduled.Principal.LogonType -ne 'Interactive') {
            throw 'Scheduled task does not match the verified workspace launcher'
        }
        if ($taskService.State -ne 'Stopped') { throw 'Stop the service before using the user-session launcher' }
        $taskUserSession = $true
        $taskReport.mode = 'user-session'
        $taskReport.task = $taskName
    } else { $taskReport.mode = 'service' }

    $taskExpectedHash = (Get-FileHash -LiteralPath $taskSource -Algorithm SHA256).Hash
    Copy-Item -LiteralPath $taskTarget -Destination $taskBackup -Force
    $taskWasStopped = $true
    Stop-WorkspaceDaemon
    $taskReplaced = $true
    Copy-Item -LiteralPath $taskSource -Destination $taskTarget -Force
    Copy-Item -LiteralPath $taskSource -Destination $taskCLI -Force
    if ((Get-FileHash -LiteralPath $taskTarget -Algorithm SHA256).Hash -ne $taskExpectedHash) { throw 'Installed AEW hash mismatch' }
    Start-WorkspaceDaemon
    $taskReport.success = $true
    $taskReport.sha256 = $taskExpectedHash
    if ($taskUserSession) { $taskReport.status = (Get-ScheduledTask -TaskName $taskName).State.ToString() }
    else { $taskReport.status = (Get-Service -Name $ServiceName).Status.ToString() }
} catch {
    $taskReport.error = $_.Exception.Message
    if ($taskWasStopped) {
        try {
            if ($taskReplaced) {
                Stop-WorkspaceDaemon
                Copy-Item -LiteralPath $taskBackup -Destination $taskTarget -Force
                Copy-Item -LiteralPath $taskBackup -Destination $taskCLI -Force
            }
            Start-WorkspaceDaemon
            $taskReport.rolled_back = $true
        } catch { $taskReport.rollback_error = $_.Exception.Message }
    }
} finally {
    $taskReport | ConvertTo-Json | Set-Content -LiteralPath $taskResult -Encoding UTF8
}
if (-not $taskReport.success) { exit 1 }
