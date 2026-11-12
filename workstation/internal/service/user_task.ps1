$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if ($identity.IsSystem -or (Get-Process -Id $PID).SessionId -eq 0) {
    throw 'Run this command inside the signed-in Windows user session.'
}
$name = "$($env:AIE_TASK_SERVICE)-UserSession-$($identity.User.Value)"
$task = Get-ScheduledTask -TaskPath '\' -TaskName $name -ErrorAction SilentlyContinue
function Stop-UserTask {
    if ($task.State -eq 'Running') {
        Stop-ScheduledTask -TaskName $name
        $until = [DateTime]::UtcNow.AddSeconds(20)
        while ((Get-ScheduledTask -TaskName $name).State -eq 'Running') {
            if ([DateTime]::UtcNow -gt $until) { throw 'Timed out while stopping the AEW task.' }
            Start-Sleep -Milliseconds 200
        }
    }
    # Scheduler can finish the launcher while leaving its native child alive.
    # Match the recorded owner, executable and creation time before stopping it.
    if ($task.Actions.Arguments -match '-DataRoot "(?<root>[^"]+)"') {
        $recordPath = Join-Path $Matches['root'] 'data\aew-login-process.json'
        if (Test-Path -LiteralPath $recordPath) {
            $record = Get-Content -LiteralPath $recordPath -Raw | ConvertFrom-Json
            $child = Get-Process -Id $record.ProcessId -ErrorAction SilentlyContinue
            if ($child -and $record.UserSid -eq $identity.User.Value -and
                $child.StartTime.ToUniversalTime().ToString('o') -eq $record.StartedAt) {
                $process = Get-CimInstance Win32_Process -Filter "ProcessId=$($record.ProcessId)"
                $owner = Invoke-CimMethod -InputObject $process -MethodName GetOwnerSid
                if ($owner.Sid -ne $identity.User.Value -or $process.ExecutablePath -ine $record.Binary -or
                    $process.CommandLine -notmatch '(?:^|\s)daemon(?:\s|$)') {
                    throw 'The daemon process no longer matches the installed task.'
                }
                Stop-Process -Id $child.Id -Force
                Wait-Process -Id $child.Id -Timeout 15 -ErrorAction SilentlyContinue
            }
        }
    }
}
switch ($env:AIE_TASK_OPERATION) {
    'install' {
        $exe = $env:AIE_TASK_BINARY
        $dataRoot = $env:AIE_DATA_DIR
        if (!$dataRoot) { $dataRoot = Join-Path ([Environment]::GetFolderPath('UserProfile')) '.aie' }
        $dataRoot = [IO.Path]::GetFullPath($dataRoot)
        $launcherDir = Join-Path $dataRoot 'bin'
        New-Item -ItemType Directory -Path $launcherDir -Force | Out-Null
        $launcher = Join-Path $launcherDir 'aew-user-daemon.ps1'
        # Windows PowerShell needs a UTF-8 BOM for source containing Unicode.
        $launcherBody = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:AIE_TASK_LAUNCHER))
        [IO.File]::WriteAllText($launcher, $launcherBody, [Text.UTF8Encoding]::new($true))
        $powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
        $arguments = '-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "{0}" -Binary "{1}" -DataRoot "{2}\."' -f $launcher, $exe, $dataRoot.TrimEnd('\')
        $action = New-ScheduledTaskAction -Execute $powershell -Argument $arguments -WorkingDirectory (Split-Path -Parent $exe)
        $principal = New-ScheduledTaskPrincipal -UserId $identity.Name -LogonType Interactive -RunLevel Limited
        $trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity.Name
        $settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
        $legacy = Get-Service -Name $env:AIE_TASK_SERVICE -ErrorAction SilentlyContinue
        if ($legacy -and ($legacy.StartType -ne 'Disabled' -or $legacy.Status -ne 'Stopped')) {
            Set-Service -Name $legacy.Name -StartupType Disabled
            Stop-Service -Name $legacy.Name
            $legacy.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(30))
        }
        Register-ScheduledTask -TaskPath '\' -TaskName $name -Action $action -Principal $principal -Trigger $trigger -Settings $settings -Force | Out-Null
    }
    'status' {
        if (!$task) { '当前用户未安装 AEW 登录任务'; break }
        "$name $($task.State) $($task.Principal.UserId)"
    }
    default {
        if (!$task) { throw 'AEW logon task is missing. Run aew service install in the signed-in user session.' }
        switch ($env:AIE_TASK_OPERATION) {
            'uninstall' { Stop-UserTask; Unregister-ScheduledTask -TaskName $name -Confirm:$false }
            'stop' { Stop-UserTask }
            'restart' { Stop-UserTask; Start-ScheduledTask -TaskName $name }
            'start' { if ($task.State -ne 'Running') { Start-ScheduledTask -TaskName $name } }
            default { throw 'Unknown AEW task operation.' }
        }
    }
}
