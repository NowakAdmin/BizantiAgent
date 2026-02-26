# BizantiAgent

Lokalny agent dla Bizanti (Windows-first), który utrzymuje połączenie z Bizanti i wykonuje polecenia dla urządzeń w sieci lokalnej:

- Dibal W-025S (odczyt wagi przez TCP lub RS232),
- GoDEX G500 (druk `raw_tcp`, port 9100),
- Intermec PM43c (druk `raw_tcp`, najlepiej w trybie ZSim/ZPL),
- HP OfficeJet i inne drukarki Windows (druk `windows_spooler`).

## Założenia architektury

- Agent inicjuje połączenie wychodzące do Bizanti (WebSocket + fallback HTTP, bez otwierania portów po stronie klienta).
- Bizanti wysyła komendy typu `weigh_and_print` lub `print_label` przez WebSocket.
- Agent wykonuje komendę lokalnie i odsyła status `completed/failed` z payloadem wyniku.
- Szablony etykiet i konfiguracja urządzeń pozostają po stronie Bizanti (minimalna logika lokalna).

### Fallback API (włączony domyślnie)

Jeżeli WebSocket jest niedostępny, agent automatycznie przechodzi na polling HTTP:

- `POST /api/bizanticore/agent/heartbeat`
- `GET /api/bizanticore/agent/commands/next?limit=5`
- `POST /api/bizanticore/agent/commands/{id}/result`

To pozwala uruchomić MVP end-to-end bez stawiania serwera WebSocket.

## Komendy CLI

```bash
# zapis konfiguracji
bizanti-agent configure \
  --server=https://bizanti.pl \
  --ws=wss://bizanti.pl/agent/ws \
  --token=xxx \
  --tenant-id=tenant_123 \
  --github-repo=NowakAdmin/BizantiAgent

# uruchomienie bez tray (serwisowe/test)
bizanti-agent headless

# domyślne uruchomienie: tray
bizanti-agent
```

## Lokalizacja konfiguracji i logów

- Konfiguracja: `%ProgramData%/BizantiAgent/config.json`
- Logi: `%ProgramData%/BizantiAgent/logs/agent.log`

Przykładowy `config.json`:

```json
{
  "server_url": "https://bizanti.pl",
  "websocket_url": "wss://bizanti.pl/agent/ws",
  "agent_token": "<TOKEN_Z_BIZANTI>",
  "tenant_id": "tenant_123",
  "heartbeat_seconds": 30,
  "update": {
    "github_repo": "NowakAdmin/BizantiAgent",
    "check_interval_hours": 6
  }
}
```

Uwaga: `agent_id` oraz `device_name` nie są już wymagane w konfiguracji lokalnej.

## Autostart (Windows)

Tray ma przełącznik `Autostart (Windows)`.
Technicznie wpisuje agenta do:

`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

## Protokół wiadomości (MVP)

### Incoming (Bizanti -> Agent)

```json
{
  "type": "command",
  "job_id": "145",
  "command": "weigh_and_print",
  "payload": {
    "scale": {
      "transport": "serial",
      "serial_port": "COM3",
      "baud_rate": 9600,
      "request_command": "RW\r\n"
    },
    "printer": {
      "model": "pm43c",
      "host": "192.168.1.120",
      "port": 9100
    },
    "template": "^XA^FO50,40^FD{{product_name}}^FS^FO50,90^FDWaga: {{weight_kg}}^FS^XZ",
    "context": {
      "product_name": "Mielonka 500g"
    }
  }
}
```

### Outgoing (Agent -> Bizanti)

```json
{
  "type": "command_result",
  "agent_id": "agent_123",
  "job_id": "145",
  "status": "completed",
  "timestamp": "2026-02-22T12:00:00Z",
  "data": {
    "weight": 1.245,
    "printer": "pm43c"
  }
}
```

## Auto-update

- Agent sprawdza latest release z GitHub API (menu `Sprawdź aktualizacje`).
- Przy update wykonywany jest automatyczny self-replace binarki i restart procesu.
- Jeżeli nie ma opublikowanego GitHub Release, agent korzysta z fallbacku opartego o tagi i artefakty repo.
- Podczas update skrypt zamyka inne instancje agenta przed podmianą EXE.

## Status w tray

- `Łączenie...` – agent próbuje zestawić sesję.
- `Połączono` – heartbeat/polling lub sesja WebSocket zostały poprawnie zestawione.
- `Pauza (...)` – tymczasowa pauza po wielu błędach.

## Diagnostyka (Windows)

Jeżeli chcesz szybko potwierdzić poprawność restartu po aktualizacji:

- Sprawdź wersję pliku w `%ProgramData%\BizantiAgent\BizantiAgent.exe`.
- Sprawdź log: `%ProgramData%\BizantiAgent\logs\agent.log`.
- Zweryfikuj, że działa tylko jedna instancja procesu `BizantiAgent.exe`.

Przykład (PowerShell):

```powershell
Get-Process BizantiAgent -ErrorAction SilentlyContinue
```

## Konfiguracja drukarki

### 1) RAW TCP (GoDEX / Intermec / drukarki etykiet)

```json
{
  "model": "godex-g500",
  "transport": "raw_tcp",
  "host": "192.168.1.120",
  "port": 9100
}
```

### 2) Windows Spooler (HP OfficeJet i inne drukarki systemowe)

```json
{
  "model": "hp-officejet-pro-6960",
  "transport": "windows_spooler",
  "printer_name": "HP OfficeJet Pro 6960"
}
```

## Placeholdery etykiet

- Obsługiwane są oba formaty: `{{key}}` oraz `{key}`.
- Przykłady: `{{product_name}}`, `{weight_kg}`, `{{product.meta.some_meta_key}}`.

## Build (Windows)

```powershell
./scripts/build-windows.ps1
```
