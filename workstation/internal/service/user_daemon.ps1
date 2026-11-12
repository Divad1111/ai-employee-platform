param(
    [Parameter(Mandatory=$true)][string]$Binary,
    [Parameter(Mandatory=$true)][string]$DataRoot
)

$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if ($identity.IsSystem -or (Get-Process -Id $PID).SessionId -eq 0) {
    throw 'AEW requires the signed-in Windows user session.'
}
$DataRoot = [IO.Path]::GetFullPath($DataRoot)
$env:AIE_DATA_DIR = $DataRoot
$logDirectory = Join-Path $DataRoot 'logs'
New-Item -ItemType Directory -Path $logDirectory -Force | Out-Null
$launcherLog = Join-Path $logDirectory 'aew-launcher.log'
$stdout = Join-Path $logDirectory 'aew-daemon.stdout.log'
$stderr = Join-Path $logDirectory 'aew-daemon.stderr.log'
$processRecord = Join-Path $DataRoot 'data\aew-login-process.json'
while ($true) {
    try {
        foreach ($log in @($stdout, $stderr)) {
            if (Test-Path -LiteralPath $log) {
                Copy-Item -LiteralPath $log -Destination "$log.previous" -Force
            }
        }
        $child = Start-Process -FilePath $Binary -ArgumentList 'daemon' `
            -WorkingDirectory (Split-Path -Parent $Binary) -WindowStyle Hidden -PassThru `
            -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        # Keep the process handle open so its exit code remains available.
        $null = $child.Handle
        New-Item -ItemType Directory -Path (Split-Path -Parent $processRecord) -Force | Out-Null
        [pscustomobject]@{
            ProcessId = $child.Id
            StartedAt = $child.StartTime.ToUniversalTime().ToString('o')
            Binary = [IO.Path]::GetFullPath($Binary)
            UserSid = $identity.User.Value
        } | ConvertTo-Json | Set-Content -LiteralPath $processRecord -Encoding UTF8
        Add-Content -LiteralPath $launcherLog -Value ("{0:o} started pid={1} user={2}" -f [DateTime]::Now, $child.Id, $identity.Name)
        $child.WaitForExit()
        Add-Content -LiteralPath $launcherLog -Value ("{0:o} unexpected exit pid={1} code={2}" -f [DateTime]::Now, $child.Id, $child.ExitCode)
    } catch {
        Add-Content -LiteralPath $launcherLog -Value ("{0:o} launcher error: {1}" -f [DateTime]::Now, $_.Exception.Message)
    }
    # Supervise normal exits too. Scheduler failure recovery does not reliably
    # restart a manually started task after its child finishes successfully.
    Start-Sleep -Seconds 5
}
