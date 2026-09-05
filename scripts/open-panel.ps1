param([string]$BaseURL = 'http://127.0.0.1:8080', [switch]$PrintOnly)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$configPath = Join-Path $projectRoot '.env'
if (-not (Test-Path -LiteralPath $configPath)) { throw 'Missing .env configuration.' }
$entry = Get-Content -LiteralPath $configPath -Encoding UTF8 | Where-Object { $_ -match '^ADMIN_API_TOKEN=' } | Select-Object -First 1
if (-not $entry) { throw 'ADMIN_API_TOKEN is not configured.' }
$token = $entry.Substring('ADMIN_API_TOKEN='.Length).Trim().Trim('"').Trim("'")
$result = Invoke-RestMethod -Method Post -Uri ($BaseURL.TrimEnd('/') + '/api/v1/panel-ticket') -Headers @{ Authorization = 'Bearer ' + $token } -ContentType 'application/json' -Body '{}' -TimeoutSec 10
$url = $BaseURL.TrimEnd('/') + '/#ticket=' + $result.ticket
if ($PrintOnly) { Write-Output $url } else { Start-Process $url }
