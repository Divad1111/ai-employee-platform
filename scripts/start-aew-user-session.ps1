# Codex's local tools need the logged-in user's Windows execution session.
$ErrorActionPreference = 'Stop'
$taskRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$taskExe = Join-Path $taskRoot 'workstation\bin\aew.exe'
if (-not (Test-Path -LiteralPath $taskExe -PathType Leaf)) { throw 'AEW executable is missing' }
$taskSession = (Get-Process -Id $PID).SessionId
if ($taskSession -eq 0) { throw 'Run AEW from the logged-in user session, not Windows service session 0' }
$taskExisting = Get-CimInstance Win32_Process -Filter "Name='aew.exe'" | Where-Object {
    $_.SessionId -eq $taskSession -and $_.ExecutablePath -and
    [StringComparer]::OrdinalIgnoreCase.Equals($_.ExecutablePath, $taskExe) -and
    $_.CommandLine -match '(?i)(?:^|\s)daemon(?:\s|$)'
} | Select-Object -First 1
if ($taskExisting) {
    $taskChild = Get-Process -Id $taskExisting.ProcessId
} else {
    $taskChild = Start-Process -FilePath $taskExe -ArgumentList 'daemon' -WorkingDirectory $taskRoot -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $taskRoot 'bin\aew-user-session.stdout.log') `
        -RedirectStandardError (Join-Path $taskRoot 'bin\aew-user-session.stderr.log')
}
$taskChild.WaitForExit()
if ($null -eq $taskChild.ExitCode -or $taskChild.ExitCode -ne 0) { exit 1 }
exit 0
