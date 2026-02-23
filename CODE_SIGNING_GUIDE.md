# Code Signing Guide for BizantiAgent

This guide explains how to create and use a self-signed code signing certificate for BizantiAgent.exe.

## Overview

Your executable is signed with:
- **Subject**: Nowak Administrators sp. z o.o.
- **Validity**: 5 years
- **Key Size**: 4096 bits (strong encryption)
- **Type**: Authenticode (Windows standard)

Signed executables show your company name as the publisher, building user trust.

## Prerequisites

### Windows SDK (Required once)

You need `signtool.exe` from Windows SDK:

1. **Download**: https://developer.microsoft.com/windows/downloads/windows-sdk/
2. **Install**: Run installer, select "Windows SDK Desktop Tools"
3. **Verify**:
   ```powershell
   Get-Command signtool.exe
   # Should return path like: C:\Program Files (x86)\Windows Kits\10\bin\x64\signtool.exe
   ```

### PowerShell Execution Policy

Allow local scripts:
```powershell
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
```

## Quick Start (5 minutes)

### Step 1: Create Certificate (One Time)

```powershell
cd BizantiAgent\scripts
.\create-signing-cert.ps1
```

**What it does:**
- Creates self-signed certificate with your company info
- Exports to `certs/bizanti-code-signing.pfx` (protected with password)
- Exports public cert to `certs/bizanti-code-signing.cer`

**Output:**
```
Creating self-signed code signing certificate...
✓ Certificate created successfully
✓ PFX exported to: D:\...\certs\bizanti-code-signing.pfx
✓ CER exported to: D:\...\certs\bizanti-code-signing.cer

Enter a password to protect the certificate: [you type password]
```

**⚠️ IMPORTANT**: `.gitignore` includes `certs/` - never commit the .pfx file

### Step 2: Build and Sign

```powershell
cd BizantiAgent\scripts
.\build-and-sign.ps1 -Version "0.1.3"
```

**What it does:**
- Compiles BizantiAgent.exe
- Signs with your certificate
- Verifies the signature
- Outputs signed executable

**Output:**
```
╔════════════════════════════════════════════════╗
║  BIZANTI AGENT BUILD & SIGN                   ║
╚════════════════════════════════════════════════╝

Step 1/3: Cleaning... ✓
Step 2/3: Building... ✓ (6.2 MB)
Step 3/3: Signing...
  Enter certificate password: [you type password]
  ✓ Signed successfully
  ✓ Signature verified

BUILD COMPLETE
Ready for release: BizantiAgent.exe
```

### Step 3: Publish to GitHub

```powershell
# Tag release
git tag -a v0.1.3 -m "Release v0.1.3 - Self-signed code signing"

# Create GitHub Release with asset
# 1. Go to: https://github.com/NowakAdmin/BizantiAgent/releases
# 2. Click "Create a new release"
# 3. Enter tag: v0.1.3
# 4. Upload: BizantiAgent.exe
# 5. Publish
```

## Troubleshooting

### Error: "signtool.exe not found"
```powershell
# Verify Windows SDK installation
Get-Command signtool.exe

# If not found, reinstall Windows SDK:
# https://developer.microsoft.com/windows/downloads/windows-sdk/
```

### Error: "Certificate not found"
```powershell
# Run certificate creation script first
.\create-signing-cert.ps1

# Place certificate in certs/ directory
# ✓ certs/bizanti-code-signing.pfx (required)
```

### PowerShell: "execution of scripts is disabled"
```powershell
# Allow scripts
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
```

### Users see "Unknown Publisher"

**Expected behavior** - this is normal for self-signed certificates.

**Timeline:**
- **Month 1-2**: SmartScreen warning shown during download
- **Month 3+**: After ~1000 downloads, warning disappears automatically
- Users can click "More info" → "Run anyway" (already works in testing)

**To skip warning after v1.0:**
- Upgrade to Extended Validation (EV) certificate from DigiCert (~$500/yr)
- OR wait for SmartScreen reputation to build (free, 2-3 months)

## Advanced: GitHub Actions Automation

To auto-sign on every release, add to `.github/workflows/release.yml`:

```yaml
name: Build and Release

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.23'
      
      - name: Install Windows SDK
        run: |
          choco install windows-sdk-10.1 -y --force
      
      - name: Build and Sign
        run: |
          & '.\scripts\build-and-sign.ps1' `
            -Version ${{ github.ref_name }} `
            -CertPassword "${{ secrets.CERT_PASSWORD }}"
        env:
          CERT_PFX_PATH: ${{ secrets.CERT_PFX_BASE64 }}
      
      - name: Create Release
        uses: softprops/action-gh-release@v1
        with:
          files: BizantiAgent.exe
```

Then add to GitHub Secrets:
1. `CERT_PASSWORD` - Your certificate password
2. `CERT_PFX_BASE64` - Base64-encoded .pfx file (see below)

**Encode certificate for GitHub:**
```powershell
$pfxData = [Convert]::ToBase64String((Get-Content certs\bizanti-code-signing.pfx -Encoding Byte))
Set-Clipboard -Value $pfxData
# Paste into GitHub Secrets as CERT_PFX_BASE64
```

## Certificate Details

### View Certificate Info

```powershell
# List all installed certificates
Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.EnhancedKeyUsageList -like "*Code Signing*" }

# Get specific cert details
$cert = Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.Thumbprint -eq "YOUR_THUMBPRINT" }
$cert | Format-List Subject, Issuer, Thumbprint, NotAfter, NotBefore
```

### Renewal (optional - 5 years from now, 2031)

In 2031, when certificate expires:
1. Run `create-signing-cert.ps1` again (create new cert)
2. Update password in secure location
3. Update GitHub Secrets
4. Continue signing with new certificate

OR upgrade to purchased certificate:
- Sectigo: $99-150/year
- DigiCert: $200-300/year
- EV certificate: $500+/year (smoother user experience)

## What Users See

### Before Signing
```
You see "Unknown Publisher" SmartScreen warning
- This is a self-signed certificate
- Installation still works (click "More info" → "Run anyway")
```

### After Signing (Immediately)
```
Properties → Details → Digital Signatures
- Publisher: Nowak Administrators sp. z o.o.
- Signature verified
```

### After 2-3 Months (SmartScreen Reputation)
```
No warning - executables recognized as legitimate
- Requires ~1000 downloads
- Automatic (no action needed)
```

## Security Notes

1. **Never commit certificate**: `.gitignore` includes `certs/`
2. **Password protection**: Certificate is locked with your password
3. **Long validity**: 5 years = stable signing through 2031
4. **Timestamp server**: Uses DigiCert timestamp server (prevents expiration issues)

## Support Commands

```powershell
# Verify a signed executable
signtool verify /pa BizantiAgent.exe

# Show signature details
signtool verify /pa /v BizantiAgent.exe

# Check signature with PowerShell
Get-AuthenticodeSignature BizantiAgent.exe | Format-List

# Timestamp verification
# Ensures signature remains valid even after cert expires
```

## Files Reference

| File | Purpose |
|------|---------|
| `scripts/create-signing-cert.ps1` | Create self-signed certificate (run once) |
| `scripts/sign-executable.ps1` | Sign individual .exe file |
| `scripts/build-and-sign.ps1` | Full build + sign automation |
| `certs/bizanti-code-signing.pfx` | **PRIVATE** - never commit! |
| `certs/bizanti-code-signing.cer` | Public cert (for distribution info only) |
| `.gitignore` | Protects cert files from accidental commit |

## Next Steps

1. ✅ Run `create-signing-cert.ps1` (one time)
2. ✅ Run `build-and-sign.ps1` for each release
3. ✅ Upload signed .exe to GitHub Releases
4. ✅ Wait 2-3 months for SmartScreen reputation (optional)
5. ✅ Consider upgrading to paid certificate if needed

---

**Questions?** Certificate operations are straightforward once setup is complete. The scripts handle all complexity for you.
