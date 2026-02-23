# Create Self-Signed Code Signing Certificate for Bizanti Agent
# This script generates a professional certificate with company information
# Run this ONCE to create the certificate, then use sign-executable.ps1 for releases

# Certificate details
$CertSubject = "CN=Bizanti Agent, O=Nowak Administrators sp. z o.o., C=PL, ST=Mazovia, L=Warszawa"
$CertFriendlyName = "Nowak Administrators Code Signing Certificate"
$CertDescription = "Self-signed code signing certificate for Bizanti Agent distribution"
$ValidityYears = 5
$KeySize = 4096

# Output paths
$CertDir = "$PSScriptRoot\..\certs"
$PfxPath = "$CertDir\bizanti-code-signing.pfx"
$CerPath = "$CertDir\bizanti-code-signing.cer"

# Create certs directory if it doesn't exist
if (!(Test-Path $CertDir)) {
    New-Item -ItemType Directory -Path $CertDir -Force | Out-Null
    Write-Host "✓ Created certs directory: $CertDir" -ForegroundColor Green
}

# Check if certificate already exists
if (Test-Path $PfxPath) {
    Write-Host "✗ Certificate already exists at: $PfxPath" -ForegroundColor Yellow
    Write-Host "  Use sign-executable.ps1 to sign binaries with this certificate" -ForegroundColor Cyan
    exit 0
}

Write-Host "Creating self-signed code signing certificate..." -ForegroundColor Cyan
Write-Host "Subject: $CertSubject" -ForegroundColor Gray
Write-Host ""

try {
    # Create the certificate
    $cert = New-SelfSignedCertificate `
        -Type CodeSigningCert `
        -Subject $CertSubject `
        -FriendlyName $CertFriendlyName `
        -KeyUsage DigitalSignature `
        -KeyLength $KeySize `
        -NotAfter (Get-Date).AddYears($ValidityYears) `
        -CertStoreLocation "Cert:\CurrentUser\My" `
        -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3")

    Write-Host "✓ Certificate created successfully" -ForegroundColor Green
    Write-Host "  Thumbprint: $($cert.Thumbprint)" -ForegroundColor Gray
    Write-Host "  Valid until: $($cert.NotAfter.ToString('yyyy-MM-dd'))" -ForegroundColor Gray
    Write-Host ""

    # Prompt for password
    Write-Host "Enter a password to protect the certificate (you'll need it to sign binaries):" -ForegroundColor Yellow
    $password = Read-Host -AsSecureString

    # Export to PFX (includes private key)
    Write-Host "Exporting certificate to PFX file..." -ForegroundColor Cyan
    Export-PfxCertificate `
        -Cert "Cert:\CurrentUser\My\$($cert.Thumbprint)" `
        -FilePath $PfxPath `
        -Password $password `
        -Force | Out-Null

    Write-Host "✓ PFX exported to: $PfxPath" -ForegroundColor Green
    Write-Host ""

    # Export public cert (optional, for distribution/verification)
    Write-Host "Exporting public certificate (CER file)..." -ForegroundColor Cyan
    Export-Certificate `
        -Cert "Cert:\CurrentUser\My\$($cert.Thumbprint)" `
        -FilePath $CerPath `
        -Force | Out-Null

    Write-Host "✓ CER exported to: $CerPath" -ForegroundColor Green
    Write-Host ""

    Write-Host "CERTIFICATE CREATED SUCCESSFULLY" -ForegroundColor Green
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor Cyan
    Write-Host "  1. Add certs/bizanti-code-signing.pfx to .gitignore (IMPORTANT - never commit!)" -ForegroundColor Yellow
    Write-Host "  2. Run 'sign-executable.ps1' to sign BizantiAgent.exe" -ForegroundColor Yellow
    Write-Host "  3. Keep the certificate password in a secure location" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Troubleshooting:" -ForegroundColor Gray
    Write-Host "  • Certificate expires: $($cert.NotAfter.ToString('yyyy-MM-dd'))" -ForegroundColor Gray
    Write-Host "  • SmartScreen warmup: After ~1000 downloads over 2-3 months, warning disappears" -ForegroundColor Gray
    Write-Host "  • Manual install: Users can double-click .cer file to trust cert (optional)" -ForegroundColor Gray

} catch {
    Write-Host "✗ Error creating certificate:" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    exit 1
}
