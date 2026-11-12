<#
.SYNOPSIS
为首次安装生成 .env，使用 Windows 内置随机数生成器，不依赖 Python 或 OpenSSL。
.EXAMPLE
.\generate-env.ps1 -Domain employee.example.com -ServerImage aie-control-plane:local -AdminImage aie-admin:local
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Domain,

    [Parameter(Mandatory = $true)]
    [string]$ServerImage,

    [Parameter(Mandatory = $true)]
    [string]$AdminImage,

    [ValidateNotNullOrEmpty()]
    [string]$Output
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# PowerShell 5.1 在参数默认值求值时可能尚未设置 PSScriptRoot，进入脚本后再计算。
if (-not $PSBoundParameters.ContainsKey('Output')) {
    $Output = Join-Path $PSScriptRoot '.env'
}

# 只接受可直接写入 dotenv 的域名/IP 与镜像引用，禁止插值和换行注入。
if ($Domain.Length -gt 253 -or $Domain -notmatch '\A[A-Za-z0-9.-]+\z') {
    throw 'Domain must be a plain DNS hostname or IPv4 address without scheme, port or path.'
}
foreach ($label in $Domain.Split('.')) {
    if ($label.Length -gt 63 -or $label -notmatch '\A[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\z') {
        throw 'Domain contains an invalid hostname label.'
    }
}
foreach ($reference in @($ServerImage, $AdminImage)) {
    if ($reference -notmatch '\A[A-Za-z0-9][A-Za-z0-9._:/@-]*\z') {
        throw 'Invalid image reference: use an image name with a tag or digest.'
    }
}

$outputPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Output)
$utf8 = New-Object System.Text.UTF8Encoding($false, $true)
$template = [IO.File]::ReadAllText((Join-Path $PSScriptRoot '.env.example'), $utf8)
$values = @{
    AIE_DOMAIN = $Domain
    AIE_PUBLIC_SERVER_URL = 'https://' + $Domain
    AIE_CONTROL_PLANE_IMAGE = $ServerImage
    AIE_ADMIN_IMAGE = $AdminImage
}

# 四次独立取样，每项 32 随机字节编码为 64 位十六进制文本；不输出秘密。
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
try {
    foreach ($key in @('POSTGRES_PASSWORD', 'AIE_SECRET_MASTER_KEY', 'AIE_BACKUP_MASTER_KEY', 'AIE_SETUP_TOKEN')) {
        $randomBytes = New-Object byte[] 32
        $rng.GetBytes($randomBytes)
        $values[$key] = [BitConverter]::ToString($randomBytes).Replace('-', '').ToLowerInvariant()
    }
} finally {
    $rng.Dispose()
}

# 保留模板说明和配置间空行，统一输出 UTF-8（无 BOM）与 LF 换行。
$lines = foreach ($line in [regex]::Split($template.TrimEnd([char[]]"`r`n"), '\r\n|\n|\r')) {
    if ($line -match '^([A-Z][A-Z0-9_]*)=' -and $values.ContainsKey($Matches[1])) {
        $Matches[1] + '=' + $values[$Matches[1]]
    } else {
        $line
    }
}
$content = $utf8.GetBytes(($lines -join "`n") + "`n")

# 创建时即限制 Windows 文件 ACL；CreateNew 原子拒绝覆盖已有文件。
$security = New-Object Security.AccessControl.FileSecurity
$security.SetAccessRuleProtection($true, $false)
$currentSid = [Security.Principal.WindowsIdentity]::GetCurrent().User
foreach ($sid in @($currentSid, [Security.Principal.SecurityIdentifier]::new('S-1-5-18'), [Security.Principal.SecurityIdentifier]::new('S-1-5-32-544'))) {
    $security.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($sid, 'FullControl', 'Allow'))
}
# Windows PowerShell 5.1 和 PowerShell 7 使用不同的 .NET 文件创建接口。
$stream = if ($PSVersionTable.PSEdition -eq 'Desktop') {
    [IO.FileStream]::new($outputPath, [IO.FileMode]::CreateNew, [Security.AccessControl.FileSystemRights]::Write, [IO.FileShare]::None, 4096, [IO.FileOptions]::None, $security)
} else {
    [IO.FileSystemAclExtensions]::Create([IO.FileInfo]::new($outputPath), [IO.FileMode]::CreateNew, [Security.AccessControl.FileSystemRights]::Write, [IO.FileShare]::None, 4096, [IO.FileOptions]::None, $security)
}
try {
    $stream.Write($content, 0, $content.Length)
} finally {
    $stream.Dispose()
}
Write-Output ('Created private environment file: ' + $outputPath)
