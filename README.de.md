# FileDO - Erweiterte Datei- und Speicher-Tools

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/SerZhyAle/FileDO)](https://goreportcard.com/report/github.com/SerZhyAle/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2609241700-blue.svg)](https://github.com/SerZhyAle/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/SerZhyAle/FileDO)

**Speicher-Tests • Leistungsanalyse • Sicheres Löschen • Fake-Kapazität Erkennung • Duplikat-Verwaltung**

</div>

---

## Schnellstart

### Häufigste Aufgaben

```bash
# USB/SD-Karte auf Fälschung prüfen
filedo E: test del

# Festplatten-Leistungstest
filedo C: speed 100

# Sicheres Löschen des freien Speicherplatzes
filedo D: fill 1000 del

# Duplikate suchen und verwalten
filedo C: check-duplicates
filedo D:\Photos cd old del

# Kopieren mit Fortschrittsanzeige
filedo folder C:\Source copy D:\Backup
filedo device E: copy F:\Archive
# Erst den Baum zählen, für eine genaue Restzeit (sonst beginnt das Kopieren mit der ersten Datei)
filedo folder C:\Source copy D:\Backup --precount

# Schnelle Ordnerreinigung
filedo folder C:\Temp wipe
filedo folder D:\Cache w

# Festplatten-Informationen anzeigen
filedo C: info
```

### Installation

#### Variante 1 - winget

```powershell
winget install SerZhyAle.FileDO
```

Installiert die Kommandozeilenwerkzeuge (`filedo`, `filedo_check`, `filedo_fill`, `filedo_test`) und das grafische Fenster `filedo_win` (eine Seite je Aufgabe) und nimmt sie in den `PATH` auf.

Auch im [Microsoft Store](https://apps.microsoft.com/detail/9PH1LPCMRG83) erhältlich - dieselben Werkzeuge, Installation und Updates übernimmt der Store.

#### Variante 2 - Installationsprogramm (Setup-EXE)

`FileDO-<Version>-setup.exe` aus den [Releases](https://github.com/SerZhyAle/FileDO/releases/latest) herunterladen und ausführen. Erst damit wird FileDO ein gewöhnliches Windows-Programm und nicht bloß ein Ordner voller Programmdateien:

- legt die Dateien nach `C:\Program Files\FileDO` und nimmt `filedo` in den System-`PATH` auf;
- erstellt einen **Startmenü-Eintrag** und ein **Desktop-Symbol** für das FileDO-Fenster (`filedo_win.exe`);
- registriert die **Explorer-Integration**: eine Gruppe `File DO..` im Kontextmenü jeder Datei - Secure (Original behalten, löschen oder überschreiben, oder ein zufälliger Containername), Unsecure (optional den Container löschen oder die wiederhergestellte Datei sofort starten), Wipe this file, Check this file, Info - dazu den Dokumenttyp `.fd-sec` mit eigenem Symbol: der Doppelklick ist genau der Eintrag Unsecure and start: eine Konsole fragt dort das Kennwort ab, stellt das Original unter seinem echten Namen in `%LOCALAPPDATA%\FileDO\reveal` wieder her - einem Ordner, den nur dieses Konto und das System lesen können -, übergibt es dem Programm, dem seine echte Erweiterung gehört, und entfernt diese Kopie wieder, sobald das Konsolenfenster geschlossen wird. Unter Windows 11 steht die Gruppe unter *Weitere Optionen anzeigen*.

Das Installationsprogramm ist nicht code-signiert; deshalb zeigt Windows beim ersten Start möglicherweise *Der Computer wurde durch Windows geschützt* und fragt danach nach Administratorrechten. Die SHA256 vergleichen (die `.sha256`-Datei liegt neben dem Download; `certutil -hashfile FileDO-<Version>-setup.exe SHA256`) und dann *Weitere Informationen* und *Trotzdem ausführen* wählen. Warum die Warnung erscheint, wofür die Administratorrechte verwendet werden und was FileDO niemals tut: [Windows warned you about FileDO](https://serzhyale.github.io/FileDO/guides/install-trust.html) (Seite auf EN/RU/UA).

Die beiden letzten Punkte sind Features, die sich auf der Seite „Customize" abwählen und später über **Ändern** in „Apps & Features" ein- oder ausschalten lassen. Unbeaufsichtigt:

```powershell
FileDO-<Version>-setup.exe /quiet
FileDO-<Version>-setup.exe /uninstall
```

Dasselbe Release veröffentlicht auch die blanke `FileDO-<Version>-windows-x64.msi` - genau diese Datei steckt in der Setup-EXE - für Verteilwerkzeuge, die Paket und Feature-Namen direkt brauchen:

```powershell
msiexec /i FileDO-<Version>-windows-x64.msi /qn ADDLOCAL=Main,ExplorerIntegration,DesktopShortcut
msiexec /i FileDO-<Version>-windows-x64.msi /qn ADDLOCAL=Main
```

Das Deinstallieren entfernt alles, was das Installationsprogramm geschrieben hat, die Registrierungseinträge eingeschlossen.

#### Variante 3 - Microsoft Store (MSIX)

Ein Paket, zwei Einstiege: die anklickbare Kachel **FileDO** und der Befehl `filedo` im `PATH`. Die Store-Version hat die Explorer-Einträge **nicht**: ein Paket bekommt sie nur über einen signierten Shell-Handler, und das ist eigene Arbeit. Den Dateityp `.fd-sec` beansprucht sie **doch**: ein Doppelklick auf einen Container öffnet das FileDO-Fenster auf der Seite *Geheime Datei öffnen* mit diesem Container bereits ausgewählt, und dort wird nach dem Passwort gefragt.

#### Variante 4 - Manueller Download

1. **Herunterladen**: `FileDO-<Version>-windows-x64.zip` aus den Releases beziehen und beliebig entpacken
2. **GUI**: `filedo_win.exe` liegt im Archiv - neben `filedo.exe` starten
3. **Ausführung**: Über Kommandozeile oder GUI starten

#### Explorer-Integration ohne Installationsprogramm

winget, das portable Archiv und `go install` führen kein Installationsprogramm aus und registrieren deshalb nichts. Dieselbe Gruppe und derselbe Dokumenttyp lassen sich selbst anfordern - und ebenso zurücknehmen:

```powershell
filedo fdsec register              # für diesen Benutzer
filedo fdsec register -all-users   # für den ganzen Rechner (erhöhte Konsole nötig)
filedo fdsec unregister            # entfernt genau das Geschriebene
```

FileDO entfernt nur, was es selbst markiert hat: ein Dokumenttyp, der inzwischen einem anderen Programm gehört, bleibt unangetastet, und wer `.fd-sec` vor FileDO besaß, bekommt es zurück.

---

## Hauptoperationen

<table>
<tr>
<td width="50%">

### Geräte-Tests
```bash
# Informationen
filedo C: info
filedo D: short

# Fake-Kapazität-Erkennung
filedo E: test
filedo F: test del

# Leistungstest
filedo C: speed 100
filedo D: speed max
```

</td>
<td width="50%">

### Datei- und Ordner-Operationen
```bash
# Ordner-Analyse
filedo C:\temp info
filedo . short

# Leistungstest
filedo C:\data speed 100
filedo folder . speed max

# Netzwerk-Operationen
filedo \\server\share test
filedo network \\nas\backup speed 100

# Stapelverarbeitung
filedo from commands.txt
filedo batch script.lst

# Bereinigung
filedo C:\temp clean
```

</td>
</tr>
</table>

---

## Geheime Dateien (`.fd-sec`)

Eine Datei - oder ein Ordner, sein ganzer Baum - geht in einen Container, hinter ein Passwort, und kommt
wieder heraus - von der Befehlszeile, aus dem Explorer-Menü oder von den Seiten der Gruppe **Schützen**
im Fenster. Der wahre Name des Originals, seine tatsächliche Größe und seine Zeitstempel sind darin
versiegelt, bei einem Ordner auch Pfad, Größe und Zeitstempel jedes Eintrags; der Container selbst
verrät nur seine eigene Größe, seinen sichtbaren Namen und seine Zeitstempel (`rename` erzeugt einen
namenlosen Datenblock). Auf der Platte ist ein Ordner-Container von einem Datei-Container nicht zu
unterscheiden.

```bash
# Einpacken (das Passwort wird zweimal abgefragt, ohne Anzeige)
filedo report.docx secure

# Einpacken und das Original loswerden - wiederherstellbar und die härtere Art
filedo report.docx secure del p:hunter2
filedo report.docx secure wipe p:hunter2

# Unter dem versiegelten Namen oder in diesen Ordner zurückholen
filedo report.fd-sec unsecure
filedo report.fd-sec unsecure here

# Im zugehörigen Programm öffnen, ohne auszupacken
filedo report.fd-sec reveal

# Ein Ordner wird genauso zu einer Datei und kommt als ganzer Baum zurück
filedo "Steuer 2025" secure
filedo "Steuer 2025.fd-sec" unsecure to D:\Restored

# Die leise Suite: ein härterer Schlüssel, keine Spur von Ausrichtung - aber nur FileDO öffnet sie
filedo report.docx secure suite2
```

Ein Ordner wird vollständig eingepackt - leere Unterordner eingeschlossen - und vollständig
wiederhergestellt: zuerst in einen frischen temporären Ordner, an seinen Platz verschoben erst, nachdem
jede Datei geprüft ist, und nie in oder über einen Ordner, der schon existiert (mit `-y` unter einem Namen
mit Zahlensuffix). Ein Ordner mit einer Junction, einer symbolischen Verknüpfung oder einem
Bereitstellungspunkt wird abgelehnt statt verfolgt. `del` und `wipe` entfernen den Originalbaum erst,
nachdem der Container zurückgelesen wurde, und nur, wenn sich der Ordner seit dem Einpacken nicht geändert
hat. `reveal` öffnet eine Datei, lehnt einen Ordner-Container also ab und verweist auf `unsecure`.

`suite2` versiegelt eine Datei (keinen Ordner) mit Suite 2 statt der Standard-Suite 1: ein mit Pepper
gefalteter Argon2id-Schlüssel mit 256 MiB und XChaCha20-Poly1305 in 64-KiB-Frames, sodass die Datei ab dem
ersten Byte Rauschen ist, ohne auch nur das 512-Byte-Clusterlängenmuster von Suite 1. Sie bewahrt den wahren
Namen, die tatsächliche Größe und den Zeitpunkt der Verschlüsselung, aber nicht die eigenen Zeitstempel des
Originals - die wiederhergestellte Datei erhält die aktuelle Zeit. Nur FileDO ab dieser Version öffnet sie:
ältere FileDO-Builds melden sie als beschädigt oder als falsches Passwort, und FastMediaSorter-Apps können
sie nicht öffnen, deshalb bleibt Suite 1 der Standard. `unsecure`, `reveal`, `fdsec info` und `fdsec verify`
erkennen die Suite selbst; sonst ändert sich nichts, und ein leeres Passwort bleibt reine Verschleierung.

Fünf Dinge klar gesagt, denn eine Sicherheitsfunktion, die sich überschätzt, ist schlimmer als gar keine:

- **Ein leeres Passwort ist nur Verschleierung - keine Geheimhaltung.** Es wird angenommen, und jede
  Oberfläche, die ein Passwort entgegennimmt, sagt schon beim Tippen, was es wert ist.
- **`wipe` senkt die Chancen auf Wiederherstellung und verspricht nichts.** Auf SSDs und auf
  Copy-on-Write- oder journalisierenden Dateisystemen garantiert das Überschreiben an Ort und Stelle
  nicht, dass die alten Blöcke weg sind.
- **Eine geöffnete Kopie liegt in `%LOCALAPPDATA%\FileDO\reveal`**, schreibgeschützt und nur für dieses
  Konto und das System lesbar. Sie verschwindet, wenn Sie es sagen, oder wenn das öffnende Programm sie
  freigibt.
- **`unsecure start` - also der Doppelklick - legt seine Kopie in denselben geschützten Ordner**, nicht
  neben den Container, und entfernt sie beim Schließen des Konsolenfensters. Diese Kopie ist Ihre eigene
  Datei und keine schreibgeschützte Ansicht; hat das Programm sie in diesem Moment noch offen, räumt der
  nächste FileDO-Start sie weg. Dauerhaft zurück holt die Datei das schlichte `unsecure`.
- **Nach einem Stromausfall bleibt diese Kopie bis zum nächsten FileDO-Start liegen**, der sie entfernt.
  Sonst tut es nichts.

Es gibt keinen Wiederherstellungsschlüssel: ein vergessenes Passwort ist eine verlorene Datei. Das
Original bleibt erhalten, bis Sie es entfernen lassen, und nichts wird gelöscht, bevor der Container
geschrieben, zurückgelesen und geprüft ist. Programme und Skripte werden nie aus einem Container
gestartet - sie werden ausgepackt und ihr Ort wird angezeigt.

Im Fenster trägt die Gruppe **Schützen** dieselben drei Operationen als Seiten: das Passwort ist
verdeckt, wird beim Einpacken zweimal getippt und `filedo.exe` unsichtbar übergeben - es erreicht keine
Befehlszeile, keinen Laufbericht und keine Verlaufsdatei. Ein Doppelklick auf eine `.fd-sec` öffnet dieses
Fenster nicht - er führt **Unsecure and start** in der Konsole aus, genau wie der gleichnamige
Explorer-Menüeintrag.

---

## Hauptfunktionen

### **Fake-Kapazität-Erkennung**
- **100-Dateien-Test** mit jeweils 1% Kapazität
- **Zufällige Positionsprüfung** - jede Datei wird an eindeutigen zufälligen Positionen überprüft
- **Schutz vor raffinierten Fälschungen** - schlägt Controller, die Daten an vorhersagbaren Positionen speichern
- **Lesbare Muster** - verwendet `ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789` für einfache Korruptionserkennung
- **Schnelles Raw-Sondieren** (`probe`) - 32 Marker per direktem LBA-Zugriff, fertig in ~1 Min (Admin erforderlich)

### **Leistungstest**
- Messung der tatsächlichen Lese-/Schreibgeschwindigkeit
- Optimiertes Streaming für große Dateien
- Fortschrittsverfolgung mit ETA-Berechnung
- Konfigurierbare Dateigrößen (1MB bis 10GB)

### **Datei-Duplikat-Verwaltung**
- **Integrierte Duplikat-Erkennung** - in die Hauptanwendung integriert
- **Mehrere Auswahlmodi** (älteste/neueste nach Erstellungszeit, alphabetisch)
- **Flexible Aktionen** (Duplikate löschen/verschieben) - jede Datei wird einzeln abgefragt, außer mit `-y` (oder `--yes`); ein Lauf ohne Konsole und ohne `-y` wird abgelehnt, bevor irgendetwas angefasst wird
- **Gruppierung per SHA-256 und ein Byte-für-Byte-Vergleich mit der behaltenen Kopie** unmittelbar vor jedem Löschen oder Verschieben; ein Hash, aus dem Cache oder frisch, ist nie der einzige Beweis, und Verschieben ersetzt nie eine vorhandene Datei, auch nicht auf ein anderes Laufwerk
- **Hardlinks und ein zweiter Name derselben Datei** gelten nie als Duplikate; das Stammverzeichnis eines Laufwerks oder einer Freigabe, Windows, Program Files und das System-TEMP verlangen auch mit `-y` eine getippte Bestätigung, und ein Scan überspringt Windows und Program Files, wenn er nicht darin beginnt
- **Hash-Caching** für schnellere Wiederholungsscans - der Cache (`hash_cache.json`) liegt in `%LOCALAPPDATA%\FileDO\state\`, und ein Hash daraus gilt nur, solange Größe, Änderungszeit, Datei-ID und Change Time der Datei übereinstimmen
- **Speichern/Laden von Duplikat-Listen** für Stapelverarbeitung
- **Modulare Architektur** mit dediziertem fileduplicates-Package

### **Sicherheitsfunktionen**
- **Hochgeschwindigkeits-Datenlöschung** um Wiederherstellung zu verhindern
- **Fülloperationen** mit optimierter Puffer-Verwaltung
- **Stapelverarbeitung** für mehrere Ziele
- **Umfassende Operationshistorie** mit JSON-Protokollierung
- **Kontextabhängige Unterbrechung** - Unterstützung für elegante Abbrüche

---

## Befehlsreferenz

### Zieltypen (Automatische Erkennung)
| Muster | Typ | Beispiel |
|--------|-----|----------|
| `C:`, `D:` | Gerät | `filedo C: test` |
| `C:\folder` | Ordner | `filedo C:\temp speed 100` |
| `\\server\share` | Netzwerk | `filedo \\nas\backup test` |
| `file.txt` | Datei | `filedo document.pdf info` |

### Operationen
| Befehl | Zweck | Beispiel |
|--------|-------|----------|
| `info` | Detaillierte Informationen anzeigen | `filedo C: info` |
| `short` | Kurze Zusammenfassung | `filedo D: short` |
| `test` | Fake-Kapazität-Erkennung | `filedo E: test del` |
| `test N` | Test mit N Dateien (Standard 100) | `filedo D: test 1000` |
| `probe` | Schnelles Raw-I/O-Probe (~1 Min, Admin erforderlich) | `filedo D: probe` |
| `speed <größe>` | Leistungstest | `filedo C: speed 500` |
| `fill [größe]` | Mit Testdaten füllen | `filedo D: fill 1000` |
| `clean` | Testdateien löschen | `filedo C: clean` |
| `check-duplicates` | Datei-Duplikate finden | `filedo C: check-duplicates` |
| `cd [modus] [aktion]` | Duplikate prüfen (Kurzform) | `filedo D:\Photos cd old del -y` |
| `from <datei>` | Stapelbefehle ausführen | `filedo from script.txt` |
| `hist` | Operationshistorie anzeigen | `filedo hist` |

### Modifikatoren
| Flag | Zweck | Beispiel |
|------|-------|----------|
| `del` | Automatisches Löschen nach Operation | `filedo E: test del` |
| `nodel` | Testdateien behalten | `filedo C: speed 100 nodel` |
| `short` | Nur kurze Ausgabe | `filedo D: speed 100 short` |
| `max` | Maximale Größe (10GB) | `filedo C: speed max` |
| `old` | Neueste als Original behalten (für cd) | `filedo D:\Photos cd old del` |
| `new` | Älteste als Original behalten (für cd) | `filedo E:\Photos cd new move F:\Dups` |
| `abc` | Alphabetisch letztes behalten (für cd) | `filedo C: cd abc` |
| `xyz` | Alphabetisch erstes behalten (für cd) | `filedo C: cd xyz list dups.lst` |
| `-y`, `--yes` | Duplikate ohne Einzelabfrage löschen/verschieben (für cd; ohne Konsole erforderlich) | `filedo D:\Photos cd old del -y` |

---

## GUI-Anwendung

**FileDO GUI** (`filedo_win.exe`) - VB.NET Windows Forms Shell: links eine Leiste mit den Aufgaben, je Aufgabe eine nummerierte Seite - was bearbeitet wird, welche Parameter, dann Prüfen und Ausführen - plus eine Seite **«Befehl»**, der Experten-Baukasten, der jeden FileDO-Befehl zusammenstellt und ausführt:

- **Visuelle Zielauswahl** mit Optionsfeldern (Gerät/Ordner/Netzwerk/Datei)
- **Operationen-Dropdown** auf der Seite «Befehl» (Info, Geschwindigkeit, Füllen, Test, Bereinigen, Duplikat-Prüfung)
- **Eine Seite je Aufgabe** im Standardfenster - Kapazität, Geschwindigkeit, Info, Prüfung auf beschädigte Dateien, direkte Kapazitätsprüfung, Wiederherstellung, Duplikate, Vergleich, Bereinigen, Kopieren, Füllen, Löschen und die drei Aufgaben für geheime Dateien - jede mit allen Optionen, die die CLI dafür annimmt, einschließlich aller neunundzwanzig `check`-Schalter
- **Parameter-Eingabe** mit Validierung
- **Echtzeit-Befehlsvorschau** auf der Seite «Befehl» zeigt äquivalenten CLI-Befehl
- **Durchsuchen-Button** für einfache Pfad-Auswahl
- **Fortschrittsverfolgung** mit Echtzeit-Ausgabe
- **Ein-Klick-Ausführung** mit Ausgabe-Anzeige
- **Seite «Über»**: Build, Autor und Links zur Website, zum Quellcode, zum Issue-Tracker, zur Datenschutzseite und zu den weiteren Programmen des Autors
- **Logs an den Autor senden** - die eine Schaltfläche auf dieser Seite packt die auf diesem Rechner gefundenen FileDO-Logs in ein Zip, öffnet den Ordner mit markierter Datei, legt den Pfad in die Zwischenablage und öffnet Ihr Mailprogramm mit ausgefüllter Adresse und Betreff. Das Zip sehen Sie zuerst selbst, und gesendet wird nichts, bevor Sie die Mail selbst abschicken

```bash
# Starten aus dem filedo_win_vb Ordner
filedo_win.exe          # Windows GUI Interface
```

Die Seiten unter **«Schützen»** behandeln geheime `.fd-sec`-Dateien - eine Datei geheim machen, das Original zurückholen oder sie ohne Entpacken öffnen. Das Passwort ist maskiert und gelangt nie in eine Kommandozeile. Wird dem Fenster ein `.fd-sec`-Pfad direkt übergeben, öffnet es sich auf der Seite dieses Containers (`filedo_win.exe C:\a\x.fd-sec`); ein Doppelklick im Explorer nutzt das Fenster gar nicht - er führt **Unsecure and start** in der Konsole aus, wie oben beschrieben. «Verlauf», «Einstellungen» und «Über» haben eigene Seiten; das ältere Befehlsbaukasten-Fenster ist außer Dienst gestellt, und eine von Hand geschriebene Befehlszeile gehört auf die Seite **«Befehl»**.

**Funktionen:**
- Mit VB.NET Windows Forms für native Windows-Erfahrung gebaut
- Automatische Befehlsvalidierung und Parameter-Überprüfung
- Echtzeit-Ausgabe mit Farbcodierung
- Integration mit der Haupt-CLI-Anwendung

---

## Erweiterte Funktionen

### Ordnervergleich & Bereinigung

```bash
# Zwei Ordner vergleichen und Bericht speichern
filedo compare D:\Data E:\Backup

# Vergleichen und löschen (permanent, ohne Rückfrage)
filedo cmp D:\Data E:\Backup del source  # in Source löschen, wenn in Target vorhanden
filedo cmp D:\Data E:\Backup del target  # in Target löschen, wenn in Source vorhanden
filedo cmp D:\Data E:\Backup del old     # ältere Seite löschen (mtime), Gleichheit: überspringen
filedo cmp D:\Data E:\Backup del new     # neuere Seite löschen (mtime), Gleichheit: überspringen
filedo cmp D:\Data E:\Backup del small   # kleinere Seite löschen, Gleichheit: überspringen
filedo cmp D:\Data E:\Backup del big     # größere Seite löschen, Gleichheit: überspringen
 
# Optionale Seiten-Einschränkung
filedo cmp D:\Data E:\Backup del small source  # nur wenn kleiner auf Source
filedo cmp D:\Data E:\Backup del big target    # nur wenn größer auf Target
filedo cmp D:\Data E:\Backup del old target    # nur wenn älter auf Target
filedo cmp D:\Data E:\Backup del new source    # nur wenn neuer auf Source
```

Hinweise: Abgleich per relativem Pfad; `del source` und `del target` löschen ein Paar nur, wenn Größe und Änderungszeit übereinstimmen (`--by-hash`: gleicher Inhalt; `--allow-mismatch`: jedes Paar) - ein abweichendes Paar wird gemeldet und bleibt; zwei Schreibweisen desselben Ordners oder ein Ordner im anderen werden abgelehnt; mtime für old/new; Windows ohne Groß-/Kleinschreibung, gelöscht wird unter dem echten Dateinamen; was nicht gelesen oder gelöscht werden konnte, endet mit Exit-Code 2; Logs: compare_report_*.log, delete_report_<mode>_*.log.


### Stapelverarbeitung
`commands.txt` erstellen:
```text
# Mehrere Geräte prüfen
device C: info
device D: test del
device E: speed 100
folder C:\temp clean
```

Ausführen: `filedo from commands.txt`

Exit-Codes (alle Nicht-Container-Befehle): **0 passed or done, 1 ran and found a defect, 2 could not be verified.**

### Historien-Verfolgung
```bash
filedo hist              # Letzte 10 Operationen anzeigen
filedo history           # Befehlshistorie anzeigen
# Historie wird automatisch für alle Operationen geführt
```

### Unterbrechungsunterstützung
```bash
# Alle langen Operationen unterstützen Ctrl+C Unterbrechung
# Eleganter Abbruch mit Bereinigung
# Kontextabhängige Unterbrechung an optimalen Punkten
```

### Netzwerk-Operationen
```bash
# SMB-Freigaben und Netzwerklaufwerke
filedo \\server\backup speed 100
filedo \\nas\storage test del
filedo network \\pc\share info
```

---

## Wichtige Hinweise

> **Fake-Kapazität-Erkennung**: Erstellt 100 Dateien (jeweils 1% Kapazität) mit **kontextabhängiger Unterbrechungsunterstützung**. Verwendet moderne zufällige Prüfmuster und optimierte Puffer-Verwaltung für zuverlässige Erkennung.

> **Verbesserte Unterbrechung**: Alle langen Operationen unterstützen **eleganten Ctrl+C-Abbruch** mit automatischer Bereinigung. Kontextabhängige Unterbrechungsprüfungen an optimalen Punkten für sofortige Reaktionsfähigkeit.

> **Sicheres Löschen**: `fill <größe> del` überschreibt freien Speicherplatz mit optimierter Puffer-Verwaltung und kontextabhängigem Schreiben für sichere Datenlöschung.

> **Testdateien**: Erstellt `FILL_*.tmp` und `speedtest_*.txt` Dateien. `clean` entfernt nur die, die FileDO geschrieben hat - mit FileDO-Namen und FileDO-Inhalt, sonst nichts -, nachdem es sie aufgelistet und nachgefragt hat; `--yes` beantwortet die Frage.

> **Gemeinsame Packages**: `fileduplicates` (Duplikaterkennung), `fsx` (Pfadidentität und atomares Schreiben ohne Ersetzen) und `statedir` (das Zustandsverzeichnis, `%LOCALAPPDATA%\FileDO\state`).

---

## Anwendungsbeispiele

<details>
<summary><b>USB/SD-Karten Authentizitätsprüfung</b></summary>

```bash
# Schnelltest mit Bereinigung
filedo E: test del

# Detaillierter Test, Dateien für Analyse behalten
filedo F: test

# Zuerst Festplatten-Infos prüfen
filedo E: info
```
</details>

<details>
<summary><b>Leistungs-Benchmark</b></summary>

```bash
# Schneller 100MB Test
filedo C: speed 100 short

# Maximaler Leistungstest (10GB)
filedo D: speed max

# Netzwerk-Geschwindigkeitstest
filedo \\server\backup speed 500
```
</details>

<details>
<summary><b>Sicheres Datenlöschen</b></summary>

```bash
# 5GB füllen dann sicher löschen
filedo C: fill 5000 del

# Vorhandene Testdateien bereinigen
filedo D: clean

# Vor Festplatten-Entsorgung
filedo E: fill max del
```
</details>

<details>
<summary><b>Duplikat-Suche und Verwaltung</b></summary>

```bash
# Duplikate im aktuellen Verzeichnis finden
filedo . check-duplicates

# Alte Duplikate finden und löschen (mit Abfrage je Datei)
filedo D:\Photos cd old del

# Dasselbe ohne Abfrage je Datei
filedo D:\Photos cd old del -y

# Neue Duplikate finden und in Backup verschieben
filedo E:\Photos cd new move E:\Backup

# Duplikat-Liste für spätere Verarbeitung speichern
filedo D: cd list duplicates.lst

# Gespeicherte Liste mit spezifischer Aktion verarbeiten
filedo cd from list duplicates.lst xyz del
```
</details>

---

## Technische Details

### Architektur
- **Modulares Design**: Aufgeteilt in spezialisierte Packages für bessere Wartbarkeit
- **Kontextabhängige Operationen**: Alle langen Operationen unterstützen eleganten Abbruch
- **Einheitliches Interface**: Gemeinsames `Tester` Interface für alle Speichertypen
- **Speicher-Optimierung**: Streaming-Operationen mit optimierter Puffer-Verwaltung
- **Plattformübergreifend**: Hauptunterstützung für Windows mit portabler Go-Codebasis

### Package-Struktur
```
FileDO/
├── main.go                    # Anwendungs-Einstiegspunkt
├── fsx/                      # Pfadidentität und atomares Schreiben
├── statedir/                 # Ort der Zustandsdateien (%LOCALAPPDATA%\FileDO\state)
├── fileduplicates/           # Datei-Duplikat-Verwaltung
│   ├── types.go              # Duplikat-Erkennungs-Interfaces
│   ├── duplicates.go         # Haupt-Duplikat-Logik
│   ├── duplicates_impl.go    # Implementierungsdetails
│   └── worker.go             # Hintergrundverarbeitung
├── filedo_win_vb/           # VB.NET GUI-Anwendung
│   ├── FileDOGUI.sln        # Visual Studio Solution
│   ├── Program.vb           # Einstiegspunkt; das Fenster ist ShellForm.vb
│   └── bin/                 # Kompilierte GUI-Ausführdatei
├── command_handlers.go       # Befehlsverarbeitung
├── device_windows.go         # Geräte-Operationen
├── folder.go                 # Ordner-Operationen
├── network_windows.go        # Netzwerk-Speicher-Operationen
├── interrupt.go              # Unterbrechungsbehandlung
├── progress.go               # Fortschrittsverfolgung
├── main_types.go             # Legacy-Typdefinitionen
├── history.json              # Operationshistorie
└── hash_cache.json           # Hash-Cache für Duplikate
```

### Schlüsselfunktionen
- **Verbesserter InterruptHandler**: Thread-sichere Unterbrechung mit Kontext-Unterstützung
- **Optimierte Puffer-Verwaltung**: Dynamische Puffergrößenanpassung für optimale Leistung
- **Umfassende Tests**: Fake-Kapazität-Erkennung mit zufälliger Verifikation
- **Duplikat-Erkennung**: SHA-256-basierter Dateivergleich mit Caching und Byte-für-Byte-Prüfung vor jedem Löschen oder Verschieben
- **Stapelverarbeitung**: Skriptausführung mit Fehlerbehandlung
- **Historienführung**: JSON-basierte Operationsverfolgung

---

## Versionshistorie

**v2609241700** (Aktuell)
- **Geheime Dateien (`.fd-sec`)**: eine Datei wird in einen passwortgeschützten Container gepackt und wieder herausgeholt - `secure`, `unsecure`, `reveal` - per Kommandozeile, über das Explorer-Menü oder auf den Protect-Seiten des Fensters; Name, Größe und Zeitstempel des Originals sind darin versiegelt
- **Explorer-Integration**: das Setup fügt die Kontextmenügruppe `File DO..` (Secure, Unsecure, Wipe this file, Check this file, Info) und den Dokumenttyp `.fd-sec` hinzu; `filedo fdsec register` / `unregister` erledigt dasselbe ohne Installer
- **GUI**: ein neues Fenster - links die Aufgaben, pro Aufgabe eine Seite mit allen CLI-Optionen, dazu die Seiten Command, History, Settings und About; das alte Befehlsbaukasten-Fenster entfällt
- **Kopieren**: beginnt mit der ersten Datei; `--precount` zählt den Baum vorher für exakte Summen und Restzeit
- **CLI**: einheitliche Exit-Codes - 0 bestanden oder erledigt, 1 Defekt gefunden, 2 nicht prüfbar; Zugangsdaten werden entfernt, bevor etwas in den Verlauf gelangt
- **Vertrieb**: FileDO ist im Microsoft Store; `THIRD-PARTY-NOTICES.txt` liegt im Zip, im MSI und im Store-Paket
- **Dokumentation**: neue Anleitungen - geheime Dateien und "Windows warned you about FileDO"

**v2607301014** (Vorherige)
- **GUI**: Oberfläche in 5 Sprachen (Englisch, Russisch, Ukrainisch, Deutsch, Französisch) mit Sprachwechsel zur Laufzeit und App-Symbol
- **GUI**: "Über"-Fenster mit Funktion zum Senden der Protokolle an den Autor
- **Datenschutz**: veröffentlichte Datenschutzseite, verlinkt von der Website und dem Store-Eintrag
- **CLI**: neuer Slogan "pleasantly paranoid" und klarerer Hilfetext
- **Dokumentation**: neu gestaltete Website mit neuen Schritt-für-Schritt-Anleitungen (Speicherprüfung, Dateien und Kopien, GUI-Befehlsgenerator)
- **Projekt**: Quellcode unter `cmd/` reorganisiert (filedo, filedo-check, filedo-fill, filedo-test); getrennte Build- und Release-Abläufe

**v2606120121**
- **Wipe**: verstärkte Sicherheitsprüfungen beim Löschen
- **Duplikate**: Korrektheit und Parallelität des Duplikat-Caches korrigiert
- **Kopieren**: Verzeichnisdurchläufe beim Kopieren reduziert

**v2605152056**
- **Installer**: Setup-EXE (ein WiX-Bundle mit dem MSI darin) und das blanke MSI; App-Symbol in alle EXEs und den Eintrag der installierten Programme eingebettet
- **Store**: Microsoft-Store-Einreichungsspezifikation und Vorschaubild

**v2604272228**
- **Distribution**: erste winget-Veröffentlichung (SerZhyAle.FileDO) und GitHub-Actions-Release-Pipeline
- **Binärdateien**: PE-Versionsinformationen und Windows-Anwendungsmanifest in alle Binärdateien eingebettet
- **Dokumentation**: GitHub-Pages-Seiten neu gestaltet und vereinfacht

**v2507112115**
- **Große Refaktorierung**: Kapazitätstest-Logik in dediziertes `capacitytest` Package extrahiert
- **Verbesserte Unterbrechung**: Kontextabhängige Abbrüche mit thread-sicherem `InterruptHandler` hinzugefügt
- **Verbesserte Leistung**: Optimierte Puffer-Verwaltung und Verifikations-Algorithmen
- **Bessere Architektur**: Modulares Design mit klarer Trennung der Verantwortlichkeiten
- **VB.NET GUI**: Aktualisierte Windows Forms Anwendung mit besserer Integration

**v2507082120**
- Datei-Duplikat-Erkennung und Verwaltung hinzugefügt
- Mehrere Duplikat-Auswahlmodi (old/new/abc/xyz)
- Hash-Caching für schnellere Duplikat-Scans
- Unterstützung für Speichern/Laden von Duplikat-Listen
- GUI-Anwendung mit Duplikat-Verwaltungsfunktionen

**v2507062220** (Frühere)
- Verbessertes Verifikationssystem mit Multi-Position-Prüfung
- Lesbare Textmuster für Korruptionserkennung
- Verbessertes Fortschritts-Display und Schutzmechanismen
- Fehlerbehebungen und Verbesserungen der Fehlerbehandlung

---

<div align="center">

**FileDO v2609241700** - Erweiterte Datei- und Speicher-Tools

Erstellt von **sza@ukr.net** | [MIT-Lizenz](LICENSE) | [GitHub-Repository](https://github.com/SerZhyAle/FileDO) | [Universal Agent Kit](https://serzhyale.github.io/universal-agent-kit/)

---

### Neueste Verbesserungen

- **Modulare Architektur**: Refaktoriert in spezialisierte Packages (`capacitytest`, `fileduplicates`)
- **Verbesserte Unterbrechung**: Kontextabhängige Abbrüche mit eleganter Bereinigung
- **Thread-sichere Operationen**: Verbesserter `InterruptHandler` mit Mutex-Schutz
- **Bessere Leistung**: Optimierte Puffer-Verwaltung und Verifikations-Algorithmen
- **Aktualisierte GUI**: VB.NET Windows Forms Anwendung mit verbesserter Integration

</div>
