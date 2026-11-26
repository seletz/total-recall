import SwiftUI
import TotalRecallCore

/// The search view: input with filters, type toggles, hits grouped by
/// type on the left, the preview of the selected hit on the right.
struct SearchView: View {
    let app: AppState
    @Bindable var model: SearchModel
    @FocusState private var inputFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            inputBar
            Divider()
            HSplitView {
                hitList
                    .frame(minWidth: 380, idealWidth: 480)
                PreviewPane(app: app, preview: model.preview)
                    .frame(minWidth: 380, maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .onKeyPress(phases: .down) { press in handle(press) }
    }

    var inputBar: some View {
        VStack(spacing: 8) {
            HStack(spacing: 10) {
                Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                TextField("Search events, sessions and memory", text: $model.query)
                    .textFieldStyle(.plain)
                    .font(.title3)
                    .focused($inputFocused)
                    .onSubmit { Task { await model.search() } }
                if model.filters.attached != nil {
                    Label("with attachment", systemImage: "paperclip")
                        .font(.callout).padding(.horizontal, 6).padding(.vertical, 2)
                        .background(Capsule().fill(Color.accentColor.opacity(0.15)))
                }
                if model.filters.raw { Text("raw FTS").font(.callout.monospaced()).foregroundStyle(.orange) }
                FilterLabel(label: "actor", value: model.filters.actor ?? "all", key: "a")
                FilterLabel(label: "since", value: model.filters.since ?? "all", key: "t")
                FilterLabel(label: "repo", value: model.filters.repo ?? "*", key: "r")
            }
            HStack(spacing: 6) {
                ForEach(SearchModel.allTypes, id: \.self) { t in
                    ChipToggle(title: t, on: model.filters.types.contains(t), key: String(t.first!)) {
                        model.toggleType(t)
                        Task { await model.search() }
                    }
                }
                Spacer()
                HStack(spacing: 4) {
                    Image(systemName: model.filters.project == nil ? "pin" : "pin.fill")
                    Text("project:").foregroundStyle(.secondary)
                    if let p = model.filters.project {
                        Swatch(hex: app.projectColors[p])
                        Text(p).fontWeight(.semibold)
                    } else {
                        Text("all")
                    }
                    KeyCap(key: "p")
                }
                .font(.callout)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
    }

    var hitList: some View {
        VStack(spacing: 0) {
            ScrollViewReader { proxy in
                List(selection: Binding(get: { model.selection }, set: { id in
                    model.selection = id
                    Task { await model.loadPreview() }
                })) {
                    ForEach(model.groups, id: \.type) { g in
                        if model.filters.types.count > 1 {
                            Section {
                                ForEach(g.hits) { hit in HitRow(app: app, hit: hit).tag(hit.id).id(hit.id) }
                            } header: {
                                HStack {
                                    Text(g.type).font(.headline)
                                    Text("\(g.hits.count)").foregroundStyle(.secondary)
                                }
                            }
                        } else {
                            ForEach(g.hits) { hit in HitRow(app: app, hit: hit).tag(hit.id).id(hit.id) }
                        }
                    }
                }
                .listStyle(.inset)
                .onChange(of: model.selection) { _, id in if let id { proxy.scrollTo(id) } }
            }
            Divider()
            HStack {
                if let e = model.error {
                    Label(e, systemImage: "exclamationmark.triangle").foregroundStyle(.red)
                } else {
                    Text(model.summary).foregroundStyle(.secondary)
                }
                Spacer()
            }
            .font(.caption)
            .padding(.horizontal, 10).padding(.vertical, 5)
        }
    }

    func handle(_ press: KeyPress) -> KeyPress.Result {
        if inputFocused {
            if press.key == .tab { inputFocused = false; return .handled }
            if press.key == "r" && press.modifiers == .control { model.filters.raw.toggle(); Task { await model.search() }; return .handled }
            return .ignored
        }
        let run = { _ = Task { await model.search() } }
        switch press.characters {
        case "e": model.toggleType("event"); run()
        case "s": model.toggleType("session"); run()
        case "m": model.toggleType("memory"); run()
        case "a": model.cycleActor(); run()
        case "t": model.cycleSince(); run()
        case "i": model.toggleAttached(); run()
        case "p": model.togglePin(); run()
        case "v": Task { await app.openAttachments() }
        case "j": Task { await model.move(1) }
        case "k": Task { await model.move(-1) }
        default:
            if press.key == .return { Task { await app.openSelectedHit() }; return .handled }
            if press.key == .tab { inputFocused = true; return .handled }
            return .ignored
        }
        return .handled
    }
}

/// One hit: time, title, actor, attachment marker and the snippet.
struct HitRow: View {
    let app: AppState
    let hit: SearchHit

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(app.clock.minute(hit.ts)).font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                if let p = hit.project { Swatch(hex: app.projectColors[p], size: 8) }
                Text(title).fontWeight(.semibold).lineLimit(1)
                Spacer(minLength: 4)
                if let n = hit.attachmentCount, n > 0 {
                    Label("\(n)", systemImage: "paperclip").font(.caption).foregroundStyle(.tint)
                }
                if let a = hit.actor {
                    Text(hit.kind == "message" || hit.kind == nil ? a : "\(a) · \(hit.kind!)")
                        .font(.caption).foregroundStyle(Theme.actor(a))
                }
            }
            SnippetText(snippet: hit.snippet).lineLimit(2).font(.callout)
        }
        .padding(.vertical, 3)
    }

    var title: String {
        switch hit.type {
        case "memory": return "\(hit.project ?? "")  \(hit.path ?? "")"
        default: return hit.sessionName ?? hit.sessionId ?? ""
        }
    }
}

/// The right pane of the search view.
struct PreviewPane: View {
    let app: AppState
    let preview: SearchModel.Preview?

    var body: some View {
        switch preview {
        case .events(let s, let events, let hit)?:
            VStack(alignment: .leading, spacing: 0) {
                SessionHeader(app: app, session: s).padding(12)
                Divider()
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 10) {
                        ForEach(events) { e in EventView(app: app, event: e, isHit: e.id == hit) }
                    }
                    .padding(12)
                }
            }
        case .session(let s, let events)?:
            VStack(alignment: .leading, spacing: 0) {
                SessionHeader(app: app, session: s).padding(12)
                Divider()
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 10) {
                        ForEach(events) { e in EventView(app: app, event: e, isHit: false) }
                    }
                    .padding(12)
                }
            }
        case .memory(_, let path, let project, let text)?:
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Swatch(hex: app.projectColors[project])
                    Text(project).font(.headline)
                    Text(path).font(.headline.monospaced()).foregroundStyle(.secondary)
                }
                .padding(12)
                Divider()
                ScrollView { MarkdownText(text: text).padding(12) }
            }
        case nil:
            ContentUnavailableView("No hit selected", systemImage: "text.magnifyingglass")
        }
    }
}

/// Session header: title, id, repo, branch, time, counts, continuations.
struct SessionHeader: View {
    let app: AppState
    let session: SessionInfo
    var counts = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(session.displayName ?? "(untitled)").font(.title3.bold())
                Spacer()
                Text(session.id).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
            HStack(spacing: 12) {
                if let r = session.repo { Label(Fmt.repoShort(r), systemImage: "shippingbox") }
                if let b = session.gitBranch { Label(b, systemImage: "arrow.triangle.branch") }
                if let s = session.startedAt, let e = session.endedAt {
                    Label("\(app.clock.minute(s)) – \(app.clock.time(e)) (\(Fmt.age(e.timeIntervalSince(s))))", systemImage: "clock")
                }
                if counts {
                    Text("\(session.humanEvents ?? 0) human").foregroundStyle(Theme.actor("human"))
                    Text("\(session.agentEvents ?? 0) agent").foregroundStyle(Theme.actor("agent"))
                }
            }
            .font(.callout)
            .foregroundStyle(.secondary)
            if let f = session.continuedFrom { Text("continued from \(f)").font(.caption.monospaced()).foregroundStyle(.secondary) }
            if let i = session.continuedIn { Text("continued in \(i)").font(.caption.monospaced()).foregroundStyle(.secondary) }
        }
    }
}

/// One event: header line (time, actor or tool) and the text; tool
/// results with JSON are pretty printed.
struct EventView: View {
    let app: AppState
    let event: SessionEvent
    let isHit: Bool
    var showDate = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(showDate ? app.clock.format(event.ts, "yyyy-MM-dd HH:mm:ss") : app.clock.format(event.ts, event.isTool ? "HH:mm:ss" : "HH:mm"))
                    .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                Text(label).font(.caption.bold()).foregroundStyle(Theme.actor(event.actor))
                if !event.attachments.isEmpty {
                    Label("\(event.attachments.count)", systemImage: "paperclip").font(.caption).foregroundStyle(.tint)
                }
                if isHit { Image(systemName: "arrowtriangle.left.fill").font(.caption2).foregroundStyle(.yellow) }
            }
            Text(body(of: event))
                .font(event.isTool ? .system(.callout, design: .monospaced) : .body)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
            ForEach(event.attachments, id: \.sha256) { a in
                Label("\(a.mediaType) \(Fmt.bytes(a.bytes))  \(a.shortHash)", systemImage: a.isImage ? "photo" : "doc")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(8)
        .background(RoundedRectangle(cornerRadius: 6).fill(isHit ? Theme.hit : (event.isTool ? Color.secondary.opacity(0.07) : Color.clear)))
    }

    var label: String {
        switch event.kind {
        case "tool_use": "tool_use \(event.toolName ?? "")"
        case "tool_result": "tool (tool_result)"
        default: event.actor ?? event.role ?? event.kind
        }
    }

    func body(of e: SessionEvent) -> String {
        let t = e.text ?? ""
        if e.kind == "tool_result", let d = t.data(using: .utf8),
           let obj = try? JSONSerialization.jsonObject(with: d),
           let pretty = try? JSONSerialization.data(withJSONObject: obj, options: [.prettyPrinted, .withoutEscapingSlashes]) {
            return String(decoding: pretty, as: UTF8.self)
        }
        return t
    }
}

/// Markdown as readable text: headings, lists and inline styles.
struct MarkdownText: View {
    let text: String
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(text.components(separatedBy: "\n").enumerated()), id: \.offset) { _, line in
                row(line)
            }
        }
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder func row(_ line: String) -> some View {
        if line.hasPrefix("# ") {
            Text(inline(String(line.dropFirst(2)))).font(.title2.bold())
        } else if line.hasPrefix("## ") {
            Text(inline(String(line.dropFirst(3)))).font(.title3.bold())
        } else if line.hasPrefix("- ") {
            HStack(alignment: .firstTextBaseline, spacing: 6) { Text("•"); Text(inline(String(line.dropFirst(2)))) }
        } else if line == "---" {
            Divider()
        } else if line.isEmpty {
            Spacer().frame(height: 2)
        } else {
            Text(inline(line))
        }
    }

    func inline(_ s: String) -> AttributedString {
        (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    }
}
