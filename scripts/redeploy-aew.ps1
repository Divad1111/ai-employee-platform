param(
    [string]$Binary = (Join-Path (Split-Path -Parent $PSScriptRoot) 'workstation\bin\aew.exe'),
    [string]$Build = (Join-Path (Split-Path -Parent $PSScriptRoot) 'bin\aew-new.exe')
)
$ErrorActionPreference = 'Stop'
$target = (Get-Item -LiteralPath $Binary).FullName
$source = (Get-Item -LiteralPath $Build).FullName
if ($source -eq $target) { throw 'Source and installed executable must differ.' }
$backup = "$target.before-$(Get-Date -Format yyyyMMdd-HHmmssfff)"
Copy-Item -LiteralPath $target -Destination $backup
$legacy = Get-CimInstance Win32_Service -Filter "Name='AIEmployeeWorkstation'"
if ($legacy -and $legacy.State -eq 'Running') {
    if ($legacy.PathName -notmatch '^(?:"(?<exe>[^"]+)"|(?<exe>.*?\.exe))(?:\s|$)') { throw 'Unexpected legacy service path.' }
    $registered = Get-Item -LiteralPath $Matches['exe']
    $serviceTarget = if ($registered.LinkType -eq 'SymbolicLink') { $registered.Target } else { $registered.FullName }
    if ($serviceTarget -ne $target) { throw 'Legacy service belongs to a different installation.' }
    Set-Service -Name $legacy.Name -StartupType Disabled
    Stop-Service -Name $legacy.Name
    (Get-Service $legacy.Name).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(30))
} else {
    & $source service stop
    if ($LASTEXITCODE -ne 0) { throw 'Cannot stop the installed user task.' }
}
# Task Scheduler can report Ready while Windows still holds the old image.
$deadline = [DateTime]::UtcNow.AddSeconds(20)
do {
    try {
        $handle = [IO.File]::Open($target, 'Open', 'ReadWrite', 'None')
        $handle.Dispose()
        break
    } catch [IO.IOException] {
        if ([DateTime]::UtcNow -ge $deadline) { throw }
        Start-Sleep -Milliseconds 250
    }
} while ($true)
$replaced = $false
try {
    Copy-Item -LiteralPath $source -Destination $target -Force
    $replaced = $true
    if ((Get-FileHash -LiteralPath $target).Hash -ne (Get-FileHash -LiteralPath $source).Hash) { throw 'Installed binary hash mismatch.' }
    & $target service install
    if ($LASTEXITCODE -ne 0) { throw 'Cannot register the user logon task.' }
    & $target service start
    if ($LASTEXITCODE -ne 0) { throw 'Cannot start the user logon task.' }
} catch {
    if ($replaced) { Copy-Item -LiteralPath $backup -Destination $target -Force }
    # Always use the user task, including when rolling back to an old executable.
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $task = Get-ScheduledTask -TaskName "AIEmployeeWorkstation-UserSession-$sid" -ErrorAction SilentlyContinue
    if ($task) { Start-ScheduledTask -InputObject $task }
    throw
}
