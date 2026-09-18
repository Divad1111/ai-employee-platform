# M2 mTLS / 登录验收脚本（PowerShell）
# 用法：先启动 server，再执行本脚本。
# 设计依据：T-0209

$ErrorActionPreference = "Stop"
$base = if ($env:AIE_HTTP) { $env:AIE_HTTP } else { "http://127.0.0.1:8080" }
$grpc = if ($env:AIE_GRPC) { $env:AIE_GRPC } else { "127.0.0.1:9090" }
$data = Join-Path $env:TEMP ("aie-m2-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Force -Path $data | Out-Null
$env:AIE_DATA_DIR = $data

Write-Host "== login =="
$login = Invoke-RestMethod -Method Post -Uri "$base/api/auth/login" -ContentType "application/json" -Body '{"username":"admin","password":"admin123"}'
$token = $login.token
if (-not $token) { throw "登录失败" }

Write-Host "== create enrollment token =="
$headers = @{ Authorization = "Bearer $token" }
$en = Invoke-RestMethod -Method Post -Uri "$base/api/enrollment/tokens" -Headers $headers -ContentType "application/json" -Body '{"label":"m2-verify"}'
$plain = $en.token

Write-Host "== aew register =="
$aew = Join-Path $PSScriptRoot "..\workstation\bin\aew.exe"
if (-not (Test-Path $aew)) { $aew = "aew" }
& $aew register --server $base --token $plain
if ($LASTEXITCODE -ne 0) { throw "register 失败" }

Write-Host "== aew ping =="
& $aew ping --grpc $grpc
if ($LASTEXITCODE -ne 0) { throw "ping 失败" }

Write-Host "M2 verify OK"
