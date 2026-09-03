$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$Domain = "tunnel.swag.best"
$BaseUrl = "https://$Domain"

if ([Environment]::Is64BitProcess) {
    $Arch = "amd64"
} else {
    $Arch = "386"
}

$InstallDir = Join-Path $env:LOCALAPPDATA "dreamtunnel"
$Binary = "dt-windows-$Arch.exe"
$Url = "$BaseUrl/releases/$Binary"
$ExePath = Join-Path $InstallDir "dt.exe"

Write-Host ""
Write-Host "  [+] dreamTunnel Installer" -ForegroundColor Cyan
Write-Host "  [+] Platform: windows/$Arch" -ForegroundColor Cyan

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

Write-Host "  [+] Downloading: $Url" -ForegroundColor Cyan
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -Uri $Url -OutFile $ExePath -UseBasicParsing
} catch {
    Write-Host "[-] Failed to download: $_" -ForegroundColor Red
    exit 1
}

$currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($currentPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$currentPath;$InstallDir", "User")
    $env:PATH = "$env:PATH;$InstallDir"
    Write-Host "  [+] Added to user PATH: $InstallDir" -ForegroundColor Green
    Write-Host "  [+] Open a NEW terminal to use dt" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "  [+] Installed to: $ExePath" -ForegroundColor Green
Write-Host ""
Write-Host "  Usage:" -ForegroundColor Cyan
Write-Host "      dt http 8080              expose local HTTP server"
Write-Host "      dt http 8080 my-site      custom subdomain -> https://my-site.maiuni.online"
Write-Host "      dt tcp 25565              raw TCP tunnel"
Write-Host ""
Write-Host "  Docs: https://$Domain/" -ForegroundColor Cyan
Write-Host ""
