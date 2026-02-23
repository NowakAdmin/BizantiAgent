# Build and Sign BizantiAgent using NowakAdmin SoftwareSigner
# This script compiles the Go binary and signs it using the centralized certificate
# Usage: .\build-and-sign.ps1 [-Version "0.1.3"] [-NoSign]

param(
    [string]$Version = "0.1.3",
    [switch]$NoSign = $false
)

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$OutputFile = "$ProjectRoot\BizantiAgent.exe"

# Path to SoftwareSigner package (sibling directory)
$SoftwareSignerPath = "$ProjectRoot\..\SoftwareSigner"

Write-Host "╔════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║  BIZANTI AGENT BUILD & SIGN                   ║" -ForegroundColor Cyan
Write-Host "║  (Using NowakAdmin SoftwareSigner)            ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""
Write-Host "Version: $Version" -ForegroundColor Yellow
Write-Host "Output: $OutputFile" -ForegroundColor Gray
Write-Host "Signer: $SoftwareSignerPath" -ForegroundColor Gray
Write-Host ""

# Step 1: Clean previous build
Write-Host "Step 1/3: Cleaning..." -ForegroundColor Cyan
if (Test-Path $OutputFile) {
    Remove-Item $OutputFile -Force -ErrorAction SilentlyContinue
    Write-Host "  ✓ Cleaned previous build" -ForegroundColor Green
} else {
    Write-Host "  ✓ No previous build found" -ForegroundColor Green
}
Write-Host ""

# Step 2: Build
Write-Host "Step 2/3: Building..." -ForegroundColor Cyan
Push-Location $ProjectRoot

$ldflags = @(
    "-H=windowsgui",           # No console window
    "-s -w",                   # Strip symbols and debugging info
    "-X main.version=$Version" # Inject version string
) -join " "

try {
    & go build -ldflags $ldflags -o $OutputFile .\cmd\bizanti-agent
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "  ✗ Build failed" -ForegroundColor Red
        Pop-Location
        exit 1
    }

    $fileSize = (Get-Item $OutputFile).Length / 1MB
    Write-Host "  ✓ Build successful" -ForegroundColor Green
    Write-Host "    File size: $([math]::Round($fileSize, 2)) MB" -ForegroundColor Gray
    Write-Host ""
} catch {
    Write-Host "  ✗ Build error: $_" -ForegroundColor Red
    Pop-Location
    exit 1
}

# Step 3: Sign using SoftwareSigner
Write-Host "Step 3/3: Signing..." -ForegroundColor Cyan

if ($NoSign) {
    Write-Host "  ⊘ Skipped (--NoSign flag)" -ForegroundColor Yellow
} else {
    # Verify SoftwareSigner exists
    if (!(Test-Path $SoftwareSignerPath)) {
        Write-Host "  ✗ SoftwareSigner not found: $SoftwareSignerPath" -ForegroundColor Red
        Write-Host "  Clone NowakAdmin/SoftwareSigner to sibling directory:" -ForegroundColor Yellow
        Write-Host "  git clone https://github.com/NowakAdmin/SoftwareSigner.git ..\SoftwareSigner" -ForegroundColor Gray
        Pop-Location
        exit 1
    }

    $CertPath = "$SoftwareSignerPath\certs\nowakadmin-codesigning.pfx"
    
    if (!(Test-Path $CertPath)) {
        Write-Host "  ✗ Certificate not found: $CertPath" -ForegroundColor Red
        Write-Host "  Create certificate first:" -ForegroundColor Yellow
        Write-Host "  ..\SoftwareSigner\scripts\create-certificate.ps1" -ForegroundColor Gray
        Pop-Location
        exit 1
    }

    Write-Host "  Enter certificate password:" -ForegroundColor Yellow
    $CertPassword = Read-Host -AsSecureString

    # Call SoftwareSigner
    try {
        & "$SoftwareSignerPath\scripts\sign-build.ps1" `
            -ConfigFile "signing-config.json" `
            -CertificatePath $CertPath `
            -CertPassword $CertPassword `
            -ExecutablePath $OutputFile

        if ($LASTEXITCODE -ne 0) {
            Write-Host "  ✗ Signing failed" -ForegroundColor Red
            Pop-Location
            exit 1
        }
    } catch {
        Write-Host "  ✗ Error calling SoftwareSigner: $_" -ForegroundColor Red
        Pop-Location
        exit 1
    }
}

Pop-Location
Write-Host ""
Write-Host "╔════════════════════════════════════════════════╗" -ForegroundColor Green
Write-Host "║  BUILD COMPLETE                               ║" -ForegroundColor Green
Write-Host "╚════════════════════════════════════════════════╝" -ForegroundColor Green
Write-Host ""
Write-Host "Ready for release: $OutputFile" -ForegroundColor Cyan
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "  1. Test on Windows system" -ForegroundColor Gray
Write-Host "  2. Create GitHub release and attach .exe file" -ForegroundColor Gray
Write-Host "  3. Publish release notes" -ForegroundColor Gray
Write-Host ""
