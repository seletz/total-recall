import SwiftUI
import TotalRecallCore

/// The memory viewer: files, history of a file (text or diff) and the
/// timeline of a project (TUI view memory).
struct MemoryView: View {
    let app: AppState
    @Bindable var model: MemoryModel

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            switch model.mode {
            case .files: filesPane
            case .history: historyPane
            case .timeline: timelinePane
            }
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(phases: .down) { handle($0) }
    }

    var modeTitle: String {
        switch model.mode {
        case .files: "files"
        case .history: "history \(model.selectedFile ?? "")"
        case .timeline: "timeline"
        }
    }

    var header: some View {
        HStack(spacing: 8) {
            Swatch(hex: app.projectColors[model.project])
            Text(model.project).font(.headline)
            Text(modeTitle).foregroundStyle(.secondary)
            Spacer()
            if let e = model.error { Text(e).foregroundStyle(.red).font(.caption) }
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
    }

    // MARK: keys

    func handle(_ p: KeyPress) -> KeyPress.Result {
        switch p.key {
        case .upArrow: move(-1); return .handled
        case .downArrow: move(1); return .handled
        case .return:
            if model.mode == .files { Task { await model.openHistory() } }
            return .handled
        case .escape:
            if model.mode != .files { model.mode = .files; return .handled }
            return .ignored
        default: break
        }
        switch p.characters {
        case "k": move(-1); return .handled
        case "j": move(1); return .handled
        case "d":
            if model.mode == .history { model.showDiff.toggle(); return .handled }
        case "t":
            if model.mode == .files { Task { await model.openTimeline() }; return .handled }
        default: break
        }
        return .ignored
    }

    func move(_ d: Int) {
        switch model.mode {
        case .files:
            guard let i = model.files.firstIndex(where: { $0.path == model.selectedFile }) else { return }
            let n = min(max(i + d, 0), model.files.count - 1)
            if model.files.indices.contains(n) { model.selectedFile = model.files[n].path }
        case .history:
            model.selectedVersion = min(max(model.selectedVersion + d, 0), max(model.history.count - 1, 0))
        case .timeline:
            model.selectedChange = min(max(model.selectedChange + d, 0), max(model.changes.count - 1, 0))
        }
    }

    // MARK: files

    var filesPane: some View {
        HSplitView {
            List(selection: $model.selectedFile) {
                ForEach(model.files) { f in
                    HStack(spacing: 8) {
                        Image(systemName: f.deleted ? "trash" : "doc.text")
                            .foregroundStyle(f.deleted ? Color.secondary : Color.accentColor)
                            .frame(width: 16)
                        Text(f.path)
                            .strikethrough(f.deleted)
                            .foregroundStyle(f.deleted ? .secondary : .primary)
                            .lineLimit(1)
                        Spacer(minLength: 8)
                        Text("\(f.versions) v").font(.caption).foregroundStyle(.secondary)
                        Text(app.clock.minute(f.changed)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                    .tag(f.path)
                    .contentShape(Rectangle())
                    .onTapGesture(count: 2) {
                        model.selectedFile = f.path
                        Task { await model.openHistory() }
                    }
                }
            }
            .frame(minWidth: 320, idealWidth: 420)
            ScrollView {
                MemoryBlobText(model: model, hash: model.currentFile?.sha256, deleted: model.currentFile?.deleted ?? false)
                    .padding(14)
            }
            .frame(minWidth: 320, maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: history

    var historyPane: some View {
        HSplitView {
            List(selection: Binding(get: { Optional(model.selectedVersion) }, set: { model.selectedVersion = $0 ?? 0 })) {
                ForEach(Array(model.history.enumerated()), id: \.offset) { i, v in
                    HStack(spacing: 8) {
                        Image(systemName: v.deleted ? "trash" : "clock").foregroundStyle(v.deleted ? Color.red : .secondary).frame(width: 16)
                        Text(app.clock.minute(v.ts)).font(.body.monospacedDigit())
                        Spacer(minLength: 8)
                        if v.deleted {
                            Text("deleted").font(.caption).foregroundStyle(.red)
                        } else {
                            Text(Fmt.bytes(v.bytes)).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .tag(i)
                }
            }
            .frame(minWidth: 260, idealWidth: 320)
            VStack(alignment: .leading, spacing: 0) {
                if let (v, prev) = model.versionPair {
                    if model.showDiff {
                        HStack {
                            Text("diff \(prev.map { app.clock.minute($0.ts) } ?? "(none)") → \(app.clock.minute(v.ts))")
                                .font(.callout).foregroundStyle(.secondary)
                            Spacer()
                            KeyCap(key: "d")
                        }
                        .padding(.horizontal, 14).padding(.vertical, 6)
                        Divider()
                        ScrollView { MemoryDiffPane(model: model, version: v, previous: prev).padding(14) }
                    } else {
                        ScrollView { MemoryBlobText(model: model, hash: v.sha256, deleted: v.deleted).padding(14) }
                    }
                } else {
                    Spacer()
                }
            }
            .frame(minWidth: 320, maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: timeline

    var timelinePane: some View {
        HSplitView {
            List(selection: Binding(get: { Optional(model.selectedChange) }, set: { model.selectedChange = $0 ?? 0 })) {
                ForEach(Array(model.changes.enumerated()), id: \.offset) { i, c in
                    HStack(spacing: 8) {
                        Text(app.clock.minute(c.version.ts)).font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                        Text(c.kind.rawValue).font(.body.monospaced().bold()).foregroundStyle(color(c.kind)).frame(width: 14)
                        Text(c.path).lineLimit(1)
                        Spacer(minLength: 8)
                        Text(delta(c.delta)).font(.caption.monospacedDigit()).foregroundStyle(c.delta < 0 ? Theme.removed : Theme.added)
                    }
                    .tag(i)
                }
            }
            .frame(minWidth: 380, idealWidth: 460)
            VStack(alignment: .leading, spacing: 0) {
                if model.changes.indices.contains(model.selectedChange) {
                    let c = model.changes[model.selectedChange]
                    TimelineDetail(app: app, model: model, change: c)
                } else {
                    Spacer()
                }
            }
            .frame(minWidth: 320, maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    func color(_ k: MemoryModel.Change.Kind) -> Color {
        switch k {
        case .added: Theme.added
        case .changed: .orange
        case .deleted: Theme.removed
        }
    }

    func delta(_ d: Int) -> String { (d < 0 ? "−" : "+") + Fmt.bytes(abs(d)) }
}

/// The detail of a timeline change: header with the line stat, the diff.
private struct TimelineDetail: View {
    let app: AppState
    let model: MemoryModel
    let change: MemoryModel.Change
    @State private var stat: (added: Int, removed: Int)?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 3) {
                Text(change.path).font(.headline)
                HStack(spacing: 10) {
                    Text(model.project).foregroundStyle(.secondary)
                    Text("\(change.previous.map { app.clock.minute($0.ts) } ?? "(none)") → \(app.clock.minute(change.version.ts))")
                        .font(.callout.monospacedDigit())
                    if let s = stat {
                        Text("+\(s.added)").foregroundStyle(Theme.added)
                        Text("-\(s.removed)").foregroundStyle(Theme.removed)
                    }
                }
                .font(.callout)
            }
            .padding(.horizontal, 14).padding(.vertical, 8)
            Divider()
            ScrollView { MemoryDiffPane(model: model, version: change.version, previous: change.previous).padding(14) }
        }
        .task(id: change.id) {
            let old = await model.text(change.previous?.sha256)
            let new = await model.text(change.version.sha256)
            stat = LineDiff.stat(old: old, new: new)
        }
    }
}

/// The text of a stored version as Markdown.
struct MemoryBlobText: View {
    let model: MemoryModel
    let hash: String?
    var deleted = false
    @State private var text = ""

    var body: some View {
        Group {
            if hash == nil {
                Text(deleted ? "(deleted)" : "(empty)").foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                MarkdownText(text: text)
            }
        }
        .task(id: hash) { text = await model.text(hash) }
    }
}

/// A unified diff: hunk headers, added lines green, removed lines red.
struct MemoryDiffPane: View {
    let model: MemoryModel
    let version: MemoryVersion
    let previous: MemoryVersion?
    @State private var hunks: [LineDiff.Hunk] = []
    @State private var loaded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if loaded && hunks.isEmpty {
                Text("no changes").foregroundStyle(.secondary)
            }
            ForEach(Array(hunks.enumerated()), id: \.offset) { _, h in
                VStack(alignment: .leading, spacing: 0) {
                    Text(h.header).foregroundStyle(.cyan).padding(.bottom, 2)
                    ForEach(Array(h.lines.enumerated()), id: \.offset) { _, l in
                        HStack(alignment: .top, spacing: 6) {
                            Text(l.op == .added ? "+" : l.op == .removed ? "-" : " ")
                            Text(l.text).frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .foregroundStyle(l.op == .added ? Theme.added : l.op == .removed ? Theme.removed : Color.primary)
                        .background((l.op == .added ? Theme.added : l.op == .removed ? Theme.removed : Color.clear).opacity(0.10))
                    }
                }
            }
        }
        .font(.system(size: 12, design: .monospaced))
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
        .task(id: "\(version.id)|\(previous?.id ?? "")") {
            hunks = await model.diff(version, previous)
            loaded = true
        }
    }
}
