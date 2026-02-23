# Code Signing - Quick Reference

⚠️ **UPDATED**: Code signing now uses the centralized **NowakAdmin/SoftwareSigner** package.

See [SIGNING_WITH_SOFTWARESIGNER.md](SIGNING_WITH_SOFTWARESIGNER.md) for complete setup guide.

## Quick Start (2 steps)

### Step 1: One-Time Setup

In `SoftwareSigner` directory:
```powershell
.\scripts\create-certificate.ps1
# Enter password → Certificate created
```

**This creates a shared certificate for ALL NowakAdmin projects.**

### Step 2: Sign Your Build

In `BizantiAgent/scripts`:
```powershell
.\build-and-sign.ps1 -Version "0.1.3"
# Enter certificate password → Binary signed
```

Done! 🎉

---

## File Locations

```
NowakAdmin/SoftwareSigner/         ← Certificate lives here
  └── certs/
      └── nowakadmin-codesigning.pfx

NowakAdmin/BizantiAgent/           ← Just references it
  └── signing-config.json
```

---

## Security

- ✅ Certificate stored **once** in SoftwareSigner
- ✅ `.gitignore` protects .pfx file
- ✅ Password stored in GitHub Secrets
- ✅ Works for **all future NowakAdmin projects**

---

## Detailed Guide

[→ See SIGNING_WITH_SOFTWARESIGNER.md](SIGNING_WITH_SOFTWARESIGNER.md)
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
