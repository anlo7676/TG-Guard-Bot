param()
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
try {
    if ((go version) -notmatch 'go1\.26\.7\s') { throw 'This project requires Go 1.26.7.' }
    New-Item -ItemType Directory -Force bin,tmp/go-build | Out-Null
    $env:GOCACHE = Join-Path $projectRoot 'tmp/go-build'
    $sourceFiles = @('go.mod','go.sum') + @(Get-ChildItem -LiteralPath cmd,internal -Recurse -File | ForEach-Object { $_.FullName })
    $fingerprintText = ($sourceFiles | Sort-Object | ForEach-Object { (Get-FileHash -LiteralPath $_ -Algorithm SHA256).Hash }) -join ''
    $sourceHash = ([BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($fingerprintText))) -replace '-','').Substring(0,12).ToLowerInvariant()
    $builtAt = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    go build -trimpath -ldflags "-X tgguard/internal/buildinfo.Source=$sourceHash -X tgguard/internal/buildinfo.BuiltAt=$builtAt" -o bin/tgguard-next.exe ./cmd/tgguard
    if ($LASTEXITCODE -ne 0) { throw 'Build failed; running binary was not changed.' }
    @{source=$sourceHash;built_at=$builtAt} | ConvertTo-Json | Set-Content -LiteralPath bin/build.json -Encoding UTF8
    Write-Output "Built source $sourceHash ($builtAt)"
} finally { Pop-Location }
