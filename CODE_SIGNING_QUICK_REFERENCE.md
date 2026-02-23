# Code Signing - Quick Reference

## Setup (First Time Only - 5 minutes)

### Prerequisites
```powershell
# 1. Install Windows SDK from:
# https://developer.microsoft.com/windows/downloads/windows-sdk/
# (Select: "Windows SDK Desktop Tools")

# 2. Allow PowerShell scripts
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
```

### Create Certificate
```powershell
cd BizantiAgent\scripts
.\create-signing-cert.ps1
# Follow prompts, enter a strong password, remember it!
```

**What you get:**
- ✅ `certs/bizanti-code-signing.pfx` (your private certificate)
- ✅ `certs/bizanti-code-signing.cer` (public certificate info)
- ✅ Automatically added to `.gitignore` (safe from accidental commit)

---

## Build & Sign (For Every Release)

### Option 1: Automated (Recommended)
```powershell
cd BizantiAgent\scripts
.\build-and-sign.ps1 -Version "0.1.3"
# Enter certificate password when prompted
```

**Output**: `BizantiAgent.exe` (signed and ready)

### Option 2: Step by Step

#### 2a. Build only
```powershell
cd BizantiAgent
go build -ldflags "-H=windowsgui -s -w" -o BizantiAgent.exe .\cmd\bizanti-agent
```

#### 2b. Sign
```powershell
cd BizantiAgent\scripts
.\sign-executable.ps1 -ExePath "../BizantiAgent.exe"
# Enter certificate password when prompted
```

---

## Verify Signature

```powershell
# View signature
Get-AuthenticodeSignature BizantiAgent.exe

# Command-line verification
signtool verify /pa BizantiAgent.exe
```

**Expected output:**
```
Publisher: Nowak Administrators sp. z o.o.
Status: Valid signature
```

---

## Publish Release

```powershell
# Tag release
git tag -a v0.1.3 -m "Release v0.1.3"

# Push to GitHub
git push origin main
git push origin v0.1.3

# Then on GitHub.com:
# 1. Go to Releases page
# 2. Click "Create release" for v0.1.3
# 3. Upload BizantiAgent.exe
# 4. Publish
```

---

## Certificate Info

**Company**: Nowak Administrators sp. z o.o.  
**Type**: Self-signed Authenticode  
**Validity**: 5 years (2026-2031)  
**Key Size**: 4096 bits  
**Timestamp Server**: DigiCert (prevents expiration)  

---

## What Users See

| Timeframe | User Experience |
|-----------|-----------------|
| Day 1 | SmartScreen warning shows "Unknown Publisher" |
| Day 1-7 | Users click "More info" → "Run anyway" (works) |
| Month 3+ | After ~1000 downloads, warning disappears (automatic) |
| Year 2+ | Considered fully trusted by Windows |

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `signtool.exe not found` | Install Windows SDK (Desktop Tools) |
| `Certificate not found` | Run `create-signing-cert.ps1` first |
| `Wrong password` | Re-run script, enter correct password |
| `Permission denied` | Run PowerShell as Administrator |
| Script won't execute | `Set-ExecutionPolicy RemoteSigned -Scope CurrentUser` |

---

## Security Checklist

- ✅ Never commit `.pfx` file (in `.gitignore`)
- ✅ Password-protect certificate
- ✅ Store password securely
- ✅ Use timestamp server (prevents future expiration issues)
- ✅ Verify signature after each build

---

## Files Created

```
BizantiAgent/
├── scripts/
│   ├── create-signing-cert.ps1  (one-time setup)
│   ├── sign-executable.ps1      (manual signing)
│   └── build-and-sign.ps1       (full automation)
├── certs/
│   ├── bizanti-code-signing.pfx  (private - never commit!)
│   └── bizanti-code-signing.cer  (public reference)
└── CODE_SIGNING_GUIDE.md  (detailed documentation)
```

---

## Next Steps

1. Install Windows SDK (if not already installed)
2. Run: `.\scripts\create-signing-cert.ps1`
3. For each release: `.\scripts\build-and-sign.ps1 -Version "x.x.x"`
4. Upload signed `.exe` to GitHub Releases
5. Done! 🎉

For detailed info, see `CODE_SIGNING_GUIDE.md`
