param([string]$Binary = (Join-Path (Split-Path -Parent $PSScriptRoot) 'workstation\bin\aew.exe'))
$ErrorActionPreference = 'Stop'
$exe = (Get-Item -LiteralPath $Binary).FullName
& $exe service install
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& $exe service start
exit $LASTEXITCODE
