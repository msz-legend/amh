# amh retrieval benchmark (deterministic). exit 1 on failure.
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')
if (-not (Test-Path .\amh.exe) -and (Get-Command go -ErrorAction SilentlyContinue)) {
    go build -o amh.exe ./cmd/amh
}
& .\amh.exe eval
exit $LASTEXITCODE
