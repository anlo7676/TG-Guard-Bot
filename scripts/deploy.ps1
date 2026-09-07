#requires -Version 7.0
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw '请先安装并启动 Docker Desktop（Linux 容器模式）。' }
docker compose version | Out-Null
if ($LASTEXITCODE -ne 0) { throw '需要 Docker Compose v2。' }
docker info 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Docker 未启动或当前用户无访问权限。' }
function New-Secret([int]$Bytes = 32) {
    return [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes($Bytes)).ToLowerInvariant()
}
if (-not (Test-Path -LiteralPath .env)) {
    $token = Read-Host '请输入 Bot Token（输入隐藏）' -MaskInput
    if ($token -notmatch '^\d+:[A-Za-z0-9_-]{30,}$') { throw 'Bot Token 格式不正确。' }
    $content = @(
        "BOT_TOKEN=$token", 'BOT_MODE=polling', "ADMIN_API_TOKEN=$(New-Secret)",
        "SETTINGS_ENCRYPTION_KEY=$(New-Secret)", "MYSQL_PASSWORD=$(New-Secret 24)",
        "MYSQL_ROOT_PASSWORD=$(New-Secret 24)", "REDIS_PASSWORD=$(New-Secret 24)", 'REDIS_DB=0'
    ) -join "`n"
    $stream = [IO.File]::Open((Join-Path $PWD '.env'), [IO.FileMode]::CreateNew)
    try {
        $bytes = [Text.UTF8Encoding]::new($false).GetBytes($content + "`n")
        $stream.Write($bytes, 0, $bytes.Length)
    } finally { $stream.Dispose() }
    $token = $null
    $content = $null
    Write-Host '已生成 .env；请备份此文件，更新时会保留原有凭据。'
}
$settings = @{}
foreach ($line in Get-Content -LiteralPath .env -Encoding UTF8) {
    if ($line -match '^([A-Z_]+)=(.*)$') { $settings[$Matches[1]] = $Matches[2].Trim().Trim('"').Trim("'") }
}
$previous = @{}
try {
    foreach ($key in @('BOT_TOKEN','ADMIN_API_TOKEN','MYSQL_PASSWORD','MYSQL_ROOT_PASSWORD','REDIS_PASSWORD')) {
        $value = $settings[$key]
        if (-not $value -or $value -like 'change-*') { throw ".env 中 $key 未配置；已保留原文件，请补齐后重试。" }
        if ($key -eq 'ADMIN_API_TOKEN' -and $value.Length -lt 32) { throw 'ADMIN_API_TOKEN 至少需要 32 个字符。' }
        $previous[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
        [Environment]::SetEnvironmentVariable($key, $value, 'Process')
    }
    Write-Host '正在构建并启动服务，首次拉取镜像可能需要几分钟……'
    docker compose --env-file .env up -d --build --wait --wait-timeout 300
    if ($LASTEXITCODE -ne 0) { throw '部署未就绪。请运行 docker compose logs --tail 50 app 查看错误；已有配置和数据保留。' }
    & (Join-Path $PSScriptRoot 'open-panel.ps1')
    Write-Host '部署完成，已打开后台。在网页设置机器人管理员、批准群组；AI 接口按需填写。'
} finally {
    foreach ($key in $previous.Keys) { [Environment]::SetEnvironmentVariable($key, $previous[$key], 'Process') }
}
