# Changelog

Wszystkie istotne zmiany w projekcie BizantiAgent.

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

## Uwagi o podpisie

- `SignatureType: Authenticode` oznacza poprawnie złożony podpis.
- `Status: UnknownError` w `Get-AuthenticodeSignature` przy certyfikacie self-signed zwykle oznacza brak zaufanego root CA na danej maszynie, a nie błąd podpisu.
