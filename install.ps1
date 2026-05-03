# mdm installer for Windows
# Usage: irm https://raw.githubusercontent.com/sethcarney/mdm/main/install.ps1 | iex

$ErrorActionPreference = 'Stop'

$Repo = "sethcarney/mdm"
$BinaryName = "mdm-windows-x64.exe"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:USERPROFILE\.local\bin" }
$InstallPath = Join-Path $InstallDir "mdm.exe"

$DownloadUrl = "https://github.com/$Repo/releases/latest/download/$BinaryName"

Write-Host "Downloading mdm (windows-x64)..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$TempFile = Join-Path $env:TEMP "mdm-install.exe"
(New-Object System.Net.WebClient).DownloadFile($DownloadUrl, $TempFile)

$CosignCmd = Get-Command cosign -ErrorAction SilentlyContinue
if ($CosignCmd) {
    Write-Host "Verifying cosign signature..."
    $BundleUrl = "https://github.com/$Repo/releases/latest/download/$BinaryName.bundle"
    $BundleFile = Join-Path $env:TEMP "mdm-install.exe.bundle"
    (New-Object System.Net.WebClient).DownloadFile($BundleUrl, $BundleFile)
    & cosign verify-blob $TempFile `
        --bundle $BundleFile `
        --certificate-identity-regexp='^https://github\.com/sethcarney/mdm/\.github/workflows/release\.yml@refs/heads/main$' `
        --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Signature verification FAILED. The binary may be tampered. Aborting."
        Remove-Item -Force $TempFile, $BundleFile -ErrorAction SilentlyContinue
        exit 1
    }
    Remove-Item -Force $BundleFile
    Write-Host "Signature verified!"
} else {
    Write-Host "cosign not found — skipping signature verification."
    Write-Host "Install cosign to verify: https://docs.sigstore.dev/cosign/system_config/installation/"
}

Move-Item -Force $TempFile $InstallPath

Write-Host ""
Write-Host "mdm installed successfully to $InstallPath"

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

Write-Host "Verify with: mdm --version"
