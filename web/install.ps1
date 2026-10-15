# dreamTunnel installer for Windows.
#
#   irm https://tunnel.swag.best/install.ps1 | iex
#
# There is nothing to configure afterwards: the built-in token registers this
# device with the relay on the first run and stores the token it gets back, so
# `dt http 8080` works immediately.
$ErrorActionPreference = "Stop"

$Domain = "tunnel.swag.best"
$TunnelDomain = "maiuni.online"
$BaseUrl = "https://$Domain"

if ([Environment]::Is64BitProcess) { $Arch = "amd64" } else { $Arch = "386" }

$InstallDir = Join-Path $env:LOCALAPPDATA "dreamtunnel"
$Binary = "dt-windows-$Arch.exe"
$Url = "$BaseUrl/dl/$Binary"
$ExePath = Join-Path $InstallDir "dt.exe"

Write-Host ""
Write-Host "  [+] dreamTunnel installer"
Write-Host "  [+] platform:    windows/$Arch"
Write-Host "  [+] install dir: $InstallDir"
Write-Host "  [+] downloading: $Url"
Write-Host ""

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
try {
    Invoke-WebRequest -Uri $Url -OutFile $ExePath -UseBasicParsing
} catch {
    Write-Host "[-] download failed: $_" -ForegroundColor Red
    exit 1
}

# Best-effort integrity check against the published SHA256SUMS file.
try {
    $sums = (Invoke-WebRequest -Uri "$BaseUrl/dl/SHA256SUMS" -UseBasicParsing).Content
    $want = ($sums -split "`n" |
        Where-Object { ($_ -split '\s+')[-1] -eq $Binary } |
        ForEach-Object { ($_ -split '\s+')[0] } |
        Select-Object -First 1)
    if ($want) {
        $got = (Get-FileHash -Path $ExePath -Algorithm SHA256).Hash.ToLower()
        if ($got -ne $want.ToLower()) {
            Remove-Item $ExePath -Force
            Write-Host "[-] checksum mismatch for $Binary" -ForegroundColor Red
            exit 1
        }
        Write-Host "  [+] checksum verified"
    }
} catch {
    Write-Host "  [!] checksum check skipped" -ForegroundColor Yellow
}

$currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($currentPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$currentPath;$InstallDir", "User")
    $env:PATH = "$env:PATH;$InstallDir"
    Write-Host "  [+] added to user PATH: $InstallDir"
    Write-Host "  [+] open a NEW terminal to use dt"
}

Write-Host ""
Write-Host "  [+] installed: $ExePath"
Write-Host ""
Write-Host "  ready to use — this device gets its own token on the first run:"
Write-Host "      dt http 8080              expose a local HTTP server"
Write-Host "      dt http 8080 my-site      -> https://my-site.$TunnelDomain"
Write-Host "      dt tcp 25565              raw TCP tunnel"
Write-Host "      dt http 8080 --json       machine-readable events"
Write-Host "      dt config                 server, device id and session"
Write-Host "      dt enroll                 request a new token for this device"
Write-Host ""
Write-Host "  site: https://$Domain/"
Write-Host ""
