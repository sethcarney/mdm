# mdm installer for Windows
# Usage: irm https://raw.githubusercontent.com/sethcarney/mdm/main/install.ps1 | iex

$ErrorActionPreference = 'Stop'

$Repo = "sethcarney/mdm"
$BinaryName = "mdm-windows-x64.exe"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:USERPROFILE\.local\bin" }
$InstallPath = Join-Path $InstallDir "mdm.exe"

# GoReleaser signs the checksum manifest, not each binary, so the chain of
# trust runs: cosign verifies sha256sums.txt against its sigstore bundle, then
# the binary is verified against sha256sums.txt. Verifying a per-binary
# signature is not possible - no release has ever published one.
$BaseUrl = "https://github.com/$Repo/releases/latest/download"
$ChecksumFile = "sha256sums.txt"
$SignatureFile = "$ChecksumFile.sigstore.json"

Write-Host "Downloading mdm (windows-x64)..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$WorkDir = Join-Path $env:TEMP ("mdm-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
try {
    $TempFile = Join-Path $WorkDir $BinaryName
    $ChecksumPath = Join-Path $WorkDir $ChecksumFile
    (New-Object System.Net.WebClient).DownloadFile("$BaseUrl/$BinaryName", $TempFile)

    Write-Host "Downloading $ChecksumFile..."
    (New-Object System.Net.WebClient).DownloadFile("$BaseUrl/$ChecksumFile", $ChecksumPath)

    $CosignCmd = Get-Command cosign -ErrorAction SilentlyContinue
    if ($CosignCmd) {
        Write-Host "Verifying the checksum manifest signature..."
        $SignaturePath = Join-Path $WorkDir $SignatureFile
        (New-Object System.Net.WebClient).DownloadFile("$BaseUrl/$SignatureFile", $SignaturePath)
        # The workflow is triggered by a tag push, so the signing identity
        # always ends in refs/tags/<tag>; it is never a branch ref.
        & cosign verify-blob $ChecksumPath `
            --bundle $SignaturePath `
            --certificate-identity-regexp='^https://github\.com/sethcarney/mdm/\.github/workflows/release\.yml@refs/tags/.+$' `
            --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
        if ($LASTEXITCODE -ne 0) {
            Write-Error "Signature verification FAILED for $ChecksumFile. Aborting."
            exit 1
        }
        Write-Host "Signature verified."
    } else {
        Write-Host "cosign not found - skipping the signature check on $ChecksumFile."
        Write-Host "Install cosign to verify it: https://docs.sigstore.dev/cosign/system_config/installation/"
    }

    # Keep only this asset's line. A missing or duplicated entry means the
    # manifest is not what we expect, and checking against it would prove
    # nothing.
    $Entries = @(Get-Content $ChecksumPath | Where-Object { $_ -match "\s\*?$([regex]::Escape($BinaryName))$" })
    if ($Entries.Count -ne 1) {
        Write-Error "Expected exactly one checksum entry for $BinaryName in $ChecksumFile. Aborting."
        exit 1
    }
    $Expected = ($Entries[0] -split '\s+')[0]

    Write-Host "Verifying checksum..."
    $Actual = (Get-FileHash -Algorithm SHA256 -Path $TempFile).Hash
    if ($Actual -ne $Expected.ToUpperInvariant() -and $Actual.ToLowerInvariant() -ne $Expected.ToLowerInvariant()) {
        Write-Error "Checksum verification FAILED for $BinaryName. The download may be corrupt or tampered. Aborting."
        exit 1
    }
    Write-Host "Checksum verified."

    Move-Item -Force $TempFile $InstallPath
} finally {
    Remove-Item -Recurse -Force $WorkDir -ErrorAction SilentlyContinue
}

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
