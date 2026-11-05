---
title: Stundenzettel füllen
icon: lucide/receipt
---
# Stundenzettel füllen

Wie viele Stunden gingen gestern in welches Projekt? `total-recall` schätzt das aus deinen eigenen Nachrichten an den Agenten: je Tag und Kategorie die aktive Zeit, roh und auf ein Raster gerundet, dazu die Sessions mit Titel als Vorlage für den Buchungstext. Die Anwesenheit (erste und letzte Aktivität) liefert daneben [`stats --workday`](40-arbeitszeit.md).

!!! warning "Eine Untergrenze"
    Gezählt wird nur, was Spuren im Archiv hinterlässt: die Lücken zwischen deinen Nachrichten an einen Agenten. Meetings, Lesen, Denken, Telefonate und alles, was ohne Agent passiert, fehlen. Die Werte sind eine Untergrenze und ein Gedächtnis, keine Zeiterfassung.

## Die Methode

- Alle eigenen Nachrichten (`actor = 'human'`) aller Sessions bilden eine Zeitlinie, ein Tag beginnt um `day_start` (Standard 04:00).
- Die Lücke zwischen zwei aufeinanderfolgenden Nachrichten zählt für das Projekt der späteren: bis 20 Minuten voll, darüber nur 3 Minuten (eine Pause oder ein Meeting zählt also nicht, die Nachricht danach ein wenig). Die erste Nachricht eines Tages zählt 3 Minuten, die Zeit nach der letzten gar nicht.
- Projekte werden Kategorien zugeordnet, etwa je Kunde. Die Summe je Kategorie und Tag wird auf 15 Minuten aufgerundet.

Alle Zahlen sind einstellbar, siehe [`[worktime]`](../reference/20-konfiguration.md#worktime).

## Kategorien einrichten

Ohne Regeln ist jedes Projekt seine eigene Kategorie, benannt wie in `stats --by project`. Für den Stundenzettel sind meist Kunden oder Kostenstellen gefragt; die Regeln stehen in der Konfiguration, in Reihenfolge, die erste passende gewinnt:

```toml
[[worktime.category]]
name = "kunde-a"
match = ["https://github.com/kunde-a/*", "~/src/kunde-a-*"]

[[worktime.category]]
name = "intern"
match = ["total-recall", "dotfiles", "~/notizen"]

[[worktime.category]]
name = "sonstiges"
match = "*"
```

Ein Muster passt auf den Projektnamen, den Git-Root (als Pfad oder mit `~`) oder die Remote-URL; `*` steht für beliebigen Text, auch über `/` hinweg. Wer seine Projekte in [`[[project]]`](../reference/20-konfiguration.md#project) definiert hat, kann hier einfach deren Namen nennen: Die Definitionen gelten zuerst, die Regeln sehen den konfigurierten Namen. Was keine Regel trifft, bleibt sein eigenes Projekt; die letzte Regel mit `*` sammelt es ein, wenn du das möchtest. `total-recall config show` zeigt die wirksamen Regeln, ein Fehler darin stoppt nur `stats --worktime`, nie die Hooks.

## Den Tag oder die Woche abrufen

```sh
total-recall stats --worktime --range today
total-recall stats --worktime --range last-week
total-recall stats --worktime --since 2026-10-26 --until 2026-10-30
```

```text
2026-10-26 04:00 – 2026-10-31 03:59 (Europe/Berlin, day starts 04:00, human events only)
gaps up to 20m count in full, longer ones 3m · rounded up to 15m per category and day · holidays Baden-Württemberg
a lower bound: meetings and work without an agent leave no messages

DATE        DAY  CATEGORY     RAW    ROUNDED  HOLIDAY
2026-10-26  Mon  fahrtenbuch  45m    45m      -
2026-10-26  Mon  kompass      2h32m  2h45m    -
2026-10-26  Mon  wetterwarte  26m    30m      -
2026-10-27  Tue  fahrtenbuch  55m    1h00m    -
2026-10-27  Tue  kompass      3h13m  3h15m    -
…

CATEGORY     RAW     ROUNDED  DAYS
fahrtenbuch  3h05m   3h15m    3
kompass      11h29m  12h00m   4
wetterwarte  1h19m   1h30m    2
total        15h53m  16h45m   4
```

`ROUNDED` ist der Wert für den Stundenzettel, `RAW` die Summe der Gutschriften. Ein Tag ohne Nachrichten erscheint mit `-`: Urlaub, Meeting-Tag oder vergessen, das weiß das Archiv nicht.

## Buchungstexte aus den Sessions

`--sessions` listet je Tag und Kategorie die Sessions mit ihrem Titel; Tage ohne Aktivität entfallen:

```sh
total-recall stats --worktime --range last-week --sessions
```

```text
2026-10-26 Mon: 3h43m raw, 4h00m rounded
  fahrtenbuch: 45m raw, 45m rounded
       45m  7d1e02c0  Fahrten zusammenfassen
  kompass: 2h32m raw, 2h45m rounded
       45m  7d1e02bb  GPX-Export
        9m  7d1e02bc  Tiles-Cache nach Neustart leer
       44m  7d1e02bd  Wegpunkte stabil sortieren
       53m  7d1e02bf  Umlaute in Ortsnamen
  wetterwarte: 26m raw, 30m rounded
       26m  7d1e02be  Frostwarnung
```

Die Titel sind die von Claude Code vergebenen oder deine eigenen (`/rename`); `total-recall show 7d1e02bb` zeigt die Session dahinter. Fasst eine Kategorie mehrere Projekte zusammen, steht ihre Aufteilung als Zeile `projects:` darüber.

### Referenzen und ein Vorschlag für den Text

Der Titel sagt, *was* die Session war, aber nicht, *woran* du gearbeitet hast. `--summary` sucht dafür in den Session-Titeln und deinen eigenen Nachrichten nach Issue- und PR-Nummern und baut je Tag und Kategorie einen Vorschlag für den Buchungstext; `--sessions` und `--summary` lassen sich kombinieren:

```sh
total-recall stats --worktime --range last-week --summary
total-recall stats --worktime --range last-week --sessions --summary
```

```text
2026-10-26 Mon: 3h43m raw, 4h00m rounded
  fahrtenbuch: 45m raw, 45m rounded
    refs: -
    text: fahrtenbuch: Fahrten zusammenfassen
  kompass: 2h32m raw, 2h45m rounded
    refs: kompass #31, #48, PR #52
    text: kompass: GPX-Export (#31/PR #52), Tiles-Cache nach Neustart leer (#31), Wegpunkte stabil sortieren (#48), Umlaute in Ortsnamen (#48)
  wetterwarte: 26m raw, 30m rounded
    refs: wetterwarte mara/wetterwarte#238
    text: wetterwarte: Frostwarnung (mara/wetterwarte#238)
```

- `refs` sind alle Referenzen der Kategorie an dem Tag, je Projekt gruppiert, ohne Doppelte, nach Repository und Nummer sortiert; `-` ohne eine. Erkannt werden `#31`, `PR #32` (auch `pull request #32`), `owner/repo#123` und die Issue- und Pull-URLs von GitHub und GitLab (`…/owner/repo/issues/123` wird zu `owner/repo#123`, `…/pull/124` zu `PR owner/repo#124`). Ein `#` direkt hinter einem Buchstaben (`C#1`), ein Farbcode (`#1a2b3c`) oder der Anker einer URL zählen nicht.
- `text` ist der Vorschlag: `<projekt>: <session>, <session>; <projekt>: …`. Eine Session steht mit ihrem Titel, dahinter in Klammern die Referenzen aus ihren Nachrichten, die der Titel nicht schon nennt; ohne Titel stehen nur die Referenzen (`#19/#26`), ohne beides der Anfang deiner ersten Nachricht. Der Text wird auf `summary_max_chars` Zeichen gekürzt (Standard 200, er endet dann mit `…`).

Gezählt werden nur deine eigenen Nachrichten (`actor = 'human'`) und die Titel. Nummern, die der Agent schreibt oder ein Werkzeug ausgibt (etwa `gh issue list`), fehlen absichtlich: sonst stünde jede Issue-Liste im Buchungstext. Der Vorschlag ist eine Vorlage, kein fertiger Text: Kürze ihn, wenn der Stundenzettel weniger verträgt, und prüfe die Nummern, wenn dieselbe Nummer in mehreren Repositories vorkommt.

```toml
[worktime]
summary_max_chars = 120   # kürzer; 0 = ohne Grenze
```

## Mehrere Agenten: Aufträge eines Koordinators { #mehrere-agenten }

Verteilt ein koordinierender Agent Aufgaben an andere Sessions, indem er sie in deren Terminal tippt oder als Block einfügt, stehen diese Aufträge im Transkript wie eigene Nachrichten. Ohne Gegenmaßnahme zählt jeder Auftrag als Arbeitszeit, die letzte Aktivität rutscht in die Nacht und die Lücken zwischen den Aufträgen landen auf dem Stundenzettel. Nachrichten, die eine Session über den Socket an eine andere schickt, erkennt `total-recall` von selbst als `peer`; für das Tippen und Einfügen braucht es zwei Regeln in der Konfiguration:

```toml
[harness.claude.actors]
peer_prefixes = ["From coordinator:", "Vom Koordinator:"]
peer_paste_headers = ["# Task:", "Auftrag:"]
```

- `peer_prefixes`: Beginnt eine Nachricht mit einem der Texte, stammt sie von einem anderen Agenten (`peer`).
- `peer_paste_headers`: Besteht eine Nachricht nur aus einem eingefügten Block und beginnt dessen erste Zeile mit einem der Texte, ebenso. Ein Block, den du selbst einfügst (ein Log, eine Fehlermeldung), hat eine andere erste Zeile und bleibt `human`; ein Block mit eigenem Text davor oder danach auch.

Der Koordinator muss die Aufträge entsprechend beginnen; das lässt sich in seinen Anweisungen festlegen. Die Regeln gelten für alles, was ab jetzt eingelesen wird. Für die Tage davor:

```sh
total-recall reindex --actors --dry-run   # 17 would change (human -> peer: 17)
total-recall reindex --actors
```

Danach zählen `stats --worktime` und `stats --workday` nur noch, was du selbst geschrieben hast. Details zu den Regeln: [`[harness.claude.actors]`](../reference/20-konfiguration.md#harnessclaudeactors), zum Befehl: [`reindex`](../reference/10-befehle.md#reindex).

## Für Skripte und Agenten

`--json` liefert dieselben Werte in Sekunden, Projekte, Sessions, `refs` und `text` immer eingeschlossen; `--format jsonl` eine Zeile. Alle Felder: [`stats --worktime`](../reference/10-befehle.md#stats-worktime). Ein Agent mit dem [Skill](50-agenten-anbinden.md) kann den Stundenzettel damit vorbereiten, etwa je Tag und Kategorie eine Buchung mit `text` als Vorschlag und `refs` zum Nachschlagen.

## Rundung anpassen

```toml
[worktime]
grid = "6m"            # Zehntelstunden
rounding = "nearest"   # kaufmännisch statt aufrunden
scope = "project"      # jedes Projekt einzeln runden, die Kategorie ist die Summe
```

`scope = "project"` rundet bei mehreren Projekten je Kategorie stärker auf; `category` (Standard) rundet die Summe einmal. `grid = "0m"` schaltet die Rundung ab, dann sind `RAW` und `ROUNDED` gleich.
