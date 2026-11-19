import SwiftUI
import TotalRecallCore

/// The window: a header with the tabs and the route, the view of the
/// current tab or route, the key hints, and the help overlay.
public struct RootView: View {
    @Bindable var app: AppState

    public init(app: AppState) { self.app = app }

    public var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            Divider()
            KeyHints(hints: hints)
                .padding(.horizontal, 12).padding(.vertical, 6)
                .background(.bar)
        }
        .overlay {
            if app.showHelp {
                HelpOverlay(app: app)
            }
        }
        .background(Color(nsColor: .windowBackgroundColor))
        .background(shortcuts)
    }

    var header: some View {
        HStack(spacing: 10) {
            Image(systemName: "archivebox").foregroundStyle(.tint)
            Text("total-recall").font(.headline)
            Text("updated \(app.clock.format(app.updated, "HH:mm:ss"))").font(.caption).foregroundStyle(.secondary)
            Spacer()
            Picker("", selection: Binding(get: { app.tab }, set: { t in Task { await app.select(t) } })) {
                ForEach(Tab.allCases, id: \.self) { t in
                    Label("\(t.title)", systemImage: t.symbol).tag(t)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
            if let r = app.route {
                HStack(spacing: 4) {
                    Image(systemName: "chevron.right").font(.caption)
                    Text(r.title).fontWeight(.medium)
                }
                .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
        .background(.bar)
    }

    @ViewBuilder var content: some View {
        switch app.route {
        case .session(let m)?: SessionView(app: app, model: m)
        case .memory(let m)?: MemoryView(app: app, model: m)
        case .attachments(let m)?: AttachmentView(app: app, model: m)
        case .day(let m)?: DayView(app: app, model: m)
        case nil:
            switch app.tab {
            case .search: SearchView(app: app, model: app.search)
            case .stats: StatsView(app: app, model: app.stats)
            case .activity: ActivityView(app: app, model: app.activity)
            case .config: ConfigView(app: app, model: app.config)
            }
        }
    }

    /// The global keys: ⌘1–⌘4 tabs, esc back, ⌘R reload, ? help.
    var shortcuts: some View {
        ZStack {
            ForEach(Tab.allCases, id: \.self) { t in
                Button("") { Task { await app.select(t) } }.keyboardShortcut(KeyEquivalent(t.key), modifiers: .command)
            }
            Button("") { app.pop() }.keyboardShortcut(.cancelAction)
            Button("") { Task { await app.reload() } }.keyboardShortcut("r", modifiers: .command)
            Button("") { app.showHelp.toggle() }.keyboardShortcut("?", modifiers: [.command, .shift])
        }
        .opacity(0)
        .allowsHitTesting(false)
    }

    var hints: [(String, String)] {
        let global: [(String, String)] = [("⌘1–4", "views"), ("⌘?", "help"), ("⌘R", "reload")]
        let local: [(String, String)]
        switch app.route {
        case .session?: local = [("↑↓", "scroll"), ("n/N", "next/prev hit"), ("T", "tools"), ("v", "attachments"), ("y", "copy id"), ("esc", "back")]
        case .memory?: local = [("↑↓", "move"), ("↩", "history"), ("d", "diff"), ("t", "timeline"), ("esc", "back")]
        case .attachments?: local = [("←→", "browse"), ("o", "open in Preview"), ("esc", "back")]
        case .day?: local = [("↑↓", "block"), ("space", "sessions"), ("←→", "day"), ("h", "human/all"), ("↩", "session"), ("esc", "back")]
        case nil:
            switch app.tab {
            case .search: local = [("↩", "open"), ("e/s/m", "types"), ("a", "actor"), ("i", "attached"), ("t", "time"), ("p", "pin project"), ("v", "attachments"), ("⌃R", "raw fts")]
            case .stats: local = [("d/w/m", "period"), ("p/a/v", "by"), ("←→", "range"), ("-/+", "zoom"), ("e", "metric"), ("h", "heatmap")]
            case .activity: local = [("d/w/m", "period"), ("←→", "earlier/later"), ("↑↓", "slide"), ("c/l", "this/last"), ("K/J", "select"), ("↩", "day"), ("h", "human/all"), ("p", "projects"), ("o", "order")]
            case .config: local = [("⇥", "page"), ("↑↓", "move"), ("↩", "edit/run")]
            }
        }
        return local + global
    }
}

/// The help: the keys of the current view and the global keys.
struct HelpOverlay: View {
    let app: AppState
    var body: some View {
        ZStack {
            Color.black.opacity(0.25).ignoresSafeArea()
                .onTapGesture { app.showHelp = false }
            VStack(alignment: .leading, spacing: 12) {
                Text("Keys: \(app.route?.title ?? app.tab.rawValue)").font(.title3.bold())
                Grid(alignment: .leading, horizontalSpacing: 28, verticalSpacing: 6) {
                    ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                        GridRow {
                            ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                                if cell.0.isEmpty {
                                    Color.clear.frame(width: 1, height: 1)
                                } else {
                                    HStack(spacing: 6) {
                                        KeyCap(key: cell.0)
                                        Text(cell.1)
                                    }
                                }
                            }
                        }
                    }
                }
                Text("esc or ⌘? closes this help").font(.caption).foregroundStyle(.secondary)
            }
            .padding(20)
            .background(RoundedRectangle(cornerRadius: 12).fill(.regularMaterial))
            .shadow(radius: 20)
            .frame(maxWidth: 720)
        }
    }

    /// Four columns as the TUI: view keys in three, globals in the last.
    var rows: [[(String, String)]] {
        let local: [(String, String)]
        switch app.tab {
        case .stats: local = [("d", "days"), ("w", "weeks"), ("m", "months"), ("e", "metric"), ("p", "by project"), ("a", "by agent"), ("v", "by directory"), ("h", "heatmap"), ("←", "earlier"), ("→", "later"), ("-", "zoom out"), ("+", "zoom in")]
        case .activity: local = [("d", "day"), ("w", "week"), ("m", "month"), ("h", "human/all"), ("←", "earlier"), ("→", "later"), ("c", "this period"), ("l", "last period"), ("K/J", "select day"), ("↩", "open day"), ("p", "project colours"), ("o", "newest on top")]
        case .search: local = [("↩", "open"), ("e", "events"), ("s", "sessions"), ("m", "memory"), ("a", "actor"), ("t", "time"), ("r", "repo"), ("i", "attached"), ("p", "pin project"), ("v", "attachments"), ("⌃R", "raw FTS"), ("⇥", "list/input")]
        case .config: local = [("⇥", "next page"), ("⇧⇥", "prev page"), ("↑↓", "move"), ("↩", "edit / run"), ("y", "confirm"), ("c", "copy block"), ("a", "add project"), ("x", "remove"), ("E", "open config")]
        }
        let global: [(String, String)] = [("⌘1", "search"), ("⌘2", "stats"), ("⌘3", "activity"), ("⌘4", "config"), ("⌘R", "reload"), ("⌘?", "help"), ("esc", "back"), ("⌘Q", "quit")]
        var out: [[(String, String)]] = []
        let perCol = 4
        let cols = stride(from: 0, to: local.count, by: perCol).map { Array(local[$0..<min($0 + perCol, local.count)]) }
        for r in 0..<max(perCol, global.count / 2) {
            var row: [(String, String)] = []
            for c in cols { row.append(r < c.count ? c[r] : ("", "")) }
            row.append(r < global.count / 2 ? global[r] : ("", ""))
            row.append(r + global.count / 2 < global.count ? global[r + global.count / 2] : ("", ""))
            out.append(row)
        }
        return out
    }
}
