param([switch]$Race)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$env:GOCACHE = Join-Path $projectRoot 'tmp\go-build'
go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if ($Race) { go test -race -count=1 ./... } else { go test -count=1 ./... }
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& (Join-Path $PSScriptRoot 'build.ps1')
