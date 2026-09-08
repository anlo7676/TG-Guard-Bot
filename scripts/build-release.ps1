#requires -Version 7.0
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
if ((go version) -notmatch 'go1\.26\.7\s') { throw 'This project requires Go 1.26.7.' }
$previous = @{}
foreach ($key in @('CGO_ENABLED','GOOS','GOARCH','GOCACHE')) { $previous[$key] = [Environment]::GetEnvironmentVariable($key, 'Process') }
try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'linux'
    $env:GOCACHE = Join-Path $PWD 'tmp/go-build'
    New-Item -ItemType Directory -Force dist | Out-Null
    $source = git rev-parse --short=12 HEAD
    $builtAt = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    foreach ($arch in @('amd64','arm64')) {
        $env:GOARCH = $arch
        go build -trimpath -ldflags "-s -w -X tgguard/internal/buildinfo.Source=$source -X tgguard/internal/buildinfo.BuiltAt=$builtAt" -o "dist/tgguard-linux-$arch" ./cmd/tgguard
        if ($LASTEXITCODE -ne 0) { throw "Build failed: $arch" }
    }
    $lines = foreach ($arch in @('amd64','arm64')) {
        $name = "tgguard-linux-$arch"
        "$((Get-FileHash -LiteralPath (Join-Path 'dist' $name) -Algorithm SHA256).Hash.ToLowerInvariant())  $name"
    }
    [IO.File]::WriteAllText((Join-Path $PWD 'dist/SHA256SUMS'), ($lines -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
    Write-Output 'Linux amd64 and arm64 binaries built in dist/.'
} finally {
    foreach ($key in $previous.Keys) { [Environment]::SetEnvironmentVariable($key, $previous[$key], 'Process') }
}
