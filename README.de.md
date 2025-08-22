# FileDO - Erweiterte Datei- und Speicher-Tools

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/SerZhyAle/FileDO)](https://goreportcard.com/report/github.com/SerZhyAle/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2507112115-blue.svg)](https://github.com/SerZhyAle/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/SerZhyAle/FileDO)

**🔍 Speicher-Tests • 🚀 Leistungsanalyse • 🛡️ Sicheres Löschen • 🎯 Fake-Kapazität Erkennung • 📁 Duplikat-Verwaltung**

</div>

---

## 🎯 Schnellstart

### ⚡ Häufigste Aufgaben

```bash
# USB/SD-Karte auf Fälschung prüfen
filedo E: test del

# Festplatten-Leistungstest
filedo C: speed 100

# Sicheres Löschen des freien Speicherplatzes
filedo D: fill 1000 del

# Duplikate suchen und verwalten
filedo C: check-duplicates
filedo D: cd old del

# Kopieren mit Fortschrittsanzeige
filedo folder C:\Source copy D:\Backup
filedo device E: copy F:\Archive

# Schnelle Ordnerreinigung
filedo folder C:\Temp wipe
filedo folder D:\Cache w

# Festplatten-Informationen anzeigen
filedo C: info
```

### 📥 Installation

1. **Herunterladen**: `filedo.exe` aus den Releases beziehen
2. **GUI-Version**: Zusätzlich `filedo_win.exe` für grafische Benutzeroberfläche (VB.NET) herunterladen
3. **Ausführung**: Über Kommandozeile oder GUI starten

---

## 🔧 Hauptoperationen

<table>
<tr>
<td width="50%">

### 💾 Geräte-Tests
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

### 📁 Datei- und Ordner-Operationen
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

## 🌟 Hauptfunktionen

### 🎯 **Fake-Kapazität-Erkennung**
- **100-Dateien-Test** mit jeweils 1% Kapazität
- **Zufällige Positionsprüfung** - jede Datei wird an eindeutigen zufälligen Positionen überprüft
- **Schutz vor raffinierten Fälschungen** - schlägt Controller, die Daten an vorhersagbaren Positionen speichern
- **Lesbare Muster** - verwendet `ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789` für einfache Korruptionserkennung

### ⚡ **Leistungstest**
- Messung der tatsächlichen Lese-/Schreibgeschwindigkeit
- Optimiertes Streaming für große Dateien
- Fortschrittsverfolgung mit ETA-Berechnung
- Konfigurierbare Dateigrößen (1MB bis 10GB)

### 🔍 **Datei-Duplikat-Verwaltung**
- **Integrierte Duplikat-Erkennung** - in die Hauptanwendung integriert
- **Mehrere Auswahlmodi** (älteste/neueste/alphabetisch)
- **Flexible Aktionen** (Duplikate löschen/verschieben)
- **Zuverlässige MD5-basierte Identifikation**
- **Hash-Caching** für schnellere Wiederholungsscans
- **Speichern/Laden von Duplikat-Listen** für Stapelverarbeitung
- **Modulare Architektur** mit dediziertem fileduplicates-Package

### 🛡️ **Sicherheitsfunktionen**
- **Hochgeschwindigkeits-Datenlöschung** um Wiederherstellung zu verhindern
- **Fülloperationen** mit optimierter Puffer-Verwaltung
- **Stapelverarbeitung** für mehrere Ziele
- **Umfassende Operationshistorie** mit JSON-Protokollierung
- **Kontextabhängige Unterbrechung** - Unterstützung für elegante Abbrüche

---

## 💻 Befehlsreferenz

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
| `speed <größe>` | Leistungstest | `filedo C: speed 500` |
| `fill [größe]` | Mit Testdaten füllen | `filedo D: fill 1000` |
| `clean` | Testdateien löschen | `filedo C: clean` |
| `check-duplicates` | Datei-Duplikate finden | `filedo C: check-duplicates` |
| `cd [modus] [aktion]` | Duplikate prüfen (Kurzform) | `filedo C: cd old del` |
| `from <datei>` | Stapelbefehle ausführen | `filedo from script.txt` |
| `hist` | Operationshistorie anzeigen | `filedo hist` |

### Modifikatoren
| Flag | Zweck | Beispiel |
|------|-------|----------|
| `del` | Automatisches Löschen nach Operation | `filedo E: test del` |
| `nodel` | Testdateien behalten | `filedo C: speed 100 nodel` |
| `short` | Nur kurze Ausgabe | `filedo D: speed 100 short` |
| `max` | Maximale Größe (10GB) | `filedo C: speed max` |
| `old` | Neueste als Original behalten (für cd) | `filedo D: cd old del` |
| `new` | Älteste als Original behalten (für cd) | `filedo E: cd new move F:` |
| `abc` | Alphabetisch letztes behalten (für cd) | `filedo C: cd abc` |
| `xyz` | Alphabetisch erstes behalten (für cd) | `filedo C: cd xyz list dups.lst` |

---

## 🖥️ GUI-Anwendung

**FileDO GUI** (`filedo_win.exe`) - VB.NET Windows Forms Anwendung bietet eine benutzerfreundliche Oberfläche:

- ✅ **Visuelle Zielauswahl** mit Optionsfeldern (Gerät/Ordner/Netzwerk/Datei)
- ✅ **Operationen-Dropdown** (Info, Geschwindigkeit, Füllen, Test, Bereinigen, Duplikat-Prüfung)
- ✅ **Parameter-Eingabe** mit Validierung
- ✅ **Echtzeit-Befehlsvorschau** zeigt äquivalenten CLI-Befehl
- ✅ **Durchsuchen-Button** für einfache Pfad-Auswahl
- ✅ **Fortschrittsverfolgung** mit Echtzeit-Ausgabe
- ✅ **Ein-Klick-Ausführung** mit Ausgabe-Anzeige

```bash
# Starten aus dem filedo_win_vb Ordner
filedo_win.exe          # Windows GUI Interface
```

**Funktionen:**
- Mit VB.NET Windows Forms für native Windows-Erfahrung gebaut
- Automatische Befehlsvalidierung und Parameter-Überprüfung
- Echtzeit-Ausgabe mit Farbcodierung
- Integration mit der Haupt-CLI-Anwendung

---

## 🔍 Erweiterte Funktionen

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

## ⚠️ Wichtige Hinweise

> **🎯 Fake-Kapazität-Erkennung**: Erstellt 100 Dateien (jeweils 1% Kapazität) mit **kontextabhängiger Unterbrechungsunterstützung**. Verwendet moderne zufällige Prüfmuster und optimierte Puffer-Verwaltung für zuverlässige Erkennung.

> **🔥 Verbesserte Unterbrechung**: Alle langen Operationen unterstützen **eleganten Ctrl+C-Abbruch** mit automatischer Bereinigung. Kontextabhängige Unterbrechungsprüfungen an optimalen Punkten für sofortige Reaktionsfähigkeit.

> **🛡️ Sicheres Löschen**: `fill <größe> del` überschreibt freien Speicherplatz mit optimierter Puffer-Verwaltung und kontextabhängigem Schreiben für sichere Datenlöschung.

> **🟢 Testdateien**: Erstellt `FILL_*.tmp` und `speedtest_*.txt` Dateien. Verwenden Sie den `clean` Befehl für automatische Löschung.

> **🔵 Modulare Architektur**: Refaktoriert mit separaten `capacitytest` und `fileduplicates` Packages für bessere Wartbarkeit und Erweiterbarkeit.

---

## 📖 Anwendungsbeispiele

<details>
<summary><b>🔍 USB/SD-Karten Authentizitätsprüfung</b></summary>

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
<summary><b>⚡ Leistungs-Benchmark</b></summary>

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
<summary><b>🛡️ Sicheres Datenlöschen</b></summary>

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
<summary><b>🔍 Duplikat-Suche und Verwaltung</b></summary>

```bash
# Duplikate im aktuellen Verzeichnis finden
filedo . check-duplicates

# Alte Duplikate finden und löschen
filedo C: cd old del

# Neue Duplikate finden und in Backup verschieben
filedo E: cd new move E:\Backup

# Duplikat-Liste für spätere Verarbeitung speichern
filedo D: cd list duplicates.lst

# Gespeicherte Liste mit spezifischer Aktion verarbeiten
filedo cd from list duplicates.lst xyz del
```
</details>

---

## 🏗️ Technische Details

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
├── capacitytest/             # Kapazitätstest-Modul
│   ├── types.go              # Kern-Interfaces und Typen
│   ├── test.go               # Haupt-Testlogik
│   └── utils.go              # Hilfsfunktionen und Prüffunktionen
├── fileduplicates/           # Datei-Duplikat-Verwaltung
│   ├── types.go              # Duplikat-Erkennungs-Interfaces
│   ├── duplicates.go         # Haupt-Duplikat-Logik
│   ├── duplicates_impl.go    # Implementierungsdetails
│   └── worker.go             # Hintergrundverarbeitung
├── filedo_win_vb/           # VB.NET GUI-Anwendung
│   ├── FileDOGUI.sln        # Visual Studio Solution
│   ├── MainForm.vb          # Hauptformular-Logik
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
- **Duplikat-Erkennung**: MD5-basierter Dateivergleich mit Caching
- **Stapelverarbeitung**: Skriptausführung mit Fehlerbehandlung
- **Historienführung**: JSON-basierte Operationsverfolgung

---

## 🔄 Versionshistorie

**v2507112115** (Aktuell)
- **Große Refaktorierung**: Kapazitätstest-Logik in dediziertes `capacitytest` Package extrahiert
- **Verbesserte Unterbrechung**: Kontextabhängige Abbrüche mit thread-sicherem `InterruptHandler` hinzugefügt
- **Verbesserte Leistung**: Optimierte Puffer-Verwaltung und Verifikations-Algorithmen
- **Bessere Architektur**: Modulares Design mit klarer Trennung der Verantwortlichkeiten
- **VB.NET GUI**: Aktualisierte Windows Forms Anwendung mit besserer Integration

**v2507082120** (Vorherige)
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

**FileDO v2507112115** - Erweiterte Datei- und Speicher-Tools

Erstellt von **sza@ukr.net** | [MIT-Lizenz](LICENSE) | [GitHub-Repository](https://github.com/SerZhyAle/FileDO)

---

### 🚀 Neueste Verbesserungen

- **🔧 Modulare Architektur**: Refaktoriert in spezialisierte Packages (`capacitytest`, `fileduplicates`)
- **⚡ Verbesserte Unterbrechung**: Kontextabhängige Abbrüche mit eleganter Bereinigung
- **🛡️ Thread-sichere Operationen**: Verbesserter `InterruptHandler` mit Mutex-Schutz
- **📊 Bessere Leistung**: Optimierte Puffer-Verwaltung und Verifikations-Algorithmen
- **🖥️ Aktualisierte GUI**: VB.NET Windows Forms Anwendung mit verbesserter Integration

</div>
