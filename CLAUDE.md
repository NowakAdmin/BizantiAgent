# CLAUDE.md — BizantiAgent

Wiedza operacyjna dla pracy nad tym repo (build/deploy/debug/RE), żeby kolejne
sesje nie musiały tego odkrywać od nowa. Dla wiedzy user-facing (protokół
wiadomości, konfiguracja drukarek, format `config.json`) patrz [README.md](README.md) —
tu jest tylko to, czego README nie pokrywa.

## Środowisko builda na tym serwerze (VPS)

Go nie jest domyślnie na `PATH`:

```bash
export PATH=$PATH:/usr/local/go/bin
```

`go build ./...` (pełny build, bez `GOOS=windows`) **zawsze failuje** na dwóch
niezwiązanych z niczym rzeczach — to nie regresja, tak jest zawsze na tym
Linuksowym VPS:
- `github.com/getlantern/systray` — brakuje `ayatana-appindicator3-0.1`
  (biblioteka GUI Linuksa, nieistotna przy cross-compile na Windows).
- `internal/update/updater.go` — używa `syscall.SysProcAttr.HideWindow`,
  pola dostępnego tylko przy `GOOS=windows`.

Testy działają normalnie mimo tego: `go test ./internal/devices/...` (albo
`./...` — pakiety inne niż `cmd/bizanti-agent`/`internal/update` kompilują się
czysto na Linuksie).

## Build lokalny / debug

Binarka z komendami debugowymi (patrz niżej) do szybkiej iteracji bez
publikowania release'u:

```bash
export PATH=$PATH:/usr/local/go/bin
cd /var/www/bizanti-dev-modules/BizantiAgent
GOOS=windows GOARCH=amd64 go build -tags debugtools -o BizantiAgent-debug.exe ./cmd/bizanti-agent
```

Dostarczenie userowi na fizyczną maszynę Windows (ma dostęp SSH do VPS-a):

```bash
scp nowakadmin@vps-bec15569:/var/www/bizanti-dev-modules/BizantiAgent/BizantiAgent-debug.exe .
```

**Zawsze pytaj usera, czy chce debug build czy pełny release** — debug build
jest szybszy do iteracji i weryfikacji na sprzęcie przed zrobieniem release'u
(patrz `feedback` memory o tym workflow).

## Deploy na produkcję (release)

`scripts/build-release.sh` — w pełni zautomatyzowany, uruchamiany z tego
repo na VPS-ie:

```bash
export PATH=$PATH:/usr/local/go/bin  # skrypt sam to robi, ale go-winres install poniżej potrzebuje też tego
./scripts/build-release.sh
```

Co robi (kolejno):
1. Podbija patch version w `internal/version/version.go`, commituje.
2. Synchronizuje wersję do `winres/winres.json`, regeneruje `.syso` przez
   `go-winres` (instaluje `go-winres` przez `go install` jeśli brak na PATH —
   `$(go env GOPATH)/bin`).
3. Builduje `BizantiAgent.exe` (`GOOS=windows GOARCH=amd64`,
   `-ldflags "-H=windowsgui -s"` — **celowo bez `-w`**: w pełni zstrippowana
   binarka podnosi ML-heurystykę "packed" u antywirusów).
4. Weryfikuje, że binarka faktycznie zawiera nowy string wersji (`strings |
   grep`) — przerywa przed publikacją stale builda, jeśli nie.
5. Commituje `BizantiAgent.exe` + `winres.json` + `.syso`, tagguje `vX.Y.Z`,
   pushuje `main` i tag.
6. Tworzy GitHub Release + uploaduje `.exe` jako asset, przez GitHub API
   (token z `~/.git-credentials`).

To jest akcja z realnym efektem na zewnątrz (nowy publiczny release, podbity
tag) — **zawsze potwierdź z userem** przed odpaleniem, nie rób tego
automatycznie po prostu dlatego, że coś naprawiłeś.

Inne skrypty w `scripts/` (mniej używane / Windows-side, nie na tym VPS):
`build-release.ps1` (odpowiednik PowerShellowy), `build-and-sign.ps1`
(podpisywanie certyfikatem przez sąsiedni pakiet `../SoftwareSigner` —
`build-release.sh` NIE podpisuje binarki), `build-windows.ps1` (prosty dev
build bez release/signing, do `dist/`).

## Wykonywanie komend na konkretnym tenancie przez tinker

Do testowania/weryfikacji zmian bez czekania na akcję w Nova UI — przełącz
aktywne połączenie DB na tenanta, potem zwykłe Eloquent:

```bash
php artisan tinker --execute '
$tenant = \NowakAdmin\BizantiCore\Models\Tenant::find(2);
$tenant->makeCurrent();

$label = \NowakAdmin\BizantiManufacturing\Models\Label::find(5);
echo json_encode($label->details) . "\n";
'
```

`$tenant->makeCurrent()` (Spatie Multitenancy) przełącza połączenie `tenant`
na bazę tego konkretnego tenanta (`dev_biz4nti_1`/`dev_biz4nti_2` na dev) dla
reszty procesu. Bez tego Eloquent nie wie, na której bazie tenant-connection
ma operować.

**Uwaga:** zapisy (`->save()`) przez tinker w tym trybie modyfikują REALNY
rekord na tej bazie. Jeśli user ma akurat otwartą stronę edycji tego samego
rekordu w Nova, jego kolejny zapis dostanie błąd konfliktu wersji Nova
("Inny użytkownik dokonał zmian...") — to nie bug, to poprawne zachowanie
Nova (`_retrieved_at` vs `updated_at`). Uprzedź o tym, jeśli testujesz na
rekordzie, który user może mieć otwarty.

## Migracje tenantów aplikowane ręcznie przez PDO

Jeśli `php artisan tenants:artisan migrate` jest zablokowane (np. przez
niepowiązany konflikt) i migracja zostaje zaaplikowana ręcznie przez PDO na
bazie tenanta zamiast przez `artisan migrate` — **koniecznie dopisz też
wiersz do tabeli `migrations` tego tenanta** (`INSERT INTO migrations
(migration, batch) VALUES ('nazwa_pliku_bez_php', max(batch)+1)`), inaczej
kolejne uruchomienie prawdziwego `migrate` spróbuje odtworzyć tę migrację od
zera na już docelowym schemacie i wybuchnie błędem "Unknown column" (już się
tak zdarzyło).

## Architektura urządzeń (`internal/devices/`)

| Plik | Rodzina | Payload / command_type | Rola |
|---|---|---|---|
| `dibal.go` | Dibal K-235/K-265 (Lantronix, rejestr tekstowy) | `DibalPLU` (bez tagów JSON! klucze PascalCase dosłownie: `Group`,`Mode`,`Code`,`Name`,`Price`,`Barcode`,`LabelNum`) w `DibalProgramPayload`, `command_type=program_dibal_plu` | Prosty, 7 pól, bez składu/ważności/promocji |
| `dibal500.go` | Dibal 500-series (W-025S, natywny `commL.dll`) | `Dibal500PLU` (snake_case JSON) w `Dibal500ProgramPayload`, `command_type=program_dibal_plu_500` | Rejestry L2 (dane artykułu)/L3 (daty, format, kod kreskowy)/L4 (Tek1-10, skład)/X4 (wolny tekst)/AS (EAN stały) — pełny opis bajtowy w komentarzach `dibal500.go` przy każdej funkcji `BuildXXRegister` |
| `dibal_manager.go` | wspólny trwały TCP manager dla obu rodzin Dibala | `DibalManagerConfig` | Trzyma RX/TX porty połączone |
| `printer.go` | generyczny transport drukarki (raw TCP/JetDirect, Windows spooler) | `PrinterConfig`; treść to gotowy, wyrenderowany string | ZPL i Dibal-jako-drukarka; brak dekompozycji pól — Bizanti renderuje cały szablon po swojej stronie |
| `intermec.go` | Intermec/Honeywell PM43c | `read_printer_settings` | Tylko diagnostyka (scrape strony konfiguracyjnej drukarki), nie wysyła danych etykiety |
| `scale.go` | generyczny odczyt wagi (serial/TCP) | `ScaleConfig`, `command_type=read_weight` | Bez koncepcji PLU |

`internal/agent/agent.go` `executeCommand()` — pełna lista `command_type`:
`weigh_and_print`, `print_label`, `read_weight`, `tcp_probe`,
`program_dibal_plu`, `program_dibal_plu_500`, `ping_device`,
`read_printer_settings`, `agent_version`, `list_serial_ports`,
`serial_probe`.

### Most 32-bitowy (`cmd/dibalcom-bridge/`)

`commL.dll` (natywny protokół Dibal 500-series) jest **tylko i386** — agent
jest 64-bit i nie może go załadować bezpośrednio. Dlatego 64-bit agent
odpala osobny proces 32-bit (`dibalcom-bridge.exe`, `GOOS=windows
GOARCH=386`) przez `os/exec`, komunikacja przez stdin (hex-encoded 130-bajtowe
rejestry, jeden per linia) / stdout (JSON z wynikiem per rejestr).

## Narzędzia debugowe (build tag `debugtools`)

Gated w `internal/agent/debugtools.go` (`//go:build debugtools`, stub w
`debugtools_stub.go` dla normalnego builda). Buduj z `-tags debugtools`
(patrz sekcja "Build lokalny / debug" wyżej). Komendy:

- **`dibal500_raw`** — wysyła ręcznie skonstruowane hex-encoded rejestry
  130-bajtowe bezpośrednio przez most, z pominięciem `BuildArticleRegisters`.
  **Kluczowe narzędzie** przy każdym RE nowego pola: pozwala hand-craftować
  bajty i sprawdzić na żywo na wadze, co się wydarzy, zanim napiszesz kod
  produkcyjny. Tak znaleziono m.in. bug "2 bajty/znak" w L4.
- `tcp_capture` — pasywny listener TCP, zrzuca surowe bajty.
- `ssh_exec`, `port_scan` — ogólne narzędzia sieciowe.

## Dodawanie nowego sprzętu / pola — checklist

1. **RE protokołu** (jeśli producent nie ma otwartej dokumentacji): patrz
   sekcja "Zasoby do reverse-engineeringu" niżej — sprawdź najpierw, czy dany
   producent/model ma już coś zdekompilowane.
2. Zbuduj funkcję `BuildXXRegister`/strukturę payloadu w
   `internal/devices/<urzadzenie>.go`, z testami (`go test
   ./internal/devices/...`).
3. Dodaj `case` w `executeCommand()` (`internal/agent/agent.go`) dla nowego
   `command_type`.
4. Zweryfikuj na sprzęcie przez `dibal500_raw`-podobne narzędzie debugowe
   (albo napisz analogiczne dla nowego urządzenia) **przed** podpięciem do
   normalnego flow — taniej złapać błąd na surowych bajtach niż przez całą
   ścieżkę Nova → agent → most.
5. Po stronie Bizanti (PHP, `BizantiManufacturing`):
   - Pola specyficzne dla typu etykiety/urządzenia idą do
     `src/Support/LabelFieldCatalog.php` (JSON w `Label.details[type]`,
     **zero migracji** — to jest cały sens tego katalogu, dodanie pola to
     jeden wpis w tablicy).
   - Mapowanie Produkt → payload PLU: `src/Support/DibalPluBuilder.php`
     (jedyne miejsce, które czyta katalog + `Product`, żeby dodać kolejne
     źródło danych, dodaj tu jedną linię, nie duplikuj w akcji Nova).
   - Payload do agenta: `src/Nova/Actions/ProgramDibalPluViaAgent.php` — klucze
     `plu` MUSZĄ dokładnie odpowiadać JSON-tagom struktury Go (snake_case dla
     500-series, dosłowne PascalCase dla K-series — patrz tabela wyżej).
   - Nova/Label.php **nie wymaga zmian** dla nowych pól typu
     `product_meta_select`/`text`/`boolean` w katalogu — generuje je
     automatycznie z `LabelFieldCatalog::all()`.
6. Testy: `tests/Packages/BizantiManufacturing/Feature/DibalPluBuilderTest.php`
   (i analogiczne dla nowego urządzenia), plus `LabelFieldCatalogTest.php`.
7. Migracja potrzebna tylko gdy dodajesz **nowy typ** etykiety/urządzenia do
   `LabelFieldCatalog::types()` (Select "Label Type" w Nova) — nie dla
   nowego pola w istniejącym typie.

## Zasoby do reverse-engineeringu (Dibal)

- Kopia instalacji DFS klienta (MySQL dump + DLL-e + configi):
  `/home/nowakadmin/bizanti-docs-files/Dibal/DFS installed files from customer pc/DFS/`
  - `DBSQL/SYS_DATOS_DFS_V101.sql` — **pełny schemat + dane** bazy DFS,
    w tym tabela `sys_campos_etiqueta` (katalog pól etykiety DLD: numer
    pola/AP-XXX widoczny na wadze, nazwa, kolumna źródłowa w `dat_articulo`
    lub `NULL` jeśli to statyczny tekst literałowy) i pełna definicja tabeli
    `dat_articulo` (z komentarzami PL/EN po angielsku i hiszpańsku dla
    każdej kolumny — najszybszy sposób ustalenia znaczenia pola bez
    dekompilacji).
  - `DFS/ComunicacionesBalPC.dll` (i kopie w `RGI/`, `CDA/`, `DLD/`, `GDA/`
    — identyczne) — assembly .NET z generatorami rejestrów
    (`GenerarL2`/`GenerarL3`/`GenerarH2`/`GenerarH3`/`GenerarAS`/... —
    `Generar<TYP>_EnBytes` dla wersji bajtowej). **L-series i H-series to
    różne, równoległe generatory** dla różnych modeli/wersji wagi — sprawdź
    obie, jeśli pole którego szukasz nie występuje w tej, którą już
    zaimplementowałeś (np. `FechaCongelacion`/data zamrożenia w innym sensie
    niż `FechaEnvasado` istnieje tylko w `GenerarH3`, nie w `GenerarL3`).
- **Już zdekompilowane źródła** (trwałe, nie w `/tmp` — scratchpad sesji
  Claude jest ulotny i znika):
  `/home/nowakadmin/bizanti-docs-files/Dibal/decompiled/ComunicacionesBalPC.decompiled.cs`
  (~48.5k linii) i `FuncionesCom.decompiled.cs` (~6.1k linii). Grep tutaj
  najpierw, zanim dekompilujesz cokolwiek ponownie.
- Regeneracja dekompilacji (gdyby trzeba było dekompilować kolejny DLL —
  `ilspycmd` jest zainstalowany, ale wymaga wymuszenia roll-forward runtime,
  bo celuje w net6.0 a na maszynie jest tylko .NET 8):
  ```bash
  export DOTNET_ROOT=/home/nowakadmin/.dotnet
  export PATH=$DOTNET_ROOT:$DOTNET_ROOT/tools:$PATH
  export DOTNET_ROLL_FORWARD=LatestMajor
  ilspycmd -o /tmp/decompiled_out "<ścieżka_do.dll>"
  ```
- Znane, potwierdzone ustalenia (nie odkrywaj ponownie):
  - **"Przechowuj zamrożone" / "-18°C" / "w chłodzie" to statyczny tekst
    literałowy wklejony w layout formatu w DLD** (`txt_Conservar`/`txt_A18C`/
    `txt_EnFrio`, kategoria `TextosLiterales`, `ValorEnBD = NULL` w
    `sys_campos_etiqueta`) — **nie da się tego sterować żadnym rejestrem
    danych**, zero bajtów w L2/L3/L4/X4/AS to zmieni. Fix zawsze w edycji
    formatu na wadze/w DLD, nigdy w kodzie. Potwierdzone też empirycznie:
    ręczne czyszczenie daty na klawiaturze wagi i tak zostawia napis.
  - L3 "Envasado" (pakowanie/zamrożenie, offset [25:31]) to **6 cyfr bez
    osobnej flagi aktywacji w samym rejestrze** — oryginalny DFS sam
    decyduje po swojej stronie (kolumna `FechaEnvasadoActivada`, nigdy
    nie wysyłana do wagi), czy wpisać tam datę czy licznik dni. Waga zawsze
    interpretuje te 6 bajtów tak samo. "000000" (zero-fill) waga czyta jako
    poprawną datę 00/00/00, nie jako "brak" — dlatego dla braku daty trzeba
    wpisać spacje, nie zera (patrz `BuildL3Register` w `dibal500.go`).
  - Pola AP-220 do AP-239 na wadze ("Pytanie 1..20 produktu") to generyczny
    system pytań `Trazabilidad` (`dat_param_clase.Parametro`), niezwiązany z
    wartościami odżywczymi mimo pozornego podobieństwa do wcześniej
    badanego "Nutritional Questions" — to ten sam mechanizm co
    `GenerarRegistrosPreguntasNutricionales`.
  - AP-031 "Ahorro"/"Saving" to **cena promocyjna (oszczędność)**, nie
    "zapisywanie"/storage — angielskie tłumaczenie na wadze jest mylące.
