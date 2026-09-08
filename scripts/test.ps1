param([switch]$Race)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$env:GOCACHE = Join-Path $projectRoot 'tmp\go-build'
node --check internal/api/web/app.js
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
node --test scripts/web-authorization.test.cjs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
node --test scripts/deploy.test.cjs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
node --test scripts/install.test.cjs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
node --test scripts/manage.test.cjs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if ($Race) { go test -race -count=1 ./... } else { go test -count=1 ./... }
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& (Join-Path $PSScriptRoot 'build.ps1')
