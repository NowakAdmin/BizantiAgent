# BizantiAgent

## Local context
- Windows-first Go tray app that bridges Bizanti with local devices and keeps a single running instance.
- Key files: `cmd/bizanti-agent/main.go`, `internal/agent/agent.go`, `internal/config/config.go`, `internal/tray/app.go`, `internal/update/*`, `internal/devices/*`.
- Build and release helpers live in `scripts/build-windows.ps1`, `scripts/build-and-sign.ps1`, and `cmd/bizanti-agent/manifest.xml`.

## Repo-specific rules
- Keep the first-run move-to-%PROGRAMDATA% flow, autostart, and tray startup behavior aligned with the existing implementation.
- Prefer WebSocket connection logic first and HTTP polling only as the fallback path.
- Keep config and logs under `%ProgramData%/BizantiAgent`; do not push that responsibility into Bizanti.
- For startup, retry, update, or device-flow changes, check [ARCHITECTURE.md](ARCHITECTURE.md) and [QUICK_REFERENCE.md](QUICK_REFERENCE.md) before editing code.
- Use the existing Go tests under `internal/*_test.go` and run focused `go test ./...` for touched packages when behavior changes.
