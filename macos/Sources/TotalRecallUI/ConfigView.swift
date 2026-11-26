import AppKit
import SwiftUI
import TotalRecallCore

/// The config view with its five pages (TUI view config).
struct ConfigView: View {
    let app: AppState
    @Bindable var model: ConfigModel

    var ui: ConfigUIState { .shared }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Picker("", selection: $model.page) {
                    ForEach(ConfigModel.Page.allCases, id: \.self) { Text($0.rawValue.capitalized).tag($0) }
                }
                .pickerStyle(.segmented).labelsHidden().fixedSize()
                Spacer()
                Image(systemName: "doc.text").foregroundStyle(.secondary)
                Text(model.tilde(model.config?.file)).font(.callout.monospaced()).foregroundStyle(.secondary)
            }
            .padding(.horizontal, 12).padding(.vertical, 8)
            Divider()
            if let e = model.error { Text(e).foregroundStyle(.red).padding(8) }
            switch model.page {
            case .status: ConfigStatusPage(model: model)
            case .settings: ConfigSettingsPage(model: model, ui: ui)
            case .projects: ConfigProjectsPage(app: app, model: model, ui: ui)
            case .actions: ConfigActionsPage(model: model, ui: ui)
            case .setup: ConfigSetupPage(model: model, ui: ui)
            }
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(.tab, phases: .down) { p in
            let all = ConfigModel.Page.allCases
            let i = all.firstIndex(of: model.page)!
            model.page = all[(i + (p.modifiers.contains(.shift) ? all.count - 1 : 1)) % all.count]
            return .handled
        }
    }
}

/// A selectable row of the config tables.
struct ConfigRow<Content: View>: View {
    let selected: Bool
    let action: () -> Void
    var onDouble: (() -> Void)?
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(.horizontal, 12).padding(.vertical, 4)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(selected ? Color.accentColor.opacity(0.18) : Color.clear)
            .contentShape(Rectangle())
            .onTapGesture(count: 2) { action(); onDouble?() }
            .onTapGesture(perform: action)
    }
}

/// A column header line of a table.
struct ConfigHeader: View {
    let columns: [(String, CGFloat?)]
    var body: some View {
        HStack(spacing: 12) {
            ForEach(Array(columns.enumerated()), id: \.offset) { _, c in
                Text(c.0).frame(width: c.1, alignment: .leading).frame(maxWidth: c.1 == nil ? .infinity : nil, alignment: .leading)
            }
        }
        .font(.caption.weight(.semibold)).foregroundStyle(.secondary)
        .padding(.horizontal, 12).padding(.vertical, 4)
        .background(Color.secondary.opacity(0.08))
    }
}

/// An inline known-gap note.
struct ConfigGap: View {
    let text: String
    var body: some View {
        Label(text, systemImage: "exclamationmark.triangle").foregroundStyle(.orange).font(.callout)
    }
}

// MARK: - status

struct ConfigStatusPage: View {
    let model: ConfigModel
    var body: some View {
        ScrollView {
            Grid(alignment: .topLeading, horizontalSpacing: 20, verticalSpacing: 6) {
                ForEach(model.statusRows, id: \.self) { r in
                    GridRow {
                        Text(r.label).foregroundStyle(.secondary)
                        Text(r.value).textSelection(.enabled)
                        Text(r.note).foregroundStyle(r.note.contains("pending") || r.note.contains("saves") ? Color.orange : .secondary)
                    }
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

// MARK: - settings

struct ConfigSettingsPage: View {
    let model: ConfigModel
    @Bindable var ui: ConfigUIState

    func quoted(_ v: ConfigShow.Value) -> String { v.value.map { model.tilde($0) } ?? "\"\"" }

    var body: some View {
        VStack(spacing: 0) {
            ConfigHeader(columns: [("KEY", 260), ("VALUE", nil), ("ORIGIN", 110)])
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(model.settingGroups, id: \.table) { g in
                        Text(g.table.isEmpty ? "(top level)" : "[\(g.table)]")
                            .font(.callout.weight(.semibold)).foregroundStyle(Color.accentColor)
                            .padding(.horizontal, 12).padding(.top, 10).padding(.bottom, 2)
                        ForEach(g.values) { v in
                            ConfigRow(selected: ui.selectedSetting == v.key || ui.editingKey == v.key, action: { ui.selectedSetting = v.key }, onDouble: { ui.editingKey = v.key }) {
                                HStack(spacing: 12) {
                                    Text(v.name).frame(width: 260, alignment: .leading)
                                    Text(quoted(v)).font(.body.monospaced()).lineLimit(1).truncationMode(.middle)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                    Text(v.origin).foregroundStyle(.secondary).frame(width: 110, alignment: .leading)
                                }
                            }
                        }
                    }
                }
            }
            if let key = ui.editingKey, let v = model.config?.values.first(where: { $0.key == key }) {
                Divider()
                SettingEditor(model: model, ui: ui, value: v)
            }
        }
        .onKeyPress(phases: .down) { p in
            let keys = model.settingGroups.flatMap(\.values).map(\.key)
            let i = keys.firstIndex(of: ui.selectedSetting ?? "") ?? -1
            switch p.key {
            case .downArrow: ui.selectedSetting = keys[min(i + 1, keys.count - 1)]; return .handled
            case .upArrow: ui.selectedSetting = keys[max(i - 1, 0)]; return .handled
            case .return: if i >= 0 { ui.editingKey = keys[i] }; return .handled
            case .escape:
                if ui.editingKey != nil { ui.editingKey = nil; return .handled }
                return .ignored
            default: return .ignored
            }
        }
    }
}

struct SettingEditor: View {
    let model: ConfigModel
    @Bindable var ui: ConfigUIState
    let value: ConfigShow.Value
    @State private var text = ""
    @State private var failure: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Edit").foregroundStyle(.secondary)
                Text(value.key).font(.headline.monospaced())
                Text("origin: \(value.origin)").font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button { ui.editingKey = nil } label: { Image(systemName: "xmark") }.buttonStyle(.borderless)
            }
            HStack {
                TextField("value", text: $text).textFieldStyle(.roundedBorder).font(.body.monospaced())
                Button("Save") {
                    Task {
                        do { try await model.save(key: value.key, value: text); failure = nil; ui.editingKey = nil } catch {
                            failure = (error as? CLIError)?.message ?? "\(error)"
                        }
                    }
                }
                .keyboardShortcut(.defaultAction)
                Button("Open config in editor") {
                    if let f = model.config?.file { NSWorkspace.shared.open(URL(fileURLWithPath: f)) }
                }
            }
            if let f = failure {
                ConfigGap(text: "Known gap: \(f)")
            } else {
                ConfigGap(text: "Known gap: saving needs `total-recall config set KEY VALUE`, which the CLI does not offer yet.")
            }
        }
        .padding(12)
        .background(.bar)
        .task(id: value.key) { text = value.value.map { model.tilde($0) } ?? ""; failure = nil }
    }
}

// MARK: - projects

struct ConfigProjectsPage: View {
    let app: AppState
    let model: ConfigModel
    @Bindable var ui: ConfigUIState
    @State private var selected = 0
    @State private var unmatchedSelected: String?
    @State private var note: String?

    static let gap = "Known gap: writing [[project]] needs a CLI command (`total-recall config project …`)."

    var formIndex: Int? { ui.newProject != nil ? model.definitions.count : ui.editingProject }

    var body: some View {
        VStack(spacing: 0) {
            toolbar
            Divider()
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ConfigHeader(columns: [("", 14), ("PROJECT", 140), ("DESCRIPTION", nil), ("SESSIONS", 70), ("MATCH", 320)])
                    ForEach(Array(model.projectRows.enumerated()), id: \.offset) { i, r in
                        ConfigRow(selected: selected == i, action: { selected = i }, onDouble: { ui.newProject = nil; ui.editingProject = i }) {
                            HStack(spacing: 12) {
                                Swatch(hex: r.definition.hex).frame(width: 14)
                                Text(r.definition.name).fontWeight(.medium).frame(width: 140, alignment: .leading)
                                Text(r.definition.description).foregroundStyle(.secondary).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
                                Text("\(r.sessions)").monospacedDigit().frame(width: 70, alignment: .leading)
                                Text(r.definition.match.joined(separator: ", ")).font(.callout.monospaced()).lineLimit(1).frame(width: 320, alignment: .leading)
                            }
                        }
                    }
                    if model.definitions.indices.contains(selected) {
                        let d = model.definitions[selected]
                        HStack(spacing: 8) {
                            Text(d.name).fontWeight(.semibold)
                            Text(d.hex ?? "no colour").font(.callout.monospaced()).foregroundStyle(.secondary)
                            Text(model.previewLine(d, at: selected)).foregroundStyle(.secondary)
                        }
                        .font(.callout).padding(.horizontal, 12).padding(.vertical, 8)
                    }
                    Text("Unmatched").font(.callout.weight(.semibold)).foregroundStyle(Color.accentColor)
                        .padding(.horizontal, 12).padding(.top, 8).padding(.bottom, 4)
                    ConfigHeader(columns: [("PATH", 220), ("SESSIONS", 70), ("LAST", 110), ("DIRECTORY", nil), ("REMOTE", 240)])
                    ForEach(model.unmatched) { k in
                        ConfigRow(selected: unmatchedSelected == k.key, action: { unmatchedSelected = k.key }) {
                            HStack(spacing: 12) {
                                Text(k.derivedName(home: model.home)).frame(width: 220, alignment: .leading).lineLimit(1)
                                Text("\(k.sessions)").monospacedDigit().frame(width: 70, alignment: .leading)
                                Text(k.last.map { model.clock.ago($0) } ?? "").foregroundStyle(.secondary).frame(width: 110, alignment: .leading)
                                Text(model.tilde(k.key)).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle).frame(maxWidth: .infinity, alignment: .leading)
                                Text(k.remote ?? "").font(.callout.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle).frame(width: 240, alignment: .leading)
                            }
                        }
                    }
                    if let n = note { ConfigGap(text: n).padding(12) }
                }
            }
            if let i = formIndex {
                Divider()
                ProjectForm(model: model, ui: ui, index: i)
                    .id("\(i)-\(ui.newProject?.name ?? "")")
            }
        }
        .onKeyPress(phases: .down) { handle($0) }
    }

    var toolbar: some View {
        HStack(spacing: 8) {
            Button { openNew() } label: { Label("Add", systemImage: "plus") }
            Button { note = Self.gap } label: { Label("Remove", systemImage: "minus") }
            Button { note = Self.gap } label: { Image(systemName: "arrow.up") }.help("Move up (K)")
            Button { note = Self.gap } label: { Image(systemName: "arrow.down") }.help("Move down (J)")
            Button { addPath() } label: { Label("Add path as pattern", systemImage: "arrow.right.circle") }
                .disabled(unmatchedSelected == nil)
            Spacer()
            if formIndex == nil { Text("double-click a project to edit").font(.caption).foregroundStyle(.secondary) }
        }
        .controlSize(.small)
        .padding(.horizontal, 12).padding(.vertical, 6)
    }

    func openNew() {
        ui.editingProject = nil
        ui.newProject = ProjectDefinition(name: "")
    }

    func addPath() {
        guard let k = model.unmatched.first(where: { $0.key == unmatchedSelected }) else { return }
        ui.editingProject = nil
        ui.newProject = ProjectDefinition(name: k.derivedName(home: model.home), match: [model.pattern(for: k)])
    }

    func handle(_ p: KeyPress) -> KeyPress.Result {
        switch p.key {
        case .downArrow: selected = min(selected + 1, max(model.definitions.count - 1, 0)); return .handled
        case .upArrow: selected = max(selected - 1, 0); return .handled
        case .return: if model.definitions.indices.contains(selected) { ui.newProject = nil; ui.editingProject = selected }; return .handled
        case .escape:
            if formIndex != nil { ui.editingProject = nil; ui.newProject = nil; return .handled }
            return .ignored
        default: break
        }
        switch p.characters {
        case "a": openNew(); return .handled
        case "x", "K", "J": note = Self.gap; return .handled
        case "m": addPath(); return .handled
        default: return .ignored
        }
    }
}

/// The form of a definition: fields, colour, patterns, live preview.
struct ProjectForm: View {
    let model: ConfigModel
    @Bindable var ui: ConfigUIState
    let index: Int
    @State private var draft = ProjectDefinition(name: "")
    @State private var custom = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(ui.newProject != nil ? "New project" : "Edit project").font(.headline)
                Spacer()
                Button("Cancel") { ui.editingProject = nil; ui.newProject = nil }
                Button("Save") {}.disabled(true)
            }
            HStack(alignment: .top, spacing: 20) {
                VStack(alignment: .leading, spacing: 8) {
                    labelled("name") { TextField("name", text: $draft.name).textFieldStyle(.roundedBorder) }
                    labelled("description") { TextField("description", text: $draft.description).textFieldStyle(.roundedBorder) }
                    labelled("colour") { colours }
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text("match patterns").foregroundStyle(.secondary).font(.callout)
                    ForEach(draft.match.indices, id: \.self) { i in
                        HStack {
                            TextField("pattern", text: $draft.match[i]).textFieldStyle(.roundedBorder).font(.callout.monospaced())
                            Button { draft.match.remove(at: i) } label: { Image(systemName: "minus.circle") }.buttonStyle(.borderless)
                        }
                    }
                    Button("+ pattern") { draft.match.append("") }.buttonStyle(.borderless)
                }
                .frame(maxWidth: 360)
            }
            HStack(spacing: 6) {
                Image(systemName: "eye").foregroundStyle(.secondary)
                Text(model.previewLine(draft, at: index))
            }
            .font(.callout)
            ConfigGap(text: ConfigProjectsPage.gap)
        }
        .padding(12)
        .background(.bar)
        .onAppear {
            if let n = ui.newProject { draft = n } else if model.definitions.indices.contains(index) { draft = model.definitions[index] }
            custom = draft.hex.flatMap { Palette.name(ofHex: $0) == nil ? $0 : nil } ?? ""
        }
    }

    func labelled<V: View>(_ l: String, @ViewBuilder _ v: () -> V) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(l).foregroundStyle(.secondary).font(.callout).frame(width: 80, alignment: .leading)
            v()
        }
    }

    var colours: some View {
        HStack(spacing: 8) {
            chip(nil, "automatic")
            ForEach(Palette.colors, id: \.hex) { c in chip(c.hex, c.name) }
            TextField("custom #rrggbb", text: $custom)
                .textFieldStyle(.roundedBorder).font(.callout.monospaced()).frame(width: 130)
                .onChange(of: custom) { _, v in if let h = Palette.hex(v) { draft.color = h } }
        }
    }

    func chip(_ hex: String?, _ title: String) -> some View {
        let on = (hex == nil && draft.hex == nil) || (hex != nil && draft.hex == hex)
        return Button { draft.color = hex ?? ""; custom = "" } label: {
            HStack(spacing: 4) {
                if let hex { Swatch(hex: hex) } else { Image(systemName: "wand.and.stars").font(.caption) }
                Text(title).font(.callout)
            }
            .padding(.horizontal, 7).padding(.vertical, 3)
            .background(RoundedRectangle(cornerRadius: 5).fill(on ? Color.accentColor.opacity(0.2) : Color.secondary.opacity(0.08)))
            .overlay(RoundedRectangle(cornerRadius: 5).stroke(on ? Color.accentColor : Color.clear, lineWidth: 1.5))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - actions

struct ConfigActionsPage: View {
    let model: ConfigModel
    @Bindable var ui: ConfigUIState
    @State private var selected = 0

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(model.actions.enumerated()), id: \.offset) { i, a in
                        ConfigRow(selected: selected == i || ui.confirming == a.name, action: { selected = i }, onDouble: { ui.confirming = a.name }) {
                            HStack(spacing: 16) {
                                Text(a.name).fontWeight(.medium).frame(width: 220, alignment: .leading)
                                Text(a.description).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                    }
                    if model.actionRunning {
                        HStack { ProgressView().controlSize(.small); Text("running …").foregroundStyle(.secondary) }.padding(12)
                    }
                    if let out = model.actionOutput {
                        Text("output of the last action").font(.caption).foregroundStyle(.secondary).padding(.horizontal, 12).padding(.top, 10)
                        ScrollView {
                            Text(out).font(.system(size: 12, design: .monospaced)).textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                        }
                        .frame(maxHeight: 260)
                        .background(RoundedRectangle(cornerRadius: 6).fill(Color.secondary.opacity(0.1)))
                        .padding(.horizontal, 12)
                    }
                }
            }
            if let name = ui.confirming, let a = model.actions.first(where: { $0.name == name }) {
                Divider()
                HStack(spacing: 12) {
                    Image(systemName: "questionmark.circle").foregroundStyle(.orange).font(.title3)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("\(a.name): runs `total-recall \(a.arguments.joined(separator: " "))`").fontWeight(.medium)
                        Text(a.description).font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Cancel") { ui.confirming = nil }
                    Button("Confirm (y)") { run(a) }.keyboardShortcut(.defaultAction)
                }
                .padding(12).background(.bar)
            }
        }
        .onKeyPress(phases: .down) { p in
            let acts = model.actions
            switch p.key {
            case .downArrow: selected = min(selected + 1, acts.count - 1); return .handled
            case .upArrow: selected = max(selected - 1, 0); return .handled
            case .return: ui.confirming = acts[selected].name; return .handled
            case .escape:
                if ui.confirming != nil { ui.confirming = nil; return .handled }
                return .ignored
            default: break
            }
            if p.characters == "y", let name = ui.confirming, let a = acts.first(where: { $0.name == name }) { run(a); return .handled }
            return .ignored
        }
    }

    func run(_ a: ConfigModel.Action) {
        ui.confirming = nil
        Task { await model.run(a) }
    }
}

// MARK: - setup

struct ConfigSetupPage: View {
    let model: ConfigModel
    @Bindable var ui: ConfigUIState

    var block: (title: String, text: String) {
        ui.setupSelection == "mcp"
            ? ("MCP entry for ~/.claude.json", model.mcpBlock)
            : ("Hook block for ~/.claude/settings.json", model.hookBlock)
    }

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(model.checks) { c in
                    ConfigRow(selected: ui.setupSelection == c.name, action: { ui.setupSelection = c.name }) {
                        HStack(spacing: 12) {
                            Image(systemName: c.ok ? "checkmark.circle.fill" : "xmark.circle.fill").foregroundStyle(c.ok ? Color.green : .red)
                            Text(c.name).fontWeight(.medium).frame(width: 100, alignment: .leading)
                            Text(c.detail).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                        }
                    }
                }
            }
            .padding(.vertical, 6)
            Divider()
            HStack {
                Text(block.title).font(.callout.weight(.semibold))
                Spacer()
                Button { copy(block.text) } label: { Label("Copy", systemImage: "doc.on.doc") }.disabled(block.text.isEmpty)
            }
            .padding(.horizontal, 12).padding(.vertical, 6)
            ScrollView {
                Text(block.text.isEmpty ? "(nothing to show)" : block.text)
                    .font(.system(size: 12, design: .monospaced)).textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(10)
            }
            .background(RoundedRectangle(cornerRadius: 6).fill(Color.secondary.opacity(0.1)))
            .padding(.horizontal, 12).padding(.bottom, 12)
        }
        .onKeyPress(phases: .down) { p in
            let names = model.checks.map(\.name)
            let i = names.firstIndex(of: ui.setupSelection) ?? 0
            switch p.key {
            case .downArrow: ui.setupSelection = names[min(i + 1, names.count - 1)]; return .handled
            case .upArrow: ui.setupSelection = names[max(i - 1, 0)]; return .handled
            default: break
            }
            if p.characters == "c" { copy(block.text); return .handled }
            return .ignored
        }
    }

    func copy(_ s: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
    }
}
