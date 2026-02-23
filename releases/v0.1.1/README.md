# BizantiAgent

Lokalny agent dla Bizanti (Windows-first), który utrzymuje połączenie WebSocket z Bizanti i wykonuje polecenia dla urządzeń w sieci lokalnej:

- Dibal W-025S (odczyt wagi przez TCP lub RS232),
- GoDEX G500 (druk raw TCP 9100),
- Intermec PM43c (druk raw TCP 9100, najlepiej w trybie ZSim/ZPL).

## Założenia architektury

- Agent inicjuje połączenie wychodzące `WSS` do Bizanti (bez otwierania portów po stronie klienta).
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
  --agent-id=agent_123 \
  --token=xxx \
  --tenant-id=tenant_123 \
  --name=PRODUKCJA-01 \
  --github-repo=NowakAdmin/BizantiAgent

# uruchomienie bez tray (serwisowe/test)
bizanti-agent headless

# domyślne uruchomienie: tray
bizanti-agent
```

## Lokalizacja konfiguracji i logów

- Konfiguracja: `%ProgramData%/BizantiAgent/config.json`
- Logi: `%ProgramData%/BizantiAgent/logs/agent.log`

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
- MVP otwiera stronę release do instalacji.
- Następny krok: automatyczny self-replace binarki + restart procesu.

## Build (Windows)

```powershell
./scripts/build-windows.ps1
```

