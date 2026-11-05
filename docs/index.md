---
title: Start
icon: lucide/archive
---
# total-recall

`total-recall` sichert die Sessions deiner Coding-Agenten in einer lokalen SQLite-Datenbank. Zuerst Claude Code, später weitere Harnesses.

![Die TUI: Volltextsuche mit Vorschau](assets/screenshots/search.png)

*Die [TUI](reference/30-tui.md): Volltextsuche mit Vorschau, dazu Statistik, Aktivität je Tag und die Einstellungen*

## Warum

Claude Code löscht alte Transkripte. Der Schalter dafür heißt `cleanupPeriodDays`. Was weg ist, ist weg. Damit verschwindet auch das Wissen darüber, was du wann mit welchem Agenten gemacht hast.

`total-recall` liest die Transkripte laufend ein und hebt sie auf. Danach kannst du sie auswerten:

- Was habe ich letzte Woche in Repo X gemacht?
- Wie viele Sessions hatte ich im Oktober, und an welchen Tagen?
- In welcher Session haben wir über Y entschieden?

## Wie es funktioniert

- **Hooks statt Daemon.** Claude Code ruft `total-recall` über Hooks auf, zum Beispiel am Ende jeder Antwort. Es läuft kein Hintergrunddienst. Warum das so ist: [Einlesen über Hooks](explanation/hooks.md).
- **Inkrementell.** Jede Datei wird ab der Stelle gelesen, an der der letzte Lauf aufgehört hat.
- **Nachlesen.** Beim Start einer Session wird alles nachgelesen, was bisher fehlt. So gehen auch Sessions nicht verloren, bei denen ein Hook ausgefallen ist.
- **Lokal.** Die Datenbank liegt auf deinem Rechner. Geheimnisse werden vor dem Speichern maskiert. Details: [Datenschutz](explanation/datenschutz.md).
- **Auswertbar.** Über die [Befehle](reference/10-befehle.md), die [TUI](reference/30-tui.md) im Terminal, Read-only-SQL und einen eingebauten Skill für Agenten ([Agenten anbinden](how-to/50-agenten-anbinden.md)).

## Was es nicht tut

- Es spielt nichts in laufende Sessions zurück.
- Es gibt keine semantische Suche. Die Volltextsuche nutzt SQLite FTS5.
- Bisher wird nur Claude Code unterstützt.

## Wo anfangen?

Die Doku folgt [Diátaxis](https://diataxis.fr/): vier Quadranten nach der Frage, die du gerade hast. Etwas lernen, eine Aufgabe erledigen, etwas nachschlagen oder verstehen, warum es so ist.

<div class="grid cards" markdown>

<div markdown>
:lucide-graduation-cap:{ .lg .middle } **Tutorials**

Schritt für Schritt von der Installation zur ersten Suche. [Erste Schritte](tutorials/erste-schritte.md) installiert `total-recall`, trägt die Hooks ein, liest alles nach und öffnet die TUI.

[:octicons-arrow-right-24: Tutorials](tutorials/index.md)

</div>

<div markdown>
:lucide-wrench:{ .lg .middle } **How-To-Guides**

Rezepte für konkrete Aufgaben: [Installieren](how-to/10-installieren.md) und aktualisieren, [Hooks einrichten](how-to/20-hooks-einrichten.md), das [Archiv auswerten](how-to/30-auswerten.md), die [Arbeitszeit nachvollziehen](how-to/40-arbeitszeit.md), den [Stundenzettel füllen](how-to/45-stundenzettel.md), [Grafiken für einen Monatsbericht](how-to/47-monatsbericht.md) zeichnen, [Agenten anbinden](how-to/50-agenten-anbinden.md), das [Archiv warten](how-to/60-wartung.md), [Einstellungen in der TUI ändern](how-to/65-einstellungen-tui.md) und [Projekte definieren](how-to/67-projekte-definieren.md).

[:octicons-arrow-right-24: Alle How-To-Guides](how-to/index.md)

</div>

<div markdown>
:lucide-library:{ .lg .middle } **Referenz**

Nachschlagen: alle [Befehle](reference/10-befehle.md) mit Optionen und Ausgaben, die [Konfiguration](reference/20-konfiguration.md), die [TUI](reference/30-tui.md) mit Ansichten, Tasten und Grafikmodi, das [Schema](reference/40-schema.md) für eigene SQL-Abfragen, die [Release-Assets](reference/50-release.md) und die [Änderungen](reference/90-aenderungen.md) je Version.

[:octicons-arrow-right-24: Referenz](reference/index.md)

</div>

<div markdown>
:lucide-lightbulb:{ .lg .middle } **Erklärungen**

Hintergrund und Entscheidungen: die Spezifikation [Design](explanation/design.md), der [Datenschutz](explanation/datenschutz.md) mit Maskierung und Ablageorten, das [Einlesen über Hooks](explanation/hooks.md) und die Historie des [TUI-Spikes](explanation/spike-tui.md).

[:octicons-arrow-right-24: Erklärungen](explanation/index.md)

</div>

</div>

## Stand

!!! note "Version 1.2"
    Alle Befehle auf der Seite [Befehle](reference/10-befehle.md) sind umgesetzt, dazu die Terminal-Oberfläche [`total-recall tui`](reference/30-tui.md) mit vier Ansichten. Unterstützt wird bisher nur Claude Code. Der Quellcode steht unter MIT-Lizenz auf [GitHub](https://github.com/seletz/total-recall); Installation über das Release-Archiv, Homebrew oder den Quellcode: [Installieren](how-to/10-installieren.md). Ein Update auf eine Version mit neuem Datenbankschema migriert nichts von selbst; die Schritte stehen unter [Aktualisieren](how-to/10-installieren.md#aktualisieren). Was sich je Version geändert hat, steht unter [Änderungen](reference/90-aenderungen.md).

Die verbindliche Spezifikation steht in [Design (Spezifikation)](explanation/design.md).

## Diese Dokumentation

Diese Dokumentation wird unter <https://seletz.github.io/total-recall/> veröffentlicht. Die Quellen liegen im Repository unter `docs/`, je Quadrant ein Ordner. Lokal ansehen:

```sh
mise run docs:serve
```

Der Build ist strict, tote interne Links brechen ihn: `mise run docs:build`.
