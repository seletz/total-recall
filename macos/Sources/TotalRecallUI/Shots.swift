import AppKit
import SwiftUI
import TotalRecallCore

/// One screenshot of the spike: the counterpart of a Shot of
/// internal/screenshot/frames.go, same name, start view, clock and state.
/// Instead of key presses the setup calls the methods the keys call.
@MainActor
public struct Shot {
    public let name: String
    public let size: CGSize
    public let tab: Tab
    public let query: String
    public let now: Date
    public let caption: String
    public let setup: @MainActor (AppState) async -> Void

    init(_ name: String, tab: Tab, query: String = "", now: Date, size: CGSize = Shot.regular, caption: String,
         setup: @escaping @MainActor (AppState) async -> Void = { _ in }) {
        self.name = name
        self.tab = tab
        self.query = query
        self.now = now
        self.size = size
        self.caption = caption
        self.setup = setup
    }

    static let regular = CGSize(width: 1280, height: 800)
    static let compact = CGSize(width: 900, height: 600)
}

enum DemoClock {
    static let berlin = TimeZone(identifier: "Europe/Berlin")!
    static func at(_ y: Int, _ m: Int, _ d: Int, _ h: Int) -> Date {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = berlin
        return c.date(from: DateComponents(year: y, month: m, day: d, hour: h))!
    }
    /// The clocks of frames.go.
    static let search = at(2026, 11, 2, 10)
    static let stats = at(2026, 11, 1, 12)
    static let week = at(2026, 6, 7, 12)
    static let month = at(2026, 6, 30, 12)
}

extension AppState {
    /// enter on the n-th memory hit; then the steps of the memory viewer.
    func openMemoryHit() async {
        search.filters.types = ["memory"]
        await search.search()
        await openSelectedHit()
    }
    var memory: MemoryModel? { if case .memory(let m)? = route { return m }; return nil }
    var session: SessionModel? { if case .session(let m)? = route { return m }; return nil }
    var attachments: AttachmentModel? { if case .attachments(let m)? = route { return m }; return nil }
    var day: DayModel? { if case .day(let m)? = route { return m }; return nil }
}

@MainActor
public enum Shots {
    static let q = "goreleaser cask"

    /// The shots in the order of frames.go (docs/reference/30-tui.md).
    public static let all: [Shot] = [
        Shot("search", tab: .search, query: q, now: DemoClock.search, caption: "Suche mit Vorschau"),
        Shot("search-types", tab: .search, query: q, now: DemoClock.search, caption: "Suche über Events, Sessions und Gedächtnis, Treffer je Typ") { a in
            a.search.toggleType("session"); a.search.toggleType("memory"); await a.search.search()
        },
        Shot("search-memory", tab: .search, query: q, now: DemoClock.search, caption: "Suche nur im Gedächtnis, rechts die Datei als Markdown") { a in
            a.search.filters.types = ["memory"]; await a.search.search()
        },
        Shot("memory", tab: .search, query: q, now: DemoClock.search, caption: "Memory-Viewer eines Projekts") { a in
            await a.openMemoryHit()
            a.memory?.selectedFile = "MEMORY.md"
        },
        Shot("memory-history", tab: .search, query: q, now: DemoClock.search, caption: "Verlauf einer Memory-Datei, ein früherer Stand") { a in
            await a.openMemoryHit()
            a.memory?.selectedFile = "MEMORY.md"
            await a.memory?.openHistory()
            a.memory?.selectedVersion = 1
        },
        Shot("memory-deleted", tab: .search, query: q, now: DemoClock.search, caption: "Verlauf einer gelöschten Memory-Datei") { a in
            await a.openMemoryHit()
            a.memory?.selectedFile = "old-notes.md"
            await a.memory?.openHistory()
        },
        Shot("memory-diff", tab: .search, query: q, now: DemoClock.search, caption: "Diff zweier Versionen einer Memory-Datei") { a in
            await a.openMemoryHit()
            a.memory?.selectedFile = "MEMORY.md"
            await a.memory?.openHistory()
            a.memory?.selectedVersion = 1
            a.memory?.showDiff = true
        },
        Shot("memory-timeline", tab: .search, query: q, now: DemoClock.search, caption: "Zeitleiste eines Projekts mit Diff der markierten Änderung") { a in
            await a.openMemoryHit()
            await a.memory?.openTimeline()
            a.memory?.selectedChange = 1
        },
        Shot("search-attachment", tab: .search, query: "image", now: DemoClock.search, caption: "Treffer mit Anhang"),
        Shot("search-attachment-filter", tab: .search, now: DemoClock.search, caption: "Filter „nur mit Anhang“ bei leerer Suche") { a in
            a.search.toggleType("session"); a.search.toggleAttached(); await a.search.search()
        },
        Shot("attachment-image", tab: .search, query: "image", now: DemoClock.search, caption: "Anhang-Viewer: ein Bild") { a in
            await a.openAttachments()
        },
        Shot("attachment-document", tab: .search, query: "image", now: DemoClock.search, caption: "Anhang-Viewer: ein Dokument mit Metadaten") { a in
            await a.openAttachments()
            await a.attachments?.select(1)
        },
        Shot("attachment-narrow", tab: .search, query: "image", now: DemoClock.search, size: Shot.compact, caption: "Anhang-Viewer in einem kleinen Fenster") { a in
            await a.openAttachments()
        },
        Shot("session-attachment", tab: .search, query: "image", now: DemoClock.search, caption: "Session mit Anhängen") { a in
            await a.openSelectedHit()
        },
        Shot("search-tool", tab: .search, query: "cask generated", now: DemoClock.search, caption: "Treffer in einer Tool-Ausgabe, JSON eingerückt"),
        Shot("search-pin", tab: .search, query: "tests", now: DemoClock.search, caption: "Projekt-Pin") { a in
            a.search.toggleType("session"); a.search.toggleType("memory"); await a.search.search()
            await a.search.move(3)
            a.search.togglePin(); await a.search.search()
        },
        Shot("session", tab: .search, query: q, now: DemoClock.search, caption: "Session-Detail mit Treffer, Tool-Aufrufe eingeblendet") { a in
            await a.openSelectedHit()
            a.session?.showTools = true
        },
        Shot("stats", tab: .stats, now: DemoClock.stats, caption: "Statistik: gestapeltes Chart je Tag und Projekt mit Tabelle"),
        Shot("heatmap", tab: .stats, now: DemoClock.stats, caption: "Heatmap Wochentag × Stunde") { a in a.stats.heatmap = true },
        Shot("activity-week", tab: .activity, now: DemoClock.week, caption: "Aktivität je Tag, eine Woche"),
        Shot("activity-month", tab: .activity, now: DemoClock.month, caption: "Aktivität je Tag, ein Monat") { a in
            a.activity.setKind(.month); await a.activity.load()
        },
        Shot("activity-days", tab: .activity, now: DemoClock.week, caption: "Aktivität, drei Tage zurück (gleitendes Fenster)") { a in
            a.activity.slide(-3); await a.activity.load()
        },
        Shot("activity-top", tab: .activity, now: DemoClock.week, caption: "Aktivität, neuester Tag oben") { a in
            a.activity.newestTop = true
        },
        Shot("activity-projects", tab: .activity, now: DemoClock.week, caption: "Aktivität in Projektfarben mit Legende") { a in
            a.activity.projects = true; await a.activity.load()
        },
        Shot("day", tab: .activity, now: DemoClock.week, caption: "Tagesansicht") { a in
            a.activity.select(-5); await a.openDay()
        },
        Shot("day-sessions", tab: .activity, now: DemoClock.week, caption: "Tagesansicht mit aufgeklappten Sessions") { a in
            a.activity.select(-5); await a.openDay(); a.day?.toggleExpanded()
        },
        Shot("day-all", tab: .activity, now: DemoClock.week, caption: "Tagesansicht über alle Events") { a in
            a.activity.select(-5); await a.openDay(); a.day?.allEvents = true; await a.day?.load()
        },
        Shot("day-compact", tab: .activity, now: DemoClock.week, size: Shot.compact, caption: "Tagesansicht im kleinen Fenster") { a in
            a.activity.select(-5); await a.openDay()
        },
        Shot("compact", tab: .activity, now: DemoClock.week, size: Shot.compact, caption: "Aktivität im kleinen Fenster"),
        Shot("help", tab: .stats, now: DemoClock.stats, caption: "Hilfe mit den Tasten der Statistik") { a in a.showHelp = true },
        Shot("heatmap-compact", tab: .stats, now: DemoClock.stats, size: Shot.compact, caption: "Statistik mit Heatmap im kleinen Fenster") { a in
            a.stats.heatmap = true
        },
        Shot("config-status", tab: .config, now: DemoClock.search, caption: "Config, Seite Status"),
        Shot("config-settings", tab: .config, now: DemoClock.search, caption: "Config, Seite Settings: backup.dir wird bearbeitet") { a in
            a.config.page = .settings
            ConfigUIState.shared.editingKey = "backup.dir"
        },
        Shot("config-actions", tab: .config, now: DemoClock.search, caption: "Config, Seite Actions: Backup wartet auf Bestätigung") { a in
            a.config.page = .actions
            ConfigUIState.shared.confirming = "backup"
        },
        Shot("config-projects", tab: .config, now: DemoClock.search, caption: "Config, Seite Projects") { a in
            a.config.page = .projects
        },
        Shot("config-projects-edit", tab: .config, now: DemoClock.search, caption: "Config, Seite Projects: Formular von kompass mit Farbwahl und Vorschau") { a in
            a.config.page = .projects
            ConfigUIState.shared.editingProject = 0
        },
        Shot("config-projects-pattern", tab: .config, now: DemoClock.search, caption: "Config, Seite Projects: neues Projekt aus dotfiles") { a in
            a.config.page = .projects
            if let k = a.config.unmatched.first(where: { $0.key.hasSuffix("dotfiles") }) {
                ConfigUIState.shared.newProject = ProjectDefinition(name: k.derivedName(home: a.home), match: [a.config.pattern(for: k)])
            }
        },
        Shot("config-setup", tab: .config, now: DemoClock.search, caption: "Config, Seite Setup: Prüfungen und Hook-Block") { a in
            a.config.page = .setup
        },
        Shot("config-setup-mcp", tab: .config, now: DemoClock.search, caption: "Config, Seite Setup: MCP-Eintrag") { a in
            a.config.page = .setup
            ConfigUIState.shared.setupSelection = "mcp"
        },
    ]
}

/// Renders the shots into PNG files: one offscreen window per shot with the
/// root view at the shot's size, drawn with cacheDisplay (no screen
/// recording permission needed). The archive is reached through `runner`.
@MainActor
public enum ShotRenderer {
    public static func render(_ shots: [Shot], runner: CommandRunner, home: String, to dir: URL) async throws {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        for shot in shots {
            ConfigUIState.shared = ConfigUIState()
            let clock = Clock.fixed(shot.now)
            let app = AppState(archive: Archive(runner: runner), clock: clock, home: home, tab: shot.tab, query: shot.query)
            await app.start()
            await shot.setup(app)
            let view = RootView(app: app).frame(width: shot.size.width, height: shot.size.height)
            let host = NSHostingView(rootView: view)
            host.frame = NSRect(origin: .zero, size: shot.size)
            let window = NSWindow(contentRect: host.frame, styleMask: [.borderless], backing: .buffered, defer: false)
            window.contentView = host
            window.setFrameOrigin(NSPoint(x: -10000, y: -10000))
            window.orderFrontRegardless()
            // Let SwiftUI lay out and the lists fill.
            for _ in 0..<6 {
                host.layoutSubtreeIfNeeded()
                try await Task.sleep(for: .milliseconds(120))
            }
            guard let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds) else { print("shot \(shot.name): no bitmap"); continue }
            host.cacheDisplay(in: host.bounds, to: rep)
            guard let png = rep.representation(using: .png, properties: [:]) else { print("shot \(shot.name): no png"); continue }
            try png.write(to: dir.appendingPathComponent("\(shot.name).png"))
            window.orderOut(nil)
            print("shot \(shot.name)")
        }
    }

    /// The README of the screenshot directory: one image per shot with its caption.
    public static func index(_ shots: [Shot]) -> String {
        var s = "# Screenshots\n\nDie Ansichten der macOS-App gegen das Demo-Archiv, je Shot von `internal/screenshot/frames.go` eine Aufnahme (`mise run macos:shots`).\n\n"
        for shot in shots { s += "## \(shot.name)\n\n\(shot.caption)\n\n![\(shot.name)](\(shot.name).png)\n\n" }
        return s
    }
}

/// Transient UI state of the config pages that the shots set (open
/// editor, confirmation): the TUI keeps it in its model, the app in views.
@MainActor @Observable
public final class ConfigUIState {
    public static var shared = ConfigUIState()
    public var editingKey: String?
    public var editValue = ""
    public var confirming: String?
    public var editingProject: Int?
    public var newProject: ProjectDefinition?
    public var setupSelection = "config"
    public var selectedSetting: String?
    public init() {}
}
