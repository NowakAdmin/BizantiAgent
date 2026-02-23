# BizantiAgent v0.1.3 - Architecture & Flow Diagrams

## Application Startup Flow

```
┌──────────────────────────────────┐
│  User runs BizantiAgent.exe      │
│  (from any location)             │
└───────────────┬──────────────────┘
                │
                ▼
┌──────────────────────────────────────────┐
│ main.go: runTray()                       │
│ ┌────────────────────────────────────┐   │
│ │ Check: Is first-run?               │   │
│ │ (Not in %programdata%?)            │   │
│ └──────────┬─────────────────────────┘   │
└───────────┼──────────────────────────────┘
            │
            ├─ YES ─────────► First Time ──┐
            │                               │
            └─ NO ──────────► Already OK ──┤
                                           │
                                           ▼
                            ┌──────────────────────────┐
                            │ Load config.json         │
                            │ Acquire single-instance  │
                            │ Build logger (file-only) │
                            └──────────┬───────────────┘
                                       │
                                       ▼
                            ┌──────────────────────────┐
                            │ Create tray.App          │
                            │ Run systray GUI          │
                            └──────────┬───────────────┘
                                       │
                                       ▼
        ┌──────────────────────────────────────────────────┐
        │ tray.onReady()                                   │
        │ ┌─────────────────────────────────────────────┐  │
        │ │ Create menu items                           │  │
        │ │ Start status ticker (500ms updates)         │  │
        │ │                                             │  │
        │ │ ▶ AUTO-START AGENT HERE ◀ (NEW!)           │  │
        │ │ agent.Start(ctx)                            │  │
        │ │   → agent.loop() starts in goroutine        │  │
        │ │                                             │  │
        │ └─────────────────────────────────────────────┘  │
        └──────────────────────────────────────────────────┘
                                       │
                                       ▼
                        ┌──────────────────────────┐
                        │ Agent trying to connect  │
                        │ to Bizanti server        │
                        └──────────────────────────┘
```

## First-Run Setup Flow (NEW)

```
User runs BizantiAgent.exe (from Desktop/Downloads/etc)
          │
          ▼
setup.IsFirstRun()?
  (Check if exe location != %PROGRAMDATA%\BizantiAgent\BizantiAgent.exe)
          │
    ┌─────┴────────┐
   YES            NO
    │              │
    ▼              └──► Continue normal startup
    │
User sees dialog:
"We recommend moving to %PROGRAMDATA%
 Do you want to do this now?"
    │
    ├─ YES ──────────────────┐
    │                        │
    └─ NO ───────────────────┤
                             │
                    ┌────────▼─────────┐
                    │ If user clicked  │
                    │ YES:             │
                    ├────────────────┤
                    │ 1. Create dir: │
                    │  %PROGRAMDATA% │
                    │  \BizantiAgent │
                    │                │
                    │ 2. Copy exe to │
                    │  new location  │
                    │                │
                    │ 3. Update      │
                    │  registry for  │
                    │  autostart     │
                    │                │
                    │ 4. Restart app │
                    │  from new loc  │
                    │                │
                    │ setup.       │
                    │ RestartApp() │
                    └──┬─────────────┘
                       │
                       ▼
           ┌─────────────────────────┐
           │ App closes              │
           │ config.json:            │
           │ C:\ProgramData\         │
           │ BizantiAgent\           │
           │ config.json             │
           │                         │
           │ logs:                   │
           │ C:\ProgramData\         │
           │ BizantiAgent\logs\      │
           │ agent.log               │
           └────────────┬────────────┘
                        │
                        ▼
           ┌──────────────────────────┐
           │ Windows auto-restarts    │
           │ app from new location    │
           │                          │
           │ Now app sees:            │
           │ "Already in right place" │
           │ → Normal startup         │
           └──────────────────────────┘
```

## Connection & Retry Flow

```
                ┌──────────────────────────────┐
                │ agent.loop() - Main Loop     │
                └───────────┬──────────────────┘
                            │
                            ▼
                ┌──────────────────────────────┐
            ┌─► isPaused()?                   │
            │   (5-min recovery timer active) │
            │   ├─ YES ──► Sleep 30s          │
            │   └─ NO ───► Continue           │
            │   └──────────────────────────────┘
            │            │
            │            ▼
       Skip ┌──────────────────────────────┐
       if   │ Try connect via WebSocket    │
       on   │ OR HTTP polling              │
       pause└───────────┬──────────────────┘
                        │
            ┌───────────┴────────────┐
            │                        │
      Success?                   Failure?
            │                        │
            ▼                        ▼
       ┌─────────────┐         ┌──────────────────┐
       │ recordSucc  │         │ recordFailure()  │
       │ ess()       │         │ Increment counter│
       │             │         │ counter++        │
       │ ├─ Reset    │         │                  │
       │ │ counter   │         │ ┌──────────────┐ │
       │ │ = 0       │         │ │counter >= 3? │ │
       │ │           │         │ │   YES:       │ │
       │ │ ├─ Keep   │         │ │ Set pause    │ │
       │ │   running │         │ │ = now +      │ │
       │ │           │         │ │ 5 minutes    │ │
       │ └─ Connec   │         │ │              │ │
       │   ted OK    │         │ │   NO:        │ │
       │             │         │ │ Just retry   │ │
       └─────────────┘         │ │ immediately  │ │
                               │ └──────────────┘ │
                               └──────────────────┘
                                      │
                                      ▼
                     [Loop continues, tries again]
```

## Status Display Update (NEW - Every 500ms)

```
                 ┌─────────────────────┐
                 │ statusTicker.C      │
                 │ Fires every 500ms   │
                 └──────────┬──────────┘
                            │
                            ▼
            ┌──────────────────────────────────┐
            │ agent.GetStatus()                │
            │ Returns human-readable string    │
            └──────────┬───────────────────────┘
                       │
        ┌──────────────┼──────────────────┐
        │              │                  │
   agent.IsRunning?    │              pausedUntil?
        │              │                  │
   NO   │         YES  │             YES  │ NO
        │              │              │   └─────────┐
        ▼              ▼              ▼             │
     "Offline"  failuresCount? YES  "Pauza"      NO
                  │                 (timeRemain   failures?
                  │ > 0              ing)         YES
               "łączenie.        │               │
                .. (próba N)"     │          "Łączenie"
                  │               │
                  └───────┬───────┘
                          │
                          ▼
        ┌──────────────────────────────┐
        │ Update tray items:           │
        │ • status.SetTitle()          │
        │ • systray.SetTooltip()       │
        │                              │
        │ Result: User hovers over     │
        │ tray icon → sees live status │
        └──────────────────────────────┘
```

## Memory/Concurrency Model

```
┌─────────────────────────────────────────────────────────┐
│ Main Thread (systray event loop)                        │
│  - Handles tray menu clicks                            │
│  - Responds to timers (statusTicker, updateTicker)     │
│  - Updates UI (menu items, tooltips)                   │
│  - Synchronized access to agent via methods            │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ Agent Goroutine (background connection loop)            │
│  - Runs continuously in background                      │
│  - Manages WebSocket/HTTP connections                  │
│  - Tracks connection state (failures, pause)           │
│  - Protected by sync.Mutex for state changes           │
│                                                         │
│  Fields:                                                │
│  ├─ mu (sync.Mutex) - protects below                   │
│  ├─ consecutiveFailures (int) - failure counter        │
│  └─ pausedUntil (time.Time) - pause expire time        │
│                                                         │
│  Methods:                                               │
│  ├─ recordFailure() - increment failures, set pause    │
│  ├─ recordSuccess() - reset counter                    │
│  ├─ isPaused() - check if in recovery pause            │
│  └─ GetStatus() - return status string (for display)   │
└─────────────────────────────────────────────────────────┘

Communication:
  Main Thread ──► agent.Start(ctx) ──► Agent Goroutine
                      (signal to start)
  
  Main Thread ◄── agent.GetStatus() ◄── Agent Goroutine
                   (read-only query)

  Main Thread ──► agent.Stop() ──► Agent Goroutine
                      (signal to stop)
```

## Configuration File Structure

```
C:\ProgramData\BizantiAgent\
├── BizantiAgent.exe           ← Main executable
├── config.json                ← Configuration (created on first run)
│   ├── server_url
│   ├── websocket_url
│   ├── agent_id
│   ├── agent_token
│   ├── tenant_id
│   ├── device_name
│   ├── heartbeat_seconds
│   └── update.*
└── logs/
    └── agent.log              ← Log file (rotated on each restart)
```

## Version Comparison & Update Check Flow

```
        ┌─────────────────────────────┐
        │ User clicks "Sprawdź         │
        │ aktualizacje" OR             │
        │ updateTicker fires (6h)      │
        └──────────┬──────────────────┘
                   │
                   ▼
        ┌─────────────────────────────────┐
        │ github.CheckGitHubRelease()     │
        │                                 │
        │ Tries:                          │
        │ 1. /releases/latest (published) │
        │ 2. /tags (fallback)             │
        └──────────┬──────────────────────┘
                   │
           ┌───────┴────────┐
           │                │
      Release         Tag only
      found?          found?
           │                │
      Has              No
      assets?         release
           │          assets
        YES │              │
           │          Use version
           ▼            number only
    ┌─────────────┐    (can't download)
    │  Compare    │
    │  versions   │
    │  using      │
    │  numeric    │
    │  parsing    │
    │  (fixed in  │
    │  v0.1.3)    │
    └──────┬──────┘
           │
    ┌──────┴──────────┐
    │                 │
NewVersion?          No Update
  YES   │                 │
       │                 ▼
       │            "Masz najnowszą
       │             wersję X.X.X"
       │            (show dialog)
       │
       ▼
   "Dostępna
    aktualizacja X.X
    Zainstalować?"
   (Yes/No dialog)
       │
  User clicks Yes?
       │
      YES ▼
    Download .exe
    from assets
       │
      ▼
    StartSelfUpdate()
    (batch script)
       │
      ▼
    App restarts
    from new
    binary
```

## Log Rotation Strategy

```
┌───────────────────────────────────┐
│ BizantiAgent starts               │
└──────────┬────────────────────────┘
           │
           ▼
┌───────────────────────────────────┐
│ buildLogger() in main.go:         │
│                                   │
│ 1. Delete old agent.log           │
│    os.Remove(logPath)             │
│                                   │
│ 2. Create new empty file          │
│    os.O_CREATE|os.O_TRUNC         │
│                                   │
│ 3. Write startup message          │
│    "Logger uruchomiony..."        │
│                                   │
│ Result: Fresh log per start       │
│ (Historical logs discarded)       │
└────────────┬─────────────────────┘
             │
             ▼
    ┌──────────────────────┐
    │ App runs, logs       │
    │ heartbeats,          │
    │ connections, etc     │
    │                      │
    │ Log grows over time  │
    │ but only current run │
    └──────────────────────┘
             │
             ▼
    ┌──────────────────────┐
    │ App exits or crashes │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │ User restarts app    │
    │ → Cycle repeats      │
    │ → Old log deleted    │
    │ → New log created    │
    └──────────────────────┘

Note: No historical logs retained.
For long-term monitoring, consider:
- Redirect logs to centralized
  system (Bizanti backend log)
- Or implement log archival
```

## Registry Auto-Correction Flow

```
┌──────────────────────────────────┐
│ App starts from any location     │
│ (OR user enables autostart)      │
└───────────┬──────────────────────┘
            │
            ▼
┌──────────────────────────────────┐
│ setup.VerifyAutostart() called    │
│                                  │
│ 1. Query Windows registry:       │
│    HKCU\Software\...\Run\        │
│    BizantiAgent                  │
│                                  │
│ 2. Read stored path              │
└────────────┬─────────────────────┘
             │
      ┌──────┴───────────────┐
      │                      │
    Path matches             Path doesn't
    Expected?                match
      │                        │
     YES                       NO
      │                        │
      ▼                        ▼
    Do nothing          setup.VerifyAutostart
    (already            updates registry
    correct)            to correct path
```

---

## Summary: What's New in v0.1.3

| Feature | Before | After |
|---------|--------|-------|
| **Installation** | Manual dir creation | Auto-move dialog + auto-setup |
| **Autostart** | Windows registry (manual) | Auto-created + auto-verified |
| **Connection** | User clicks "Połącz" | Auto-connects on startup |
| **Retry Logic** | Immediate backoff retry | Smart pause after 3 failures |
| **Status Display** | Static menu text | Live tooltip (updates 2x/sec) |
| **First Time** | Complex setup process | One dialog, auto-handles rest |

