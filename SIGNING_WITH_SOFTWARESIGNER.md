# SoftwareSigner + BizantiAgent Integration Guide

This guide shows how to set up code signing using the centralized **NowakAdmin/SoftwareSigner** package.

## Architecture

```
NowakAdmin/
├── SoftwareSigner/           (PRIVATE - One certificate for all projects)
│   ├── certs/
│   │   └── nowakadmin-codesigning.pfx   (⚠️ Never commit!)
│   ├── PKI/
│   │   └── SigningModule.psm1
│   └── scripts/
│       ├── create-certificate.ps1       (Run once, globally)
│       ├── sign-build.ps1               (Used by all projects)
│       └── verify-signature.ps1
│
├── BizantiAgent/             (PUBLIC - Just references SoftwareSigner)
│   ├── signing-config.json               (Project-specific settings)
│   └── scripts/
│       └── build-and-sign.ps1            (Calls SoftwareSigner)
│
├── FutureProject/            (Future - Also uses SoftwareSigner)
│   ├── signing-config.json
│   └── scripts/build-and-sign.ps1
```

## Setup (One Time - Across All Projects)

### Step 1: Prerequisites

```powershell
# Install Windows SDK (if not already installed)
# https://developer.microsoft.com/windows/downloads/windows-sdk/
# Select: Windows SDK Desktop Tools
```

### Step 2: Create Global Certificate

**Run in SoftwareSigner directory:**

```powershell
cd SoftwareSigner
.\scripts\create-certificate.ps1
```

**When prompted:**
- Enter a strong password
- Store the password securely (e.g., GitHub Secrets, password manager)

**Output:**
```
✓ Certificate created: certs/nowakadmin-codesigning.pfx
✓ Valid for 5 years (2026-2031)
✓ All projects can now use this certificate
```

---

## Usage (Per Project Release)

### For BizantiAgent

**Verify the setup:**
```powershell
cd BizantiAgent
cat signing-config.json  # View configuration
```

**Build and sign:**
```powershell
cd BizantiAgent\scripts
.\build-and-sign.ps1 -Version "0.1.3"
# When prompted: Enter the certificate password
```

**Output:**
```
╔════════════════════════════════════════════════╗
║  BIZANTI AGENT BUILD & SIGN                   ║
║  (Using NowakAdmin SoftwareSigner)            ║
╚════════════════════════════════════════════════╝

Step 1/3: Cleaning... ✓
Step 2/3: Building... ✓ (6.2 MB)
Step 3/3: Signing...
  ✓ Signed successfully
  ✓ Signature verified

Publisher: Nowak Administrators sp. z o.o.
Ready for release: BizantiAgent.exe
```

---

## For Future Projects

To add your new project to the signing ecosystem:

### 1. Create `signing-config.json`

```json
{
  "signingEnabled": true,
  "projectName": "Your New Project",
  "description": "Brief description",
  "companyUrl": "https://nowakadministrators.pl",
  "timestampServer": "http://timestamp.digicert.com",
  "executablePath": "YourApp.exe",
  "version": "1.0.0"
}
```

### 2. Update Your Build Script

In `scripts/build-and-sign.ps1`:

```powershell
# Point to SoftwareSigner (sibling directory)
$SoftwareSignerPath = "$ProjectRoot\..\SoftwareSigner"

# Call it
& "$SoftwareSignerPath\scripts\sign-build.ps1" `
    -ConfigFile "signing-config.json" `
    -CertificatePath "$SoftwareSignerPath\certs\nowakadmin-codesigning.pfx" `
    -CertPassword $CertPassword
```

### 3. Build and Sign

```powershell
.\scripts\build-and-sign.ps1 -Version "1.0.0"
```

All projects use **the same certificate** ✅

---

## File Structure Reference

### SoftwareSigner (Private, Shared)

```
SoftwareSigner/
├── .gitignore                          # Protects *.pfx
├── README.md
├── composer.json
├── certs/
│   └── nowakadmin-codesigning.pfx      # 🔒 PRIVATE KEY - Never commit!
├── PKI/
│   └── SigningModule.psm1              # Core signing logic
├── scripts/
│   ├── create-certificate.ps1          # Create global cert (run once)
│   ├── sign-build.ps1                  # Sign any project
│   └── verify-signature.ps1            # Verify signature
└── examples/
    ├── BizantiAgent.json               # Template for BizantiAgent
    └── template.json                   # Template for new projects
```

### BizantiAgent (Public, Project-Specific)

```
BizantiAgent/
├── signing-config.json                 # Minimal config (no secrets!)
├── .gitignore                          # No certs/ directory needed
├── scripts/
│   └── build-and-sign.ps1             # Calls ../../SoftwareSigner/scripts/sign-build.ps1
└── ... (rest of BizantiAgent code)
```

**Key point:** `signing-config.json` contains NO passwords or certificate paths - just project metadata.

---

## Certificate Security

### ✅ Protected

- **Certificate file**: Stored in SoftwareSigner/certs/ only
- **Password**: Via environment variable or GitHub Secrets
- **Never committed**: `.gitignore` protects the .pfx file

### ✅ .gitignore Pattern (SoftwareSigner)

```
certs/
*.pfx
*.p12
*.key
*.password
```

### ⚠️ What Happens If .pfx Is Committed?

Anyone with the file + password can sign code as "Nowak Administrators sp. z o.o."
- **Solution**: .gitignore prevents this
- **If it happens**: Rotate certificate immediately

---

## Verification Commands

### Check Signature

```powershell
# View signature details
Get-AuthenticodeSignature BizantiAgent.exe

# Expected output:
# SignerCertificate: CN=Nowak Administrators Code Signing
# Status: Valid
```

### Detailed Certificate Info

```powershell
# Using SoftwareSigner script
..\..\SoftwareSigner\scripts\verify-signature.ps1 `
  -ExecutablePath "BizantiAgent.exe" `
  -ShowCertDetails
```

---

## CI/CD Integration (GitHub Actions)

### Setup GitHub Secrets

1. **Encode certificate** (one time):
   
   ```powershell
   $pfx = [Convert]::ToBase64String((Get-Content SoftwareSigner\certs\nowakadmin-codesigning.pfx -Encoding Byte))
   Set-Clipboard -Value $pfx
   ```

2. **Add to GitHub Secrets:**
   - `CODESIGNING_CERT` = Base64-encoded .pfx
   - `CERT_PASSWORD` = Your certificate password

### GitHub Actions Workflow

```yaml
name: Build and Release

on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v3
      
      # Clone SoftwareSigner
      - uses: actions/checkout@v3
        with:
          repository: NowakAdmin/SoftwareSigner
          path: SoftwareSigner
          token: ${{ secrets.PRIVATE_REPO_TOKEN }}
      
      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.23'
      
      - name: Build
        run: |
          go build -ldflags "-H=windowsgui -s -w" `
            -o BizantiAgent.exe .\cmd\bizanti-agent
      
      - name: Restore Certificate
        run: |
          $cert = [Convert]::FromBase64String("${{ secrets.CODESIGNING_CERT }}")
          Set-Content -Path SoftwareSigner\certs\nowakadmin-codesigning.pfx `
            -Value $cert -Encoding Byte
      
      - name: Sign Build
        run: |
          .\scripts\build-and-sign.ps1 -Version ${{ github.ref_name }}
        env:
          CERT_PASSWORD: ${{ secrets.CERT_PASSWORD }}
      
      - name: Upload Release
        uses: softprops/action-gh-release@v1
        with:
          files: BizantiAgent.exe
```

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `SoftwareSigner not found` | Clone to sibling: `git clone https://github.com/NowakAdmin/SoftwareSigner.git ../SoftwareSigner` |
| `Certificate not found` | Run `SoftwareSigner/scripts/create-certificate.ps1` |
| `signtool.exe not found` | Install Windows SDK (Desktop Tools) |
| `Wrong password` | Re-enter correct certificate password |
| `.pfx accidentally committed` | Move out immediately, rotate certificate |

---

## Certificate Lifecycle

### Current (2026-2031)
- **Certificate**: `nowakadmin-codesigning.pfx`
- **Used by**: BizantiAgent, future projects
- **Expires**: February 23, 2031

### Future Renewal (2031)
```powershell
# Create new certificate
.\SoftwareSigner\scripts\create-certificate.ps1

# Old signatures remain valid (timestamp server)
# Start signing new projects with new cert
```

### Optional: Upgrade to Commercial Certificate

When ready (recommended after v1.0):
1. Purchase EV certificate from DigiCert (~$500/year)
2. Save to `SoftwareSigner/certs/nowakadmin-evcodesigning.pfx`
3. Update projects to reference new cert
4. All SmartScreen warnings disappear immediately

---

## Summary

| Aspect | Before | After (SoftwareSigner) |
|--------|--------|------------------------|
| **Certificate location** | In each project | One global location |
| **# of certificates** | Multiple per project | One for all |
| **Setup per project** | Create cert script | Just reference config |
| **Reuse across projects** | Manual copy-paste | Automatic |
| **Password management** | Per project | Single password |
| **Upgrade to commercial** | Update all projects | Update one cert |

**Bottom line:** One certificate, all your software, maximum scalability.

---

## Questions?

- **Certificate setup**: See `SoftwareSigner/README.md`
- **Project integration**: Follow examples in `SoftwareSigner/examples/`
- **Troubleshooting**: Check `CODE_SIGNING_GUIDE.md` (BizantiAgent) or email support

**Next step**: Run `create-certificate.ps1` and sign your first release! 🎉
