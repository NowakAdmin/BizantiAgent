# Automated build and sign script for BizantiAgent
# This script compiles the Go binary and signs it with the code signing certificate
# Usage: .\build-and-sign.ps1 [-Version "0.1.3"] [-NoSign]

param(
    [string]$Version = "0.1.3",
    [switch]$NoSign = $false
)

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$OutputDir = "$ProjectRoot"
$OutputFile = "$OutputDir\BizantiAgent.exe"

Write-Host "╔════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║  BIZANTI AGENT BUILD & SIGN                   ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""
Write-Host "Version: $Version" -ForegroundColor Yellow
Write-Host "Output: $OutputFile" -ForegroundColor Gray
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

# Step 3: Sign
Write-Host "Step 3/3: Signing..." -ForegroundColor Cyan

if ($NoSign) {
    Write-Host "  ⊘ Skipped (--NoSign flag)" -ForegroundColor Yellow
} else {
    $CertPath = "$ProjectRoot\certs\bizanti-code-signing.pfx"
    
    if (!(Test-Path $CertPath)) {
        Write-Host "  ✗ Certificate not found: $CertPath" -ForegroundColor Red
        Write-Host "  Run: .\scripts\create-signing-cert.ps1" -ForegroundColor Yellow
        Pop-Location
        exit 1
    }

    Write-Host "  Enter certificate password:" -ForegroundColor Yellow
    $password = Read-Host -AsSecureString

    # Convert to plain text
    $ptr = [System.Runtime.InteropServices.Marshal]::SecureStringToCoTaskMemUnicode($password)
    $passwordPlain = [System.Runtime.InteropServices.Marshal]::PtrToStringUni($ptr)

    # Find signtool
    $signtoolPaths = @(
        "C:\Program Files (x86)\Windows Kits\10\bin\x64\signtool.exe",
        "C:\Program Files\Windows Kits\10\bin\x64\signtool.exe",
        "C:\Program Files (x86)\Windows Kits\11\bin\x64\signtool.exe",
        "C:\Program Files\Windows Kits\11\bin\x64\signtool.exe"
    )

    $signtoolPath = $signtoolPaths | Where-Object { Test-Path $_ } | Select-Object -First 1

    if (!$signtoolPath) {
        Write-Host "  ✗ signtool.exe not found" -ForegroundColor Red
        Write-Host "  Download Windows SDK: https://developer.microsoft.com/windows/downloads/windows-sdk/" -ForegroundColor Yellow
        Pop-Location
        exit 1
    }

    try {
        & $signtoolPath sign `
            /f "$CertPath" `
            /p "$passwordPlain" `
            /t "http://timestamp.digicert.com" `
            /d "Bizanti Agent - Device Configuration Manager" `
            /du "https://nowakadministrators.pl" `
            "$OutputFile" | Out-Null

        if ($LASTEXITCODE -eq 0) {
            Write-Host "  ✓ Signed successfully" -ForegroundColor Green
            
            # Verify
            & $signtoolPath verify /pa "$OutputFile" | Out-Null
            
            if ($LASTEXITCODE -eq 0) {
                Write-Host "  ✓ Signature verified" -ForegroundColor Green
            } else {
                Write-Host "  ⚠ Signature verification failed" -ForegroundColor Yellow
            }
        } else {
            Write-Host "  ✗ Signing failed" -ForegroundColor Red
            Pop-Location
            exit 1
        }
    } finally {
        if ($passwordPlain) {
            $passwordPlain = $null
            [System.GC]::Collect()
        }
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
