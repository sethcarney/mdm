# skl installer for Windows
# Usage: irm https://raw.githubusercontent.com/sethcarney/skl/main/install.ps1 | iex

$ErrorActionPreference = 'Stop'

$Repo = "sethcarney/skl"
$BinaryName = "skl-windows-x64.exe"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:USERPROFILE\.local\bin" }
$InstallPath = Join-Path $InstallDir "skl.exe"

$DownloadUrl = "https://github.com/$Repo/releases/latest/download/$BinaryName"

Write-Host "Downloading skl (windows-x64)..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$TempFile = Join-Path $env:TEMP "skl-install.exe"
(New-Object System.Net.WebClient).DownloadFile($DownloadUrl, $TempFile)

Move-Item -Force $TempFile $InstallPath

Write-Host ""
Write-Host "skl installed successfully to $InstallPath"

$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
$MachinePath = [Environment]::GetEnvironmentVariable("PATH", "Machine")
if ($UserPath -notlike "*$InstallDir*" -and $MachinePath -notlike "*$InstallDir*") {
    Write-Host ""
    Write-Host "Note: $InstallDir is not in your PATH."
    Write-Host "Run the following to add it for your user:"
    Write-Host ""
    Write-Host "  [Environment]::SetEnvironmentVariable('PATH', `$env:PATH + ';$InstallDir', 'User')"
    Write-Host ""
    Write-Host "Then restart your terminal."
    Write-Host ""
}

Write-Host "Verify with: skl --version"
