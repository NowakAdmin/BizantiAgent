## Build BizantiAgent - example code used in v 0.1.21 use as a reference only

cd /var/www/bizanti-dev-modules/BizantiAgent && \
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 /usr/local/go/bin/go build \
  -ldflags "-H=windowsgui -s -w" \
  -o BizantiAgent.exe \
  ./cmd/bizanti-agent 2>&1 && \
echo "BUILD OK" && ls -lh BizantiAgent.exe


git -C /var/www/bizanti-dev-modules/BizantiAgent add BizantiAgent.exe && \
git -C /var/www/bizanti-dev-modules/BizantiAgent commit -m "v0.1.21: rebuild exe" && \
git -C /var/www/bizanti-dev-modules/BizantiAgent tag -d v0.1.21 && \
git -C /var/www/bizanti-dev-modules/BizantiAgent tag v0.1.21 HEAD && \
git -C /var/www/bizanti-dev-modules/BizantiAgent push origin main && \
git -C /var/www/bizanti-dev-modules/BizantiAgent push origin :refs/tags/v0.1.21 && \
git -C /var/www/bizanti-dev-modules/BizantiAgent push origin v0.1.21 && \
echo "PUSH OK"

TOKEN=$(cat ~/.git-credentials | grep github.com | sed 's|https://[^:]*:\([^@]*\)@github.com|\1|')

RESPONSE=$(curl -s -X POST \
  -H "Authorization: token $TOKEN" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/NowakAdmin/BizantiAgent/releases" \
  -d '{
    "tag_name": "v0.1.21",
    "name": "v0.1.21",
    "body": "## Co nowego w v0.1.21\n\n### Poprawki\n- **Auto-derive WebSocket URL** — jeśli `websocket_url` nie jest ustawiony w konfiguracji, agent automatycznie wyprowadza go z `server_url` (np. `https://bizanti.pl` → `wss://bizanti.pl/agent/ws`). Eliminuje konieczność ręcznego ustawiania obu pól.\n- Poprawiono pobieranie komend HTTP fallback z backendu.\n\n### Poprzednie zmiany (v0.1.20)\n- Rotacja logów, rollback UI, wskaźnik postępu pobierania aktualizacji\n- Pojedyncza instancja via Windows named mutex (bez PowerShell)\n- Asynchroniczne wykonywanie komend urządzeń przez WebSocket\n- Nowa komenda `ping_device`",
    "draft": false,
    "prerelease": false
  }')

echo "$RESPONSE" | python3 -c "import sys,json; r=json.load(sys.stdin); print('Release ID:', r.get('id'), '| Error:', r.get('message','ok'))"

TOKEN=$(cat ~/.git-credentials | grep github.com | sed 's|https://[^:]*:\([^@]*\)@github.com|\1|')

UPLOAD=$(curl -s -X POST \
  -H "Authorization: token $TOKEN" \
  -H "Accept: application/vnd.github+json" \
  -H "Content-Type: application/octet-stream" \
  "https://uploads.github.com/repos/NowakAdmin/BizantiAgent/releases/337258918/assets?name=BizantiAgent.exe" \
  --data-binary @/var/www/bizanti-dev-modules/BizantiAgent/BizantiAgent.exe)

echo "$UPLOAD" | python3 -c "import sys,json; r=json.load(sys.stdin); print('Asset:', r.get('name'), '| Size:', r.get('size'), 'B |', r.get('browser_download_url','ERROR: '+str(r.get('message',r))))"