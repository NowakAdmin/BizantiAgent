Prepared release folder for BizantiAgent v0.1.21.

This folder is a placeholder created by the release process. Build artifacts should be placed here before creating a GitHub release or copying to the update server.

Expected artifacts:
- bizanti-agent-v0.1.21-win64.zip (contains BizantiAgent.exe and update scripts, signed)
- bizanti-agent-v0.1.21-linux-amd64.tar.gz (contains `bizanti-agent` binary)

Build notes:
- This project produces Windows-only agent binaries. Do not build Linux artifacts for releases.
- Building on this Linux dev machine previously failed due to native GUI/tray dependencies (`systray`, `syscall.SysProcAttr.HideWindow`).
- Recommended: build the Windows EXE on a Windows CI runner using `scripts/build-and-sign.ps1` or cross-compile on an environment configured for Windows builds.

No-sign option (applies here):
- For this release we will skip code signing when building locally. Use `-NoSign` on `build-and-sign.ps1` or run the `go build` step manually and package the EXE. Signing should be done in CI or on a machine that has access to the signing certificate.

Next steps:
1. Build the Windows EXE and package as `bizanti-agent-v0.1.21-win64.zip`.
2. Place the zip into this folder and update `manifest.json` if needed.
3. Create Git tag `v0.1.21` and a GitHub Release, attach the zip. The updater will detect release via GitHub API.
