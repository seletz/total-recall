---
title: Erklärungen
icon: lucide/lightbulb
weight: 4
---
# Erklärungen

Hintergrund, Entwurfsentscheidungen, Begründungen: warum `total-recall` so gebaut ist, wie es ist. Hier wird nichts angeleitet und nichts nachgeschlagen; das *Wie* steht in den [How-To-Guides](../how-to/index.md), das *Was* in der [Referenz](../reference/index.md).

In diesem Abschnitt:

- [Datenschutz](datenschutz.md): was gespeichert und maskiert wird, wo die Dateien liegen, warum die Datenbank nicht in iCloud liegt.
- [Design (Spezifikation)](design.md): die verbindliche Spezifikation: Ziele, Architektur, Datenmodell, Actor-Klassifikation, Redaction, Hooks, Schema-Versionierung, CLI.
- [Einlesen über Hooks](hooks.md): was ein Hook-Lauf liest, warum `--detach`, wie das Nachlesen Lücken auffängt.
- [Spike: TUI (Historie)](spike-tui.md): Entwurf, Messwerte und die Entscheidung, die TUI zu übernehmen; Protokoll mit Stand vom November 2026.
- [Spike: Web-UI mit Svelte](spike-webui.md): Prototyp `total-recall serve` mit eingebetteter Svelte-UI, Sicherheit, Build, Messwerte und Empfehlung; Entscheidung offen.
