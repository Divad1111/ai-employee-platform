$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$taskExe = Join-Path $taskRoot 'workstation\bin\aew.exe'
$taskLauncher = Join-Path $taskRoot 'scripts\start-aew-user-session.ps1'
$taskService = Get-CimInstance Win32_Service -Filter "Name='AIEmployeeWorkstation'"
if ($null -eq $taskService -or $taskService.PathName -notmatch '^(?:"(?<exe>[^"]+)"|(?<exe>.*?\.exe))(?:\s|$)') { throw 'Cannot resolve AEW service' }
if (-not [StringComparer]::OrdinalIgnoreCase.Equals([IO.Path]::GetFullPath($Matches['exe']), $taskExe)) { throw 'AEW service executable does not match workspace' }
if ($taskService.State -ne 'Stopped') { throw 'Stop the verified AEW service before configuring the user-session launch' }
$taskUser = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$taskName = 'AIEmployeeWorkstation-UserSession'
$taskPrevious = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($taskPrevious -and -not ($taskPrevious.Actions.Arguments -like "*$taskLauncher*")) { throw 'A different task already uses this name' }
$taskAction = New-ScheduledTaskAction -Execute (Join-Path $PSHOME 'powershell.exe') `
    -Argument "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$taskLauncher`"" -WorkingDirectory $taskRoot
$taskPrincipal = New-ScheduledTaskPrincipal -UserId $taskUser -LogonType Interactive -RunLevel Limited
$taskTrigger = New-ScheduledTaskTrigger -AtLogOn -User $taskUser
$taskSettings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -MultipleInstances IgnoreNew -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName $taskName -Action $taskAction -Principal $taskPrincipal -Trigger $taskTrigger `
    -Settings $taskSettings -Description 'Run AEW in the logged-in user session so Codex native execution and file tools work.' -Force | Out-Null
Set-Service -Name AIEmployeeWorkstation -StartupType Manual
Start-ScheduledTask -TaskName $taskName
@{ success=$true; task=$taskName; user=$taskUser; serviceStartMode='Manual'; launcher=$taskLauncher } |
    ConvertTo-Json | Set-Content -LiteralPath (Join-Path $taskRoot 'bin\aew-user-session-configured.json') -Encoding UTF8
