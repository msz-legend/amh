# amh installer (Windows): iwr https://raw.githubusercontent.com/<owner>/amh/main/scripts/install.ps1 -useb | iex
$ErrorActionPreference = 'Stop'
$Repo = if ($env:AMH_REPO) { $env:AMH_REPO } else { 'msz-legend/amh' }
$Version = if ($env:AMH_VERSION) { $env:AMH_VERSION } else { 'latest' }
$InstallDir = if ($env:AMH_INSTALL_DIR) { $env:AMH_INSTALL_DIR } else { Join-Path $HOME '.amh\bin' }

New-Item -ItemType Directory -Force $InstallDir | Out-Null
$dest = Join-Path $InstallDir 'amh.exe'

if ($env:AMH_BUILD_FROM_SOURCE -eq '1' -and (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host 'amh install: building from source...'
    $tmp = New-Item -ItemType Directory -Force (Join-Path $env:TEMP ('amh-src-' + [guid]::NewGuid()))
    git clone --depth 1 "https://github.com/$Repo" $tmp.FullName
    Push-Location $tmp.FullName
    go build -o $dest ./cmd/amh
    Pop-Location
    Remove-Item -Recurse -Force $tmp
}
else {
    if ($Version -eq 'latest') {
        $rel = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
        $Version = $rel.tag_name
    }
    $url = "https://github.com/$Repo/releases/download/$Version/amh_${Version}_windows_amd64.zip"
    Write-Host "amh install: fetching $url"
    $zip = Join-Path $env:TEMP 'amh-install.zip'
    Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
    Expand-Archive -Path $zip -DestinationPath $InstallDir -Force
    Remove-Item $zip
}

Write-Host "amh installed: $dest"
Write-Host "next: $dest connect --agent claude-code; $dest connect --agent codex"
if (($env:PATH -split ';') -notcontains $InstallDir) {
    Write-Host "note: add $InstallDir to PATH"
}
