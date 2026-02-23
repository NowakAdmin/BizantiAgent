# Sign BizantiAgent.exe with the self-signed code signing certificate
# Usage: .\sign-executable.ps1 [-ExePath "path\to\BizantiAgent.exe"]

param(
    [string]$ExePath = "BizantiAgent.exe"
)

# Certificate paths
$CertDir = "$PSScriptRoot\..\certs"
$PfxPath = "$CertDir\bizanti-code-signing.pfx"
$TimestampServer = "http://timestamp.digicert.com"

# Resolve absolute path
if (!(Test-Path $ExePath -IsValid)) {
    $ExePath = (Resolve-Path $ExePath -ErrorAction SilentlyContinue).Path
}

# Validation
if (!(Test-Path $PfxPath)) {
    Write-Host "✗ Certificate not found: $PfxPath" -ForegroundColor Red
    Write-Host "  Run 'create-signing-cert.ps1' first" -ForegroundColor Yellow
    exit 1
}

if (!(Test-Path $ExePath)) {
    Write-Host "✗ Executable not found: $ExePath" -ForegroundColor Red
    exit 1
}

Write-Host "Signing executable..." -ForegroundColor Cyan
Write-Host "  File: $(Split-Path $ExePath -Leaf)" -ForegroundColor Gray
Write-Host "  Path: $ExePath" -ForegroundColor Gray
Write-Host ""

# Prompt for certificate password
Write-Host "Enter the certificate password:" -ForegroundColor Yellow
$password = Read-Host -AsSecureString

try {
    # Convert to plain text for signtool (not ideal, but necessary)
    $ptr = [System.Runtime.InteropServices.Marshal]::SecureStringToCoTaskMemUnicode($password)
    $passwordPlain = [System.Runtime.InteropServices.Marshal]::PtrToStringUni($ptr)

    # Get signtool.exe path (part of Windows SDK)
    $signtoolPaths = @(
        "C:\Program Files (x86)\Windows Kits\10\bin\x64\signtool.exe",
        "C:\Program Files\Windows Kits\10\bin\x64\signtool.exe",
        "C:\Program Files (x86)\Windows Kits\11\bin\x64\signtool.exe",
        "C:\Program Files\Windows Kits\11\bin\x64\signtool.exe"
    )

    $signtoolPath = $null
    foreach ($path in $signtoolPaths) {
        if (Test-Path $path) {
            $signtoolPath = $path
            break
        }
    }

    if (!$signtoolPath) {
        Write-Host "✗ signtool.exe not found!" -ForegroundColor Red
        Write-Host "  Windows SDK must be installed" -ForegroundColor Yellow
        Write-Host "  Download from: https://developer.microsoft.com/en-us/windows/downloads/windows-sdk/" -ForegroundColor Yellow
        exit 1
    }

    # Sign the executable with timestamp server (prevents expiration issues)
    Write-Host "Executing signtool..." -ForegroundColor Gray
    & $signtoolPath sign `
        /f "$PfxPath" `
        /p "$passwordPlain" `
        /t "$TimestampServer" `
        /d "Bizanti Agent - Device Configuration Manager" `
        /du "https://nowakadministrators.pl" `
        "$ExePath"

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Executable signed successfully!" -ForegroundColor Green
        Write-Host ""

        # Verify signature
        Write-Host "Verifying signature..." -ForegroundColor Cyan
        & $signtoolPath verify /pa "$ExePath"

        if ($LASTEXITCODE -eq 0) {
            Write-Host "✓ Signature verified successfully!" -ForegroundColor Green
            Write-Host ""
            Write-Host "FILE READY FOR DISTRIBUTION" -ForegroundColor Green
            Write-Host "  Users will see 'Nowak Administrators sp. z o.o.' as publisher" -ForegroundColor Cyan
            Write-Host "  After ~1000 downloads, SmartScreen warning will disappear" -ForegroundColor Cyan
        } else {
            Write-Host "✗ Signature verification failed" -ForegroundColor Red
            exit 1
        }
    } else {
        Write-Host "✗ Signing failed with exit code: $LASTEXITCODE" -ForegroundColor Red
        exit 1
    }

} catch {
    Write-Host "✗ Error during signing:" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    exit 1
} finally {
    # Clear the plain text password from memory
    if ($passwordPlain) {
        $passwordPlain = $null
        [System.GC]::Collect()
    }
}
