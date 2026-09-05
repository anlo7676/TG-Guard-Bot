param([switch]$Stop)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $projectRoot 'bin/tgguard.exe'
$pidFile = Join-Path $projectRoot 'tmp/tgguard.pid'
# Build must finish successfully before stopping a healthy running service.
if (-not $Stop) { & (Join-Path $PSScriptRoot 'build.ps1') }
$existing = @(Get-Process -Name tgguard -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $binary })
foreach ($process in $existing) { Stop-Process -Id $process.Id; if (-not $process.WaitForExit(10000)) { throw 'Service did not stop.' } }
if ($Stop) { Write-Output 'Local TG Guard stopped.'; return }
Copy-Item -LiteralPath $binary -Destination (Join-Path $projectRoot 'bin/tgguard-previous.exe') -Force -ErrorAction SilentlyContinue
Copy-Item -LiteralPath (Join-Path $projectRoot 'bin/tgguard-next.exe') -Destination $binary -Force
foreach ($line in Get-Content -LiteralPath (Join-Path $projectRoot '.env') -Encoding UTF8) {
 if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') {
  [Environment]::SetEnvironmentVariable($Matches[1],$Matches[2].Trim().Trim('"').Trim("'"),'Process')
 }
}
$p = Start-Process -FilePath $binary -WorkingDirectory $projectRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $projectRoot 'tmp/tgguard.stdout.log') -RedirectStandardError (Join-Path $projectRoot 'tmp/tgguard.stderr.log') -PassThru
$p.Id | Set-Content -LiteralPath $pidFile -Encoding UTF8
$address = $env:HTTP_ADDR
if (-not $address) { $address = '127.0.0.1:8080' }
$address = $address -replace '^0\.0\.0\.0:', '127.0.0.1:' -replace '^:', '127.0.0.1:'
$baseURL = 'http://' + $address
$expected = Get-Content -LiteralPath (Join-Path $projectRoot 'bin/build.json') -Encoding UTF8 | ConvertFrom-Json
for ($attempt=0; $attempt -lt 30; $attempt++) {
 if ($p.HasExited) { throw 'Service exited. Inspect tmp/tgguard.stdout.log.' }
 try {
  $ready = Invoke-RestMethod "$baseURL/health/ready" -TimeoutSec 2
  $live = Invoke-RestMethod "$baseURL/health/live" -TimeoutSec 2
  if ($ready.status -eq 'ready' -and $live.build.source -eq $expected.source) {
   Write-Output "Ready: $baseURL ; PID $($p.Id) ; source $($live.build.source)"
   return
  }
 } catch {}
 Start-Sleep -Milliseconds 500
}
throw 'Readiness/build verification failed. Inspect tmp/tgguard.stdout.log; previous binary is retained in bin/tgguard-previous.exe.'
