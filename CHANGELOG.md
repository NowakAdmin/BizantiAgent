# Changelog

Wszystkie istotne zmiany w projekcie BizantiAgent.

## [v0.1.27] - 2026-06-24

### Added
- Nowa komenda `agent_version` — zwraca wersję działającego procesu agenta. Przydatne do zdalnej weryfikacji, czy automatyczna aktualizacja faktycznie się powiodła, bez czekania na logi czy dostępu do pulpitu.

## [v0.1.26] - 2026-06-23

### Added
- Nowa komenda `list_serial_ports` — zwraca listę portów COM widocznych lokalnie na komputerze agenta.
- Nowa komenda `port_scan` — sprawdza współbieżnie osiągalność i opóźnienie listy portów TCP na danym hoście (np. żeby znaleźć, na którym porcie drukarka udostępnia pass-through z portu RS232).
- Nowa komenda `serial_probe` — generyczne wysłanie surowych danych i odczyt odpowiedzi z lokalnego portu COM, bez parsowania wagi (do diagnozowania nieznanych protokołów nowych urządzeń).

## [v0.1.24] - 2026-06-12

### Added
- Nowa komenda `read_printer_settings` — agent pobiera konfigurację drukarki bezpośrednio z jej wbudowanego interfejsu webowego (HTTP) i zwraca sparsowane ustawienia do aplikacji Bizanti. Obsługuje Intermec PM43c i kompatybilne drukarki Honeywell.
- Nowe opcjonalne pola w konfiguracji drukarki: `web_port` (domyślnie 80), `web_user`, `web_pass` (do Basic Auth).
- Odpowiedź zawiera `raw_body` (surowy HTML, maks. 4000 znaków) oraz `parsed` (wyodrębnione pary klucz-wartość: wersja firmware, numer seryjny, adres MAC, IP itp.).

### Release
- Wydanie zawiera artefakt `BizantiAgent.exe`.

## [v0.1.14] - 2026-03-12

### Fixed
- Naprawiono self-update z wersji `v0.1.11+` przez staging nowej binarki bezpośrednio w katalogu docelowym przed restartem procesu.
- Zastąpiono prosty skrypt `.cmd` bardziej odpornym flow PowerShell z retry, backupem poprzedniego EXE i logiem aktualizacji w `%ProgramData%/BizantiAgent/logs/update.log`.
- Dodano testy dla generowania skryptu aktualizacji i kopiowania pliku stagingowego.

### Release
- Wydanie zawiera podpisane artefakty `BizantiAgent.exe` oraz `bizanti-agent-v0.1.14-win64.zip` w GitHub Release.

## [v0.1.11] - 2026-02-26

### Fixed
- Zmieniono strategię single-instance na jednorazowy cleanup procesów przy starcie:
	- wykrywanie innych instancji,
	- próba odczytu ich wersji,
	- zamknięcie innych procesów agenta.
- Utwardzono aktualizację: skrypt updatera zamyka uruchomione instancje agenta przed podmianą pliku EXE.

## [v0.1.10] - 2026-02-26

### Fixed
- Usunięto puste okno na pasku zadań w trybie tray (natychmiastowe ukrycie i odłączenie konsoli).

## [v0.1.9] - 2026-02-25

### Fixed
- Poprawiono restart po self-update na Windows (bezpieczne uruchomienie skryptu `.cmd` z poprawnym quotingiem ścieżki).

### Release
- Dodano artefakt `releases/bizanti-agent-v0.1.9-win64.zip`.

## [v0.1.8] - 2026-02-25

### Changed
- Uproszczono konfigurację agenta: usunięto zależność od lokalnych pól `agent_id` i `device_name`.
- Agent korzysta z tożsamości zwróconej przez backend (heartbeat) zamiast ręcznej konfiguracji ID.
- Wspierany format placeholderów rozszerzono o `{key}` obok `{{key}}`.

### Release
- Wydanie podpisane certyfikatem code signing (`Authenticode`).

## [v0.1.7] - 2026-02-25

### Changed
- Poprawiono UX statusu połączenia w tray (`Połączono` po realnym zestawieniu połączenia).
- Utwardzono flow self-update i uruchamiania nowej instancji.

## [v0.1.6] - 2026-02-25

### Added
- Obsługa drukowania przez `windows_spooler` (np. HP OfficeJet).
- Zachowano obsługę `raw_tcp` dla drukarek etykiet.

## [v0.1.5] - 2026-02-25

### Changed
- Osadzono ikonę tray w EXE.
- Wzmocniono mechanizm single-instance.
- Dodano fallback update oparty o tagi.

---

## [v0.1.21] - 2026-06-10

### Added
- Wersja release `v0.1.21` przygotowana do self-update; artefakty binarne powinny zostać zbudowane i dołączone do release (Windows/Linux).

### Fixed
- Poprawiono pobieranie komend HTTP fallback (backend) i domyślną konfigurację websocket w agencie.

### Release
- Przygotowano manifest i numer wersji (`internal/version/version.go`) dla `v0.1.21`.


## Uwagi o podpisie

- `SignatureType: Authenticode` oznacza poprawnie złożony podpis.
- `Status: UnknownError` w `Get-AuthenticodeSignature` przy certyfikacie self-signed zwykle oznacza brak zaufanego root CA na danej maszynie, a nie błąd podpisu.
