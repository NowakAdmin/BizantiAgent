# BizantiAgent v0.1.3 - Quick Reference

## What's New?

### 1️⃣ First-Run Auto-Setup
```
User downloads BizantiAgent.exe → Runs it → Dialog appears
"We recommend moving to %PROGRAMDATA%\BizantiAgent\"
         ↓
      USER CLICKS YES
         ↓
API moved + registry updated + auto-restart
         ↓
App now runs from C:\ProgramData\BizantiAgent\BizantiAgent.exe
```

### 2️⃣ Auto-Connect (No User Click!)
Before: User had to click "Połącz" button
Now: Agent connects automatically on startup ✓

### 3️⃣ Smart Retry with Pause
- Try to connect
- Fail? Retry immediately
- Fail 3 times? Pause 5 minutes then retry
- Success? Reset counter, keep running

### 4️⃣ Live Status Display
Hover over tray icon → see real-time status:
- `Łączenie...` - Initial connection attempt
- `Łączenie... (próba 2)` - On retry #2
- `Pauza (próba za 4:45)` - Paused, will retry in 4 min 45 sec

---

## User Deployment Instructions

### For First-Time Users:

1. **Download** `BizantiAgent.exe` from releases
2. **Run** the .exe anywhere (Desktop, Downloads, etc.)
3. **Accept dialog** asking to move to %PROGRAMDATA%
4. **Done!** App is now installed and running
   - Tray icon appears automatically
   - Connects to Bizanti automatically
   - Starts on Windows login automatically

### Configuration:

**Option A - Edit GUI:**
1. Right-click tray → "Ustawienia"
2. Edit in Notepad:
   - `server_url`: https://bizanti.yourdomain.com
   - `agent_id`: Your agent ID from Bizanti
   - `agent_token`: Your API token
   - `tenant_id`: Your tenant ID (optional)
   - `device_name`: Name visible in Bizanti

**Option B - Command Line:**
```
BizantiAgent.exe configure --server=https://bizanti.yourdom.com --token=abc123xyz
```

**Option C - Manual JSON Edit:**
`C:\ProgramData\BizantiAgent\config.json`

---

## Tray Menu Guide

| Menu Item | What it Does |
|-----------|-------------|
| **Połącz** | Start connection (auto-disabled if already running) |
| **Rozłącz** | Stop connection |
| **Autostart** | Enable/disable run on Windows login |
| **Sprawdź aktualizacje** | Check for new version manually |
| **Przeładuj ustawienia** | Reload config.json without restart |
| **Ustawienia** | Open config.json in Notepad |
| **Pokaż log** | View agent.log in Notepad |
| **Folder logów** | Open logs folder in Explorer |
| **Zamknij** | Exit agent |

---

## Status Meanings

| Status | Meaning | What's Happening |
|--------|---------|------------------|
| `Offline` | Agent stopped | No connection attempt |
| `Łączenie...` | Connecting | Initial connection or first retry |
| `Łączenie... (próba 2)` | 2nd attempt | Already failed once, trying again |
| `Pauza (próba za 5:00)` | Paused | 3 failures - waiting 5 min before retry |

---

## Troubleshooting

### Q: App doesn't auto-connect on startup?
**A:** 
- Check config.json has `server_url` and `agent_token` set
- Check logs: `C:\ProgramData\BizantiAgent\logs\agent.log`
- Try manual "Połącz" from tray menu

### Q: First-run dialog didn't appear?
**A:**
- If .exe is already in `C:\ProgramData\BizantiAgent\`, dialog won't show
- This is correct behavior (already installed)
- Just run normally in that case

### Q: Status shows "Pauza" for 5 minutes?
**A:**
- This is normal - agent had 3 connection failures
- It pauses to avoid hammering the server
- Will automatically retry after 5 minutes
- Check server connectivity or Bizanti status

### Q: Where are the logs?
**A:**
- Location: `C:\ProgramData\BizantiAgent\logs\agent.log`
- Quick access: Tray menu → "Folder logów"
- New log created on each app restart (previous deleted)

### Q: Can I move/rename the .exe after installation?
**A:**
- Not recommended - autostart registry points to installed location
- If you need to move: delete autostart, move app, re-enable autostart
- Better: Reinstall to desired location, then enable autostart

### Q: Can I run multiple instances?
**A:**
- No - Windows mutex prevents it
- If you try to run again, popup says "Agent jest już uruchomiony"
- This is by design (prevents duplicate connections)

---

## File Locations

| File | Location |
|------|----------|
| **Executable** | `C:\ProgramData\BizantiAgent\BizantiAgent.exe` |
| **Config** | `C:\ProgramData\BizantiAgent\config.json` |
| **Logs** | `C:\ProgramData\BizantiAgent\logs\agent.log` |
| **Registry (Autostart)** | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\BizantiAgent` |

---

## For IT Admins / Deployment

### Silent Installation:
```batch
# Download and extract BizantiAgent.exe to Program Files
copy BizantiAgent.exe "C:\Program Files\BizantiAgent\BizantiAgent.exe"

# Create config.json before running
echo {
  "server_url": "https://bizanti.yourdomain.com",
  "agent_token": "your-token-here",
  "agent_id": "your-agent-id",
  "tenant_id": "your-tenant-id",
  "device_name": "%COMPUTERNAME%"
} > "C:\ProgramData\BizantiAgent\config.json"

# Run agent (will auto-move to ProgramData on first run if needed)
"C:\Program Files\BizantiAgent\BizantiAgent.exe"
```

### Group Policy Deployment (Future):
- Package as MSI installer (TODO)
- Deploy via SCCM or Intune
- Set config via GPO startup scripts

### Rollout Checklist:
- [ ] Test on Windows 10 / Windows 11 / Windows Server 2019/2022
- [ ] Test on restricted user accounts (non-admin)
- [ ] Verify antivirus doesn't quarantine (may need whitelisting)
- [ ] Test network connectivity (can agent reach Bizanti server?)
- [ ] Verify logs are being written
- [ ] Test pause behavior (unplug network, see if pause triggers)

---

## Performance Impact

- **Memory**: ~15-25 MB baseline
- **CPU**: <1% idle, spikes during polling/heartbeat
- **Network**: 1 heartbeat every 30 seconds (configurable)
- **Disk**: ~1 KB log per heartbeat cycle, log rotated on agent restart

---

## Security Notes

✅ **Secure by design:**
- No passwords/secrets logged (token masked)
- All connections should be HTTPS
- API tokens stored in local config.json (plain text - consider encrypting on production)
- Runs as standard user (no elevation needed)
- No registry modifications beyond autostart

⚠️ **Recommendations:**
- Use HTTPS for all connections
- Keep `agent_token` confidential (treat like password)
- Monitor logs for unusual activity
- Code-sign executable (upcoming)

---

## Version Info

- **Current Version**: 0.1.3
- **Build Date**: 2026-02-23
- **Platform**: Windows only (x86-64)
- **Requirements**: Windows 7 SP1 or newer (most Windows 8+ systems)

---

## Support

If issues arise:

1. **Check logs first**: `C:\ProgramData\BizantiAgent\logs\agent.log`
2. **Verify config**: `C:\ProgramData\BizantiAgent\config.json`
3. **Test connectivity**: Can you ping the Bizanti server?
4. **Restart agent**: Tray menu → "Zamknij" then run again
5. **Check Bizanti status**: Verify Bizanti service is online

---

