---
title: TUI
icon: lucide/keyboard
---
# TUI

`total-recall tui` öffnet das Archiv in einer Vollbild-Oberfläche im Terminal: Volltextsuche mit Vorschau beim Tippen, eine Detailansicht je Session, eine Statistik der Aktivität als Chart, Tabelle und Heatmap und die erste und letzte Aktivität je Tag. Die Datenbank wird nur lesend geöffnet.

!!! note "Herkunft"
    Die TUI ist das Ergebnis eines Spikes und seit v0.2.0 fester Bestandteil von `total-recall`. Entwurf, Abweichungen, Messwerte und die Entscheidung zur Übernahme stehen in [Spike: TUI](../explanation/spike-tui.md). Die Oberfläche ist Englisch wie die übrige CLI-Ausgabe. Die Textbeispiele auf dieser Seite sind erfunden, ebenso die Bilder: Sie zeigen das synthetische Demo-Archiv (`mise run demo:db`), eine erfundene Entwicklerin mit erfundenen Projekten und Sessions von Mai bis Oktober 2026. Erzeugt werden sie mit `mise run docs:screenshots`; wer die TUI ändert, erzeugt sie neu, und `mise run docs:screenshots:check` prüft, ob sie noch stimmen, siehe [Design](../explanation/design.md#technik).

## Aufruf

```sh
total-recall tui                          # startet in der Suche
total-recall tui stats                    # startet in der Statistik
total-recall tui activity                 # startet in der Aktivität je Tag
total-recall tui --query "goreleaser"     # Suche mit vorbelegtem Suchtext
total-recall tui stats --graphics text    # Chart als Text, kein Bild
```

| Argument / Option | Bedeutung |
|---|---|
| `search` \| `stats` \| `activity` | Ansicht beim Start, Default `view` aus `[tui]`, sonst `search` |
| `--query TEXT` | Suchtext vorbelegen; die Suche läuft sofort beim Start |
| `--graphics auto\|text\|kitty` | Chart-Darstellung in der Statistik, Default `graphics` aus `[tui]`, sonst `auto`; siehe [Grafikmodi](#grafikmodi) |
| `--no-mouse` | Maus dem Terminal überlassen: kein Mausrad, keine Klicks, dafür Textauswahl durch einfaches Ziehen; siehe [Maus](#maus). Default `mouse` aus `[tui]`; `--no-mouse=false` schaltet die Maus für einen Aufruf wieder ein |

Die [globalen Optionen](10-befehle.md#globale-optionen) `--db`, `--tz` und `--config` gelten wie überall; `--tz` bzw. `timezone` aus der Config bestimmt Tages-, Wochen- und Monatsgrenzen, `day_start` die Tagesgrenze der Aktivitätsansicht. Der Abschnitt [`[tui]`](20-konfiguration.md#tui) der Konfiguration setzt die Defaults von Ansicht, Grafikmodus und Maus sowie Periode und Dimension der Statistik und das Fenster der Aktivität; Flags und Argument gehen vor. Beenden mit `q` oder ++ctrl+c++ (++ctrl+c++ immer, `q` nur, solange kein Eingabefeld den Fokus hat).

Alle Tasten auf dieser Seite sind die Standardbelegung. Sie ist auf ein deutsches Mac-Layout ausgelegt und lässt sich über den Abschnitt `[keys]` der Konfiguration ändern, siehe [Tastenbelegung anpassen](#tastenbelegung-anpassen).

Die Oberfläche braucht mindestens 80×24 Zeichen; die Bilder auf dieser Seite zeigen 120×36 Zeichen, die kompakten Layouts 80×24. Bei weniger Platz werden Fußzeile und Tabellen abgeschnitten (`…`), die Heatmap wechselt in ein kompaktes Layout und blendet sich darunter aus.

![Aktivität bei 80×24: Kopf- und Fußzeile werden gekürzt, die Zeitachse wird schmaler](../assets/screenshots/compact.png)

*Aktivität bei 80×24: Kopf- und Fußzeile werden gekürzt, die Zeitachse wird schmaler*


## Aufbau

Alle Ansichten teilen denselben Rahmen:

- **Kopfzeile:** links der Programmname in einem Farbverlauf, rechts die Ansichten als Tabs. Die aktive steht in eckigen Klammern auf der Akzentfarbe (`[1 search]  2 stats  3 activity`), damit sie auch ohne Farbe erkennbar ist. Ist eine Session geöffnet, hängt ihr Name an der Suche: `[1 search] › session`.
- **Fußzeile:** die wichtigsten Tasten der aktuellen Ansicht, dann die globalen (`1 search • 2 stats • 3 activity • ? help • q quit`), die Tasten in der Akzentfarbe, die Beschreibungen gedämpft. Hat ein Eingabefeld den Fokus, steht dort nur noch `ctrl+c quit`, weil alle anderen Tasten in das Feld gehen.
- **Hilfe:** `?` legt ein Overlay mit allen Tasten der aktuellen Ansicht und den globalen Tasten über den Inhalt, in einem abgerundeten Rahmen in der Akzentfarbe. ++esc++ oder `?` schließt es. Das Overlay wird aus derselben Tastenbelegung erzeugt wie die Fußzeile.
- **Farben:** alle Ansichten teilen ein Theme, siehe [Farben](#farben).
- **Maus:** ein Klick auf einen Tab in der Kopfzeile wechselt die Ansicht, das Mausrad blättert im Inhalt; siehe [Maus](#maus).

![Hilfe-Overlay in der Statistik: die Tasten der Ansicht, dann die globalen Tasten](../assets/screenshots/help.png)

*Hilfe-Overlay (`?`) in der Statistik: die Tasten der Ansicht, dann die globalen Tasten*

## Farben

Alle Ansichten zeichnen mit demselben Theme. Die Farbtöne sind die Reihenfarben der Statistik-Charts; das Theme weist ihnen Rollen zu:

| Rolle | Farbe | Wo |
|---|---|---|
| Akzent | Blau | Programmname (Verlauf von Blau über Türkis nach Violett), aktiver Tab und aktive Umschalter (weiß auf Blau), Trennlinien, Rahmen der Hilfe, Tasten der Fußzeile, Überschriften, Spaltenköpfe, Markierungen `▸` und `◄` |
| Mensch | Orange | Actor `human` in Trefferliste, Vorschau und Session, Zähler im Session-Kopf, Kachel `human` |
| Agent | Blau | Actor `agent`, Zähler, Kachel `agent` |
| Peer | Violett | Actor `peer` |
| Tool | Türkis | Trennzeilen von Tool-Aufrufen und -Ergebnissen; ihr Text ist gedämpft |
| Treffer | Gelb | Suchwörter in Textausschnitt, Vorschau und Session (fett und unterstrichen); der gewählte Treffer ist invertiert |
| Auswahl | Hinterlegung | der markierte Treffer, solange die Liste den Fokus hat |
| Bewertung | Türkis, Grün, Orange | Spanne und Saldo der Aktivität unter, im und über dem Rahmen |
| Wochenende, Feiertag | Violett, Magenta | Zeilen und Namen in Aktivität und Statistik |
| Fehler | Rot | Fehlermeldungen in den Statuszeilen |

Das Theme folgt dem Terminalhintergrund: beim Start fragt die TUI das Terminal nach seiner Hintergrundfarbe; auf hellem Hintergrund werden die Farben abgedunkelt, die Heatmap läuft dort von Hellblau nach Blau, auf dunklem von Dunkelblau nach Hellblau. Bis die Antwort kommt, gilt das dunkle Theme. Mit `NO_COLOR` oder in einem Terminal ohne Farben bleibt alles lesbar: jede farbige Stelle unterscheidet sich auch in Zeichen, Schriftschnitt oder Position (Klammern um den aktiven Tab, `▸` und `◄`, Unterstreichung der Treffer, Schattierungen in Heatmap und Aktivitätsbalken).

## Maus

Die TUI fordert Mausmeldungen vom Terminal an (Cell Motion: Klicks und Rad, keine Bewegung). Damit gilt überall:

| Maus | Wirkung |
|---|---|
| Rad | blättert, was unter dem Zeiger liegt: die Trefferliste (bewegt die Markierung wie ++up++/++down++), die Vorschau, den Verlauf einer Session, die Tage der Aktivität |
| Klick auf einen Tab in der Kopfzeile | wechselt die Ansicht wie `1`, `2`, `3`; schließt eine offene Session |
| Klick auf einen Treffer | markiert ihn und gibt der Liste den Fokus; ein zweiter Klick auf den markierten Treffer öffnet die Session wie ++enter++ |
| Klick auf die Eingabezeile der Suche | gibt der Eingabe den Fokus |

Klicks anderswo (Vorschau, Statistik, Aktivität, Fußzeile, Hilfe) tun nichts. Das Mausrad in der Vorschau verschiebt den Austausch zeilenweise; mit dem nächsten Treffer springt die Vorschau wieder an die automatische Position mit dem Treffer im oberen Drittel.

**Textauswahl:** Solange die Maus an das Programm geht, wählt einfaches Ziehen keinen Text mehr aus. Die Terminals erlauben die Auswahl trotzdem mit einer Zusatztaste: ++shift++ und Ziehen in Ghostty und kitty, ++option++ und Ziehen in Terminal.app und iTerm2. In herdr und tmux kommen die Mausmeldungen beim Programm an, die Auswahl mit Zusatztaste geht an das äußere Terminal. Wer ganz ohne Maus arbeiten will, startet mit `--no-mouse` oder setzt `mouse = false` im Abschnitt [`[tui]`](20-konfiguration.md#tui) der Konfiguration; dann bleibt die Maus beim Terminal und die Bedienung ist rein über die Tastatur. `--no-mouse=false` schaltet sie für einen Aufruf wieder ein.

## Suche

![Suche mit Vorschau: Trefferliste links, der Austausch um den markierten Treffer rechts](../assets/screenshots/search.png)

*Suche mit Vorschau: Trefferliste links, der Austausch um den markierten Treffer rechts*

- **Eingabe** oben, mit dem Fokus beim Start. Jede Änderung startet nach 150 ms Ruhe eine Suche wie `search`: ohne Rohsyntax wird jedes Wort in Anführungszeichen gesetzt, alle Wörter müssen vorkommen. Höchstens 200 Treffer werden geholt; die Statuszeile sagt `200 hits, limit reached`, wenn es mehr gäbe. Rechts in der Eingabezeile stehen die aktiven Filter.
- **Trefferliste** links, zwei Zeilen je Treffer: Zeit (gedämpft), Session-Name und Actor (in seiner Farbe, siehe [Farben](#farben)), darunter der Textausschnitt mit den Suchwörtern gelb, fett und unterstrichen. Sortierung wie `search`: Rang, dann Zeit. Der markierte Treffer trägt `▸`; hat die Liste den Fokus, ist seine Kopfzeile zusätzlich hinterlegt.
- **Vorschau** rechts, folgt der Markierung (nach 100 ms Ruhe): der Session-Kopf (Name in der Akzentfarbe, ID und Repo gedämpft, Zeitraum und Branch), dann der Austausch um den Treffer, also die menschliche Nachricht davor und die Antwort des Agenten danach. Die Trennzeilen nennen Zeit und Actor in seiner Farbe, der Treffer-Event ist mit `◄` markiert, die Suchwörter sind hervorgehoben wie in der Liste. Trifft die Suche einen Tool-Aufruf, lädt die Vorschau den vollen Verlauf inklusive Tools.
- **Fokus:** ++tab++ wechselt zwischen Eingabe und Liste, siehe [Fokus](#fokus). In der Liste bewegen ++up++/++down++ und `k`/`j`, ++enter++ öffnet die markierte Session (auch direkt aus der Eingabe). Mit der [Maus](#maus): das Rad über der Liste bewegt die Markierung, ein Klick markiert den Treffer, ein zweiter Klick öffnet ihn.
- **Statuszeile:** Trefferzahl und Dauer der Abfrage (`5 hits (12 ms)`), sonst `type to search`, `searching…` mit einem drehenden Spinner davor, solange die Abfrage läuft, `no hits` oder die Fehlermeldung.

### Fokus

Innerhalb der Suche können zwei Elemente den Fokus haben: die **Eingabe** und die **Trefferliste**. Die Vorschau ist reine Anzeige und folgt der Markierung. Der Repo-Filter ist ein modales Feld, das `r` aus der Liste öffnet; er gehört nicht zur Reihenfolge.

- ++tab++ und ++shift+tab++ wechseln zwischen Eingabe und Liste, in beide Richtungen, und bleiben dabei in der Suche. Die Ansichten wechseln nur `1`, `2` und `3`; ++tab++ ist dafür nicht belegt.
- ++esc++ geht eine Ebene zurück: aus dem Repo-Filter in die Liste (die Änderung wird verworfen), aus der Liste in die Eingabe. In der Eingabe tut ++esc++ nichts.
- Hat die **Eingabe** den Fokus, sind alle Tasten Suchtext. Ausnahmen sind nur ++tab++/++shift+tab++ (Fokus), ++enter++ (markierten Treffer öffnen), ++ctrl+r++ (Rohsyntax) und ++ctrl+c++ (beenden). `a`, `t`, `r`, `q`, `?`, `1`, `2` und `3` landen im Suchtext und lösen nichts aus.
- Hat die **Liste** den Fokus, gelten ihre Tasten (bewegen, öffnen, Filter) und die globalen Tasten der App: `1`, `2`, `3`, `?`, `q`.
- Im **Repo-Filter** übernehmen ++enter++ und ++tab++ den Wert, ++esc++ verwirft ihn; alles andere ist Text.
- Die **Maus** folgt derselben Ordnung: ein Klick auf einen Treffer gibt der Liste den Fokus, ein Klick auf die Eingabezeile der Eingabe; das Rad ändert den Fokus nicht. Solange der Repo-Filter offen ist, tut die Maus nichts.
- Sichtbar ist der Fokus am Cursor und an der Markierung: in der Eingabe und im Repo-Filter steht der Cursor des Terminals im Feld; hat die Liste den Fokus, ist der markierte Treffer hinterlegt. Das `▸` vor dem markierten Treffer bleibt in jedem Fall, weil ++enter++ ihn aus beiden Elementen öffnet und die Vorschau ihn zeigt. Die Fußzeile nennt die Tasten des fokussierten Elements.

Der Fokus bleibt beim Wechsel der Ansicht und beim Öffnen einer Session erhalten; ++esc++ aus der Session führt dorthin zurück, wo man war.

### Filter

Die Filter werden in der **Liste** umgeschaltet (in der Eingabe wären die Buchstaben Suchtext). Jede Änderung sucht sofort neu.

| Taste | Filter | Werte |
|---|---|---|
| `a` | Actor | `all` → `human` → `agent` → `peer` → `all` |
| `t` | Zeitraum (Zeit des Events) | `all` → `7d` → `30d` → `90d` → `all` |
| `r` | Repo | Teilstring von Remote-URL, Git-Root oder Arbeitsverzeichnis. Öffnet ein Eingabefeld in der Statuszeile: ++enter++ oder ++tab++ übernimmt, ++esc++ verwirft. Leer löscht den Filter |
| ++ctrl+r++ | Rohsyntax | FTS5-Syntax unverändert durchreichen (`AND`, `OR`, `NOT`, `"Phrase"`, `präfix*`), wie `search --raw`. Geht auch aus der Eingabe |

Die Hervorhebung in Vorschau und Detailansicht nimmt die Wörter der Eingabe; in Rohsyntax ohne Operatoren, Anführungszeichen und Sterne.

## Session

++enter++ auf einem Treffer öffnet die Session als Ebene über der Suche. Die Suche bleibt darunter erhalten: ++esc++ führt mit Eingabe, Treffern, Markierung und Fokus zurück. `1`, `2` und `3` schließen die Detailansicht ebenfalls.

![Session-Detail mit Treffer, Tool-Aufrufe eingeblendet (`T`)](../assets/screenshots/session.png)

*Session-Detail mit Treffer, Tool-Aufrufe eingeblendet (`T`)*

- **Kopf:** Name (Akzentfarbe) und volle Session-ID (gedämpft), darunter Repo, Branch, Zeitraum mit Dauer, Anzahl menschlicher und Agenten-Events in den Farben der Actors, bei Subagent-Sessions die Eltern-ID. In schmalen Fenstern gewinnt der Name, die ID wird gekürzt.
- **Trennlinie mit Status:** der oberste sichtbare Event (`3/17 events`), der gewählte Treffer (`hit 1/4`), die Position im Verlauf, wenn er länger als das Fenster ist (`top`, `42%`, `bot`), `tools`, wenn Tool-Events eingeblendet sind, und kurz `copied` nach dem Kopieren.
- **Verlauf** wie `show`: eine Trennzeile je Event mit Zeit (gedämpft) und Actor in seiner Farbe, Tool-Aufrufe mit dem Werkzeugnamen in Türkis und gedämpftem Text, damit sie hinter dem Gespräch zurücktreten; Text am Wortende umbrochen. Die Suchwörter sind gelb, fett und unterstrichen, der gewählte Treffer ist invertiert, der markierte Event trägt `◄`. Beim Öffnen steht der Treffer-Event oben; ist er ein Tool-Event, werden Tools automatisch eingeblendet.
- **Blättern** mit ++up++/++down++, ++pgup++/++pgdn++ oder dem Mausrad (drei Zeilen je Schritt).
- `n`/`N` springen zum nächsten/vorigen Treffer, am Ende wieder von vorn. `T` blendet Tool-Aufrufe, Ergebnisse und Automatik ein und aus (wie `show --all`); die Position bleibt am markierten Event. `y` kopiert die Session-ID über OSC 52 in die Zwischenablage, für `show`, `sql` oder Agenten.

## Statistik

![Statistik: gestapeltes Chart je Tag und Projekt, Kacheln mit Kennzahlen, Tabelle](../assets/screenshots/stats.png)

*Statistik: gestapeltes Chart je Tag und Projekt, Kacheln mit Kennzahlen, Tabelle*

- **Umschalter** oben: Periode (`d` Tage, `w` Wochen, `m` Monate) und Dimension (`p` Projekt, `a` Agent, `v` Verzeichnis), der aktive Wert in Klammern. Rechts der gezeigte Zeitraum.
- **Zeitfenster:** Standard sind die letzten 28 Tage, 16 Wochen oder 12 Monate bis heute. ++left++ und ++right++ (ebenso `,` und `.` oder `[` und `]`) verschieben um ein ganzes Fenster, `-` und `+` halbieren bzw. verdoppeln die Anzahl Perioden (mindestens eine, höchstens das Achtfache des Standardfensters). Ein Periodenwechsel springt auf das Standardfenster, das die zuletzt gezeigte Periode enthält. Periode und Dimension beim Start: `stats_period` und `stats_dimension` in [`[tui]`](20-konfiguration.md#tui).
- **Kacheln** unter den Umschaltern: die Events des Fensters (davon menschliche und Agenten-Events in den Farben der Actors), die eindeutigen Sessions und die Perioden mit Aktivität, jede Zahl fett hinter einem Akzentbalken. Die Zeile weicht, wenn das Chart sonst unter seine Mindesthöhe von acht Zeilen fiele, etwa mit Heatmap bei 80×24.
- **Kennzahl:** `e` wechselt zwischen `events` (menschliche plus Agenten-Events), `human` und `sessions`. Der Titel über dem Chart nennt sie.
- **Chart:** eine gestapelte Säule je Periode, Segmente sind die sechs größten Werte der Dimension über das ganze Fenster plus `other`. Die Legende steht darunter; die Farben bleiben beim Umschalten stabil. Farben unterscheiden die Segmente. Perioden ohne Aktivität bleiben leer, die Achse ist lückenlos. Bei Periode Tag sind die Achsenbeschriftungen von Feiertagen (aus `bundesland` in [`[work]`](20-konfiguration.md#work)) magenta und die des Wochenendes blass, nur im Text-Chart.
- **Tabelle** (Spaltenköpfe in der Akzentfarbe): dieselben Reihen mit zwei Session-Zahlen, menschlichen und Agenten-Events und Anteil an allen Events. `sessions` zählt die eindeutigen Sessions im ganzen Zeitfenster, jede einmal. `session-days`, `session-weeks` bzw. `session-months` (je nach Periode) summiert die Sessions je Periode, so wie das Chart sie zeigt: eine Session, die an drei Tagen aktiv war, zählt bei Tagen dreimal. Die Differenz der beiden Spalten zeigt, wie viele Sessions über Periodengrenzen liefen.
- **Statuszeile:** Grafikmodus, Anzahl Perioden, Dauer der Abfrage, Zustand der Heatmap. Ergebnisse werden je Kombination gecacht; Zurückschalten ist sofort.

Die Dimensionen:

| Dimension | Schlüssel | Anzeige |
|---|---|---|
| `project` | Git-Root der Session, sonst ihr Arbeitsverzeichnis; Subagent-Sessions zählen zur Eltern-Session, Worktrees zum Hauptrepo | Basisname des Pfads. Gleiche Basisnamen bekommen das Elternverzeichnis dazu (`a/tools`, `b/tools`) |
| `agent` | `claude main` (Session ohne Eltern) oder `claude subagent` | unverändert |
| `directory` | Arbeitsverzeichnis der Session, bei Subagents das der Eltern-Session | voller Pfad |

### Heatmap

`h` blendet unter dem Chart die Heatmap Wochentag × Stunde für das Zeitfenster ein (Daten wie `stats`, Kennzahl wie das Chart). Je voller die Zelle (`░▒▓█`), desto näher am Maximum, das der Titel nennt; die Farbe läuft dazu von blass nach kräftig, auf dunklem Hintergrund von Dunkel- nach Hellblau, auf hellem umgekehrt. Leere Zellen bleiben frei.

![Heatmap Wochentag × Stunde unter dem Chart](../assets/screenshots/heatmap.png)

*Heatmap Wochentag × Stunde unter dem Chart*

Die Heatmap braucht zehn Zeilen. Bleibt dem Chart daneben weniger als seine Mindesthöhe von acht Zeilen, wechselt sie in ein **kompaktes Layout** mit neun Zeilen: die Stundenbeschriftung rückt in die Achsenzeile, die Legende der Schattierungen von leer bis Maximum (`░▒▓█`) steht im Titel. Bei 80×24 passen so Chart und Heatmap untereinander; die Tabelle bleibt dafür ausgeblendet. In beiden Layouts werden die Zellen schmaler, bis jede Stunde eine Spalte hat, dann steht nur noch jede zweite Stunde dran.

![Statistik mit Heatmap bei 80×24: kompaktes Layout, Stunden in der Achsenzeile, Legende im Titel, Tabelle ausgeblendet](../assets/screenshots/heatmap-compact.png)

*Statistik mit Heatmap bei 80×24: kompaktes Layout, Stunden in der Achsenzeile, Legende im Titel, Tabelle ausgeblendet*

Reichen auch neun Zeilen nicht (Terminal unter 24 Zeilen), bleibt die Heatmap ausgeblendet und die Statuszeile sagt `heatmap needs more rows`.

## Aktivität

Die dritte Ansicht (`3`, `total-recall tui activity`) zeigt je Tag die **erste und letzte Aktivität**: ein Notnagel für vergessenes Ein- und Ausstempeln. Dieselben Daten liefert [`stats --workday`](10-befehle.md#stats-workday) als Text und JSON.

![Eine Woche: Kernzeit 09–17 als Band, Balken je Tag, Feiertag, Wochenarbeitszeit und Kennzahlen](../assets/screenshots/activity-week.png)

*Eine Woche: Kernzeit 09–17 als Band, Balken je Tag, Feiertag, Wochenarbeitszeit und Kennzahlen*

![Ein Monat (`m`): Monatsarbeitszeit gegen das Soll der Arbeitstage, Feiertag ohne Soll](../assets/screenshots/activity-month.png)

*Ein Monat (`m`): Monatsarbeitszeit gegen das Soll der Arbeitstage, Feiertag ohne Soll*

- **Umschalter** oben: Zeitfenster (`d` ein Tag, `w` eine ISO-Woche ab Montag, `m` ein Monat) und was zählt (`human`, der Standard: nur eigene Nachrichten, denn Agent-Events laufen nachts weiter und dehnen die Spanne; `h` schaltet auf `all`, eigene Nachrichten und Agent-Events wie in der Statistik; den Standard setzt `count` in [`[work]`](20-konfiguration.md#work)). Rechts das Fenster: `this week`/`last week` (bzw. `today`/`yesterday`, `this month`/`last month`), Kalenderwoche oder Monat und der erste und letzte Tag; ein Feiertag steht bei einem einzelnen Tag mit Namen dahinter.
- **Kopfzeile** bei Woche und Monat: die Arbeitszeit der Periode (Summe der Spannen, fett und gefärbt: türkis unter, grün im Rahmen, orange über dem Wochenrahmen `weekly_low`–`weekly_high`, bei einer Woche mit fünf Arbeitstagen 32–48 h, sonst anteilig), die Tage mit Aktivität, das Soll (Tagessoll × Arbeitstage der Periode) mit dem Saldo dagegen und die Feiertage der Periode mit Namen. Arbeitstage sind Montag bis Freitag ohne Feiertage, mit oder ohne Aktivität, aber nur bis heute: in der laufenden Woche oder dem laufenden Monat zählen künftige Tage noch nicht. Ein vergessener Tag oder Urlaub drückt den Saldo also, ein Feiertag nicht.
- **Zeitfenster:** Start ist die aktuelle Woche (`activity_window` in [`[tui]`](20-konfiguration.md#tui)). ++left++ und ++right++ (ebenso `,` und `.` oder `[` und `]`) verschieben um einen Tag, eine Woche oder einen Monat. `c` springt in die aktuelle Periode (heute, diese Woche, dieser Monat), `l` in die vorige (gestern, letzte Woche, letzter Monat). Ein Periodenwechsel behält den zuletzt gezeigten Tag bei: aus der letzten Woche führt `m` in den Monat, der ihren letzten Tag enthält.
- **Zeilen:** je Tag Wochentag und Datum, erste und letzte Aktivität als Uhrzeit, die Spanne dazwischen, die Differenz zum Tagessoll und ein Balken auf einer Zeitachse. Tage ohne Aktivität stehen mit `-` und leerem Balken da, damit Lücken auffallen.
- **Normalarbeitszeit:** Kernzeit und Tagessoll kommen aus `[work]` in der [Konfiguration](20-konfiguration.md#work), Standard 09:00–17:00 und 8 h. Die Kernzeit liegt als Band mit Linien an Anfang und Ende auf der Achse jedes Werktags (`│───│`, farbig hinterlegt). Der Balken ist innerhalb der Kernzeit blau (`█`), davor und danach orange (`▓`), nach Mitternacht rot (`▒`); am Wochenende violett ohne Band. Ohne Farben (`NO_COLOR`) unterscheiden die Zeichen.
- **Spanne und Differenz:** Die Spanne eines Arbeitstags wird gegen `daily_low`/`daily_high` bewertet und mit der Differenz zum Soll (`+1h15m`, `-2h35m`) gefärbt: türkis unter, grün im Rahmen, orange deutlich über Soll. Wochenende und Feiertage haben kein Soll, ihre ganze Spanne ist die Differenz; ein Tag ohne Aktivität hat keine (`-`), zählt bei Woche und Monat aber als Arbeitstag mit (siehe Kopfzeile).
- **Feiertage** kommen aus `bundesland` in [`[work]`](20-konfiguration.md#work) (Standard Baden-Württemberg, unbekannt oder leer: nur die bundesweiten). Ein Feiertag ist magenta: Datum, Spanne und Balken; die Zeitachse hat kein Band, und der Name steht auf ihr, links vorn oder hinter dem Balken, wo Platz ist. Ein Feiertag am Wochenende zählt als Feiertag. Die Statuszeile zählt die Feiertage des Fensters.
- **Tagesgrenze:** Ein Tag beginnt um `day_start` aus der [Konfiguration](20-konfiguration.md#day_start), Standard 04:00 Ortszeit. Aktivität davor zählt zum Vortag und wird als `01:30 (+1)` angezeigt; die Zeitachse läuft entsprechend von 04 bis 04 Uhr. Die Spanne ist die tatsächlich verstrichene Zeit, auch über eine Zeitumstellung hinweg.
- **Kennzahlen** unter der Linie bei Woche und Monat: Median, Mittelwert, Minimum und Maximum von erster Aktivität, letzter Aktivität (als Uhrzeit, nach Mitternacht mit `(+1)`) und Spanne über die Tage mit Aktivität; die Spannen sind nach der Tagesbewertung gefärbt. `s` blendet den Block aus und ein; er weicht von selbst, wenn sonst weniger als drei Tageszeilen blieben.
- **Farben** kommen aus dem gemeinsamen [Theme](#farben) und folgen damit dem Terminalhintergrund; Magenta ist den Feiertagen vorbehalten.
- **Statuszeile:** Anzahl Tage, Dauer der Abfrage, Zählweise (`human only` oder `all events`) und Feiertage. Passt ein Monat nicht in die Höhe, zeigt sie die sichtbaren Zeilen (`rows 1–21 of 31`); ++up++/++down++ bzw. `k`/`j` und das Mausrad blättern. Ergebnisse werden je Fenster und Zählweise gecacht.

Der Balken ist immer Text (Blockzeichen), unabhängig vom [Grafikmodus](#grafikmodi); bei weniger als etwa 65 Spalten entfällt er (und mit ihm der Feiertagsname auf der Achse), die Uhrzeiten, Spanne und Differenz bleiben. Die Kopfzeile wird bei schmalen Fenstern rechts abgeschnitten (`…`); die Feiertage stehen dann noch in den Zeilen.

## Tasten

Die Standardbelegung ist für ein deutsches Mac-Layout gewählt: Keine Aktion hängt an einer Taste, die dort nur mit ++option++ erreichbar ist (`[ ] { } | \ ~ @`). Wo `[` und `]` naheliegen, sind sie Zweitbelegung hinter ++left++/++right++ und `,`/`.`. Die Fußzeile und die Hilfe (`?`) zeigen immer die tatsächlich wirksamen Tasten. Jede Aktion lässt sich umbelegen, siehe [Tastenbelegung anpassen](#tastenbelegung-anpassen); die Aktionsnamen stehen in den Tabellen.

### Überall

| Taste | Aktion | Wirkung |
|---|---|---|
| `1` / `2` / `3` | `global_search` / `global_stats` / `global_activity` | Suche / Statistik / Aktivität; schließt eine offene Session |
| ++tab++ / ++shift+tab++ | `search_focus` | Fokus innerhalb der Ansicht wechseln: in der Suche Eingabe ↔ Liste. Wechselt nie die Ansicht |
| `?` | `global_help` | Hilfe ein/aus |
| ++esc++ | `global_back` | eine Ebene zurück: Session → Suche, Hilfe schließen, Liste → Eingabe, Repo-Filter abbrechen |
| `q` | `global_quit` | beenden (nicht, solange Eingabe oder Repo-Filter den Fokus haben; eine Belegung mit ++ctrl++ beendet auch dort) |
| ++ctrl+c++ | – | beenden, immer; nicht umbelegbar |
| Maus | – | Rad blättert, Klick auf einen Tab wechselt die Ansicht; siehe [Maus](#maus) |

### Suche

| Taste | Aktion | Eingabe hat den Fokus | Liste hat den Fokus |
|---|---|---|---|
| ++tab++ / ++shift+tab++ | `search_focus` | in die Liste | in die Eingabe |
| ++esc++ | `global_back` | – | in die Eingabe |
| ++enter++ | `search_open` | markierten Treffer öffnen | markierten Treffer öffnen |
| ++up++ / `k`, ++down++ / `j` | `search_up`, `search_down` | – | Markierung bewegen |
| `a`, `t`, `r` | `search_actor`, `search_time`, `search_repo` | Suchtext | Filter Actor, Zeitraum, Repo |
| ++ctrl+r++ | `search_raw` | Rohsyntax ein/aus | Rohsyntax ein/aus |

Im Repo-Filter: ++enter++ (`search_open`) oder ++tab++ (`search_focus`) übernimmt, ++esc++ (`global_back`) verwirft; siehe [Fokus](#fokus).

### Session

| Taste | Aktion | Wirkung |
|---|---|---|
| ++up++ / `k`, ++down++ / `j` | `session_up`, `session_down` | eine Zeile |
| `b` / ++pgup++, `f` / ++pgdn++ / ++space++ | `session_page_up`, `session_page_down` | eine Seite |
| `u` / ++ctrl+u++, `d` / ++ctrl+d++ | `session_half_up`, `session_half_down` | eine halbe Seite |
| `g` / ++home++, `G` / ++end++ | `session_top`, `session_bottom` | Anfang / Ende |
| `n` / `N` | `session_next`, `session_prev` | nächster / voriger Treffer |
| `T` | `session_tools` | Tool-Events ein/aus |
| `y` | `session_copy` | Session-ID kopieren |
| ++esc++ | `global_back` | zurück zur Suche |

### Statistik

| Taste | Aktion | Wirkung |
|---|---|---|
| `d` / `w` / `m` | `stats_days`, `stats_weeks`, `stats_months` | Periode Tag / Woche / Monat |
| `p` / `a` / `v` | `stats_project`, `stats_agent`, `stats_directory` | Dimension Projekt / Agent / Verzeichnis |
| ++left++ / ++right++, `,` / `.`, `[` / `]` | `stats_earlier`, `stats_later` | ein Fenster zurück / vor |
| `-` / `+` | `stats_zoom_out`, `stats_zoom_in` | Fenster halbieren / verdoppeln (`=` wie `+`) |
| `e` | `stats_metric` | Kennzahl: events → human → sessions |
| `h` | `stats_heatmap` | Heatmap ein/aus |
| `g` | `stats_graphics` | Grafikmodus: auto → text → kitty → auto |

### Aktivität

| Taste | Aktion | Wirkung |
|---|---|---|
| `d` / `w` / `m` | `activity_day`, `activity_week`, `activity_month` | Fenster ein Tag / eine Woche / ein Monat |
| ++left++ / ++right++, `,` / `.`, `[` / `]` | `activity_earlier`, `activity_later` | eine Periode zurück / vor |
| `c` | `activity_current` | heute / diese Woche / dieser Monat |
| `l` | `activity_last` | gestern / letzte Woche / letzter Monat |
| `h` | `activity_human` | Zählweise: nur eigene Nachrichten ↔ alle Events |
| `s` | `activity_figures` | Kennzahlen (Median, Mittelwert, Min/Max) ein/aus |
| ++up++ / `k`, ++down++ / `j` | `activity_up`, `activity_down` | blättern, wenn nicht alle Tage passen |

### Tastenbelegung anpassen { #tastenbelegung-anpassen }

Im Abschnitt `[keys]` der [Konfiguration](20-konfiguration.md#keys) steht je Aktion eine Taste oder eine Liste von Tasten. Nur genannte Aktionen ändern sich, alle anderen behalten die Standardbelegung aus den Tabellen oben. Die Aktionsnamen tragen die Ansicht als Präfix (`global_`, `search_`, `session_`, `stats_`, `activity_`).

```toml
[keys]
global_quit = ["q", "ctrl+q"]      # Liste: jede Taste löst aus
stats_earlier = "<"                # oder eine einzelne Taste
stats_later = ">"
activity_earlier = "<"
activity_later = ">"
```

**Tastennamen.** Ein einzelnes Zeichen (`a`, `G`, `?`, `,`, `ü`) oder ein Name: `up`, `down`, `left`, `right`, `enter`, `tab`, `esc`, `space`, `backspace`, `delete`, `insert`, `home`, `end`, `pgup`, `pgdown`, `f1` bis `f20`. Davor `ctrl+`, `alt+` oder `shift+` (`ctrl+r`, `shift+tab`, `ctrl+shift+up`). Groß- und Kleinschreibung ist bei Namen egal; ein einzelnes Zeichen mit ++shift++ schreibt man als Großbuchstaben (`G`, nicht `shift+g`), `ctrl+R` und `ctrl+r` sind dieselbe Taste.

**Regeln.**

- Innerhalb einer Ansicht gehört jede Taste zu höchstens einer Aktion, und die globalen Tasten (`global_*`) dürfen in keiner Ansicht noch einmal vorkommen. Verschiedene Ansichten dürfen dieselbe Taste nutzen (`a` ist in der Suche der Actor-Filter und in der Statistik die Dimension Agent).
- ++ctrl+c++ beendet immer und lässt sich nicht belegen. Eine Belegung von `global_quit` mit ++ctrl++ (etwa `ctrl+q`) beendet auch, während ein Eingabefeld den Fokus hat.
- Eingabefelder fangen nichts ab: Solange die Suche oder der Repo-Filter den Fokus hat, ist jedes Zeichen Text, egal wie es belegt ist. Der [Fokusvertrag](#fokus) gilt unverändert; `search_focus` und `global_back` sind die einzigen Tasten, die dort etwas anderes tun.
- Ein unbekannter Aktionsname, eine unbekannte Taste oder ein Konflikt ist ein Fehler, aber nur für die Befehle, die die Belegung brauchen: `total-recall tui` bricht beim Start ab, `total-recall config show` und `total-recall doctor` melden ihn. Hooks, Ingest und die übrigen Befehle laufen weiter, damit ein Tippfehler in `[keys]` nie die Archivierung stoppt:

```text
config: [keys] "e" is bound to both stats_days and stats_metric
```

`total-recall config show` listet jede Aktion als `keys.<aktion>` mit ihrer wirksamen Belegung und der Herkunft (`default` oder `file`).

## Grafikmodi

Das Chart der Statistik gibt es als Text (Blockzeichen, überall) und als Bild über das Kitty-Graphics-Protokoll. Umschalter, Legende, Tabelle und Heatmap bleiben in jedem Fall Text.

| Modus | Verhalten |
|---|---|
| `auto` (Default) | Beim Start wird das Terminal einmal sondiert. Ein Bild gibt es nur, wenn die Sondierung beantwortet wird **und** die Umgebung ein Terminal nennt, das Bilder über Unicode-Platzhalter darstellen kann: Ghostty, kitty oder herdr. Bis dahin und sonst: Text. |
| `text` | Nie sondieren, nie ein Bild. Es geht keine Abfrage ans Terminal. |
| `kitty` | Bild erzwingen, ohne Sondierung. Für Terminals, die die Erkennung nicht überstehen (etwa hinter ssh oder tmux), zeigt in anderen Terminals aber nur Platzhalterzeichen. |

`g` wechselt zur Laufzeit `auto` → `text` → `kitty` → `auto`. Die Statuszeile nennt den Modus und bei `auto` den Grund, wenn kein Bild gezeigt wird:

| Statuszeile | Bedeutung |
|---|---|
| `image chart (kitty)` | Bild wird gezeigt |
| `image chart (kitty, forced)` | Bild erzwungen (`--graphics kitty` oder `g`) |
| `text chart` | Modus `text` |
| `text chart (kitty: probing…)` | Sondierung läuft |
| `text chart (kitty: rendering…)` | Bild entschieden, aber noch nicht platziert |
| `text chart (kitty: not detected)` | Umgebung nennt kein Kitty-fähiges Terminal; es wurde nichts gesendet |
| `text chart (kitty: no answer)` | Abfrage ohne Antwort innerhalb von 250 ms |
| `text chart (kitty: no answer; tmux allow-passthrough?)` | dasselbe in tmux: meist fehlt `allow-passthrough` |
| `text chart (kitty: no answer; herdr kitty_graphics off?)` | dasselbe in herdr: `terminal.kitty_graphics` ist aus oder das äußere Terminal kann keine Kitty Graphics |
| `text chart (kitty: outer terminal unknown)` | tmux hat geantwortet, aber nichts weist auf Ghostty hin |
| `text chart (kitty: no placeholders in …)` | das Terminal antwortet, kann aber keine Unicode-Platzhalter (WezTerm, Konsole) |
| `text chart (kitty: ghostty 1.3.x in tmux needs 1.4)` | Ghostty vor 1.4 zeigt Platzhalter in tmux fehlerhaft |
| `text chart (kitty: not probed)` | zurück auf `auto`, aber die Sondierung kann in diesem Prozess nicht mehr laufen |
| `text chart (kitty: render error)` | das Bild konnte nicht gerendert werden |

### Voraussetzungen

| Umgebung | Ergebnis |
|---|---|
| Ghostty ≥ 1.0, nativ | Bild |
| kitty | Bild |
| Ghostty in tmux ≥ 3.3 | Bild, wenn `allow-passthrough` an ist, der tmux-Server aus Ghostty gestartet wurde und Ghostty ≥ 1.4 ist; sonst Text mit Grund |
| herdr in Ghostty | Bild, wenn herdr die Sondierung beantwortet: `terminal.kitty_graphics` an und ein äußeres Terminal mit Kitty Graphics; sonst Text mit Grund |
| tmux ohne Passthrough, Zellij | Text, Hinweis in der Statuszeile |
| WezTerm, Konsole, Warp | Text: sie beantworten die Sondierung, zeigen Platzhalter aber als Zeichensalat |
| Terminal.app, iTerm2, Alacritty, Windows Terminal | Text |
| ssh | Bild, wenn die entfernte Shell `TERM=xterm-ghostty` sieht (ssh reicht `TERM` standardmäßig durch) und `COLORTERM=truecolor` gesetzt ist; sonst Text, `g` `g` erzwingt |

**tmux.** Die Bildsequenzen werden in DCS-Passthrough verpackt; dafür muss tmux ≥ 3.3 sie durchlassen:

```text
# ~/.tmux.conf
set -g allow-passthrough on
```

tmux ersetzt in seinen Panes `TERM` und `TERM_PROGRAM`, daher erkennt die TUI Ghostty dort an `GHOSTTY_RESOURCES_DIR` bzw. `GHOSTTY_BIN_DIR`. Die Variablen hat nur ein tmux-Server, der aus Ghostty heraus gestartet wurde (`tmux show-environment -g | grep GHOSTTY`). Fehlen sie, meldet die Statuszeile `outer terminal unknown`; dann hilft ein Neustart des tmux-Servers aus Ghostty, die Variable beim Aufruf (`GHOSTTY_RESOURCES_DIR=1 total-recall tui stats`) oder `g` `g` zum Erzwingen.

**herdr.** herdr setzt in seinen Panes `TERM_PROGRAM=herdr` und `TERM=xterm-256color` und reicht die Ghostty-Variablen nicht durch. Die Chart-Bibliothek würde dort gar nicht sondieren, deshalb schickt die TUI die Kitty-Abfrage (`a=q`) selbst und wartet 250 ms. herdr zeichnet Platzhalter selbst und reicht das Bild an das äußere Terminal weiter; es antwortet nur, solange `terminal.kitty_graphics` an ist und das äußere Terminal Kitty Graphics kann. Bleibt die Antwort aus, meldet die Statuszeile `no answer; herdr kitty_graphics off?`, `g` `g` erzwingt. `NTCHARTS_KITTY=supported|unsupported` gilt auch in herdr und ersetzt die Abfrage.

**ssh.** Die Sondierung läuft durch ssh hindurch, die Umgebungsvariablen nicht alle: `TERM` kommt an, `TERM_PROGRAM` und `COLORTERM` normalerweise nicht. Ohne Truecolor gehen die Platzhalter kaputt (die Bild-ID steckt in der Vordergrundfarbe), also auf der Gegenseite `COLORTERM=truecolor` setzen oder per `SendEnv`/`AcceptEnv` durchreichen.

**Wie das Bild entsteht.** Das Chart wird außerhalb der Hauptschleife als PNG gerendert, einmal je Änderung an das Terminal übertragen und über Unicode-Platzhalterzellen platziert; der Hintergrund ist transparent, Terminalfarben scheinen durch. Die Bildgröße folgt der Zellenzahl des Chart-Bereichs mal Zellgröße in Pixeln, die das Terminal auf Anfrage meldet (Rückfall 10×20 px). Ein Wechsel von Periode, Dimension, Kennzahl, Fenster oder Größe ersetzt das Bild; die Heatmap verkleinert den Chart-Bereich.

!!! warning "Noch nicht in echten Terminals geprüft"
    Der Bildpfad ist bisher nur in einem Pseudo-Terminal mit nachgestellten Ghostty-Antworten getestet. Die manuelle Prüfung in Ghostty (nativ, in tmux mit und ohne Passthrough), Terminal.app und bei 80×24 steht aus ([#40](https://github.com/seletz/total-recall/issues/40)); der Textpfad ist davon unabhängig.

## Bekannte Grenzen

- **Suche:** höchstens 200 Treffer je Abfrage, sortiert nach Rang. Der Zeitfilter startet bei `all`, nicht bei 90 Tagen. Die Filter `a`, `t`, `r` wirken nur, wenn die Liste den Fokus hat.
- **Projektnamen** kommen aus einer Heuristik: Repositories heißen wie der Basisname ihres Git-Roots (bei gleichen Namen mit so vielen übergeordneten Verzeichnissen wie nötig), Verzeichnisse ohne Repository erscheinen als Pfad, das Home-Verzeichnis darin als `~` (`~/notes`). Die Projekt-Ebene aus [#4](https://github.com/seletz/total-recall/issues/4) ersetzt das später.
- **Heatmap** nur ab etwa 25 Terminalzeilen.
- **Aktivität:** der Balken ist nur Text, kein Bild; die Tagesgrenze kommt aus der Config, nicht aus der Ansicht; kein Sprung aus einem Tag in die Session. Feiertage kennt nur Deutschland (je Bundesland); Urlaub und Krankheit sind der Ansicht unbekannt und zählen als Arbeitstage ohne Aktivität.
- **Statistik:** Feiertage markiert nur das Text-Chart in der Achse; das Bild (Kitty Graphics) hat keine farbigen Achsenbeschriftungen.
- **Bilder** nur in Ghostty, kitty und herdr; andere Terminals mit Kitty Graphics, aber ohne Platzhalter, bekommen Text. Sixel und das iTerm2-Protokoll werden nicht angeboten.
- **Maus** nur für Rad, Tabs und Treffer: kein Klick in Vorschau, Statistik und Aktivität, kein Ziehen in Listen. Textauswahl nur mit Zusatztaste des Terminals oder mit `--no-mouse`.
- **Zwischenablage** über OSC 52; das Terminal muss es erlauben (in tmux `set -g set-clipboard on`).
- **Große Sessions:** Vorschau und Detail laden den Verlauf vollständig; bei Sessions mit mehreren zehntausend Events dauert das Einblenden der Tools einen Moment (gemessen unter 0,1 s).
