import Foundation
import Observation

/// Reads files of the user's setup (config, Claude Code settings). A
/// closure, so the tests feed fixtures.
public typealias FileReader = @Sendable (String) -> String?

/// The config view with its five pages (TUI view config).
@MainActor @Observable
public final class ConfigModel {
    public enum Page: String, CaseIterable, Sendable { case status, settings, projects, actions, setup }

    public struct Row: Hashable, Sendable {
        public let label: String
        public let value: String
        public let note: String
        public init(_ label: String, _ value: String, _ note: String = "") {
            self.label = label; self.value = value; self.note = note
        }
    }

    public struct Check: Hashable, Sendable, Identifiable {
        public let name: String
        public let ok: Bool
        public let detail: String
        public var id: String { name }
    }

    public struct Action: Hashable, Sendable, Identifiable {
        public let name: String
        public let description: String
        public let arguments: [String]
        public var id: String { name }
    }

    public let archive: Archive
    public let clock: Clock
    public let home: String
    public let readFile: FileReader
    public var page: Page = .status
    public private(set) var status: ArchiveStatus?
    public private(set) var config: ConfigShow?
    public private(set) var definitions: [ProjectDefinition] = []
    public private(set) var keys: [ProjectKey] = []
    public private(set) var checks: [Check] = []
    public private(set) var hookBlock = ""
    public private(set) var mcpBlock = ""
    public private(set) var cliVersion = ""
    public private(set) var error: String?
    /// Output of the last action.
    public private(set) var actionOutput: String?
    public private(set) var actionRunning = false

    public init(archive: Archive, clock: Clock, home: String, readFile: @escaping FileReader = { try? String(contentsOfFile: $0, encoding: .utf8) }) {
        self.archive = archive
        self.clock = clock
        self.home = home
        self.readFile = readFile
    }

    public func tilde(_ p: String?) -> String { Fmt.tilde(p, home: home) }

    public func load() async {
        do {
            async let st = archive.status()
            async let cf = archive.config()
            async let ss = archive.sessions()
            async let hb = archive.hookBlock()
            async let v = archive.version()
            status = try await st
            config = try await cf
            keys = ProjectKey.from(try await ss)
            hookBlock = (try? await hb) ?? ""
            cliVersion = (try? await v) ?? ""
            if let file = config?.file, let toml = readFile(file) {
                definitions = ProjectDefinition.parse(toml: toml)
            }
            checks = setupChecks()
            error = nil
        } catch {
            self.error = "\(error)"
        }
    }

    // MARK: - status

    public var statusRows: [Row] {
        guard let s = status else { return [] }
        let pages = s.pageSize > 0 ? s.dbSizeBytes / s.pageSize : 0
        var rows = [
            Row("archive", tilde(s.db)),
            Row("size", Fmt.bytes(s.dbSizeBytes), "\(pages) pages"),
            Row("page size", "\(s.pageSize)", s.pageSize < 16384 ? "maintenance rebuilds with 16384 and saves space" : ""),
            Row("schema", "v\(s.schemaVersion), binary v\(s.binarySchemaVersion)", s.migrationPending ? "migration pending" : ""),
            Row("raw", "\(Fmt.bytes(s.rawBytes)) in \(s.rawRows) events", String(format: "ratio %.1f", s.rawRatio)),
            Row("blobs", "\(s.blobs) blobs, \(Fmt.bytes(s.blobBytes))"),
            Row("memory", "\(s.memoryFiles) files in \(s.memoryProjects) projects, \(s.memoryVersions) versions"),
            Row("meta records", s.metaRows == 0 ? "none" : "\(s.metaRows)"),
            Row("sessions", "\(s.sessions)"),
            Row("events", "\(s.events)"),
            Row("sources", "\(s.sources)"),
        ]
        for (h, t) in s.lastIngest.sorted(by: { $0.key < $1.key }) {
            rows.append(Row("last ingest (\(h))", clock.minute(t), clock.ago(t)))
        }
        rows.append(Row("last backup", tilde(s.lastBackup), s.lastBackupAt.map { clock.ago($0) } ?? ""))
        rows.append(Row("log", tilde(s.log)))
        if let c = config {
            rows.append(Row("config", tilde(c.file), c.loaded ? "loaded" : "not found, defaults"))
            rows.append(Row("problems", c.problems.isEmpty ? "none" : c.problems.joined(separator: "; ")))
        }
        rows.append(Row("errors", s.recentErrorsTotal == 0 ? "none in the last 24h" : "\(s.recentErrorsTotal) in the last 24h", s.olderErrors > 0 ? "\(s.olderErrors) older" : ""))
        return rows
    }

    // MARK: - settings

    /// The values grouped by table in the order of `config show`, without
    /// the lists the TUI edits elsewhere (project, keys).
    public var settingGroups: [(table: String, values: [ConfigShow.Value])] {
        var out: [(String, [ConfigShow.Value])] = []
        for v in config?.values ?? [] where v.key != "project" && !v.key.hasPrefix("keys.") {
            if let i = out.firstIndex(where: { $0.0 == v.table }) { out[i].1.append(v) } else { out.append((v.table, [v])) }
        }
        return out
    }

    /// GAP: the CLI has no command that writes a key (`config.SetValue` of
    /// the TUI). The draft cannot be saved through the stable interface.
    public func save(key: String, value: String) async throws {
        throw CLIError(kind: "unsupported", message: "the CLI cannot write \(key) yet (needs `total-recall config set KEY VALUE`)")
    }

    // MARK: - projects

    public var resolver: ProjectResolver { ProjectResolver(definitions: definitions, home: home) }

    public struct ProjectRow: Identifiable, Sendable {
        public let definition: ProjectDefinition
        public let keys: [ProjectKey]
        public var id: String { definition.name }
        public var sessions: Int { keys.reduce(0) { $0 + $1.sessions } }
    }

    public var projectRows: [ProjectRow] {
        let a = resolver.assign(keys)
        return definitions.map { ProjectRow(definition: $0, keys: a.matched[$0.name] ?? []) }
    }

    public var unmatched: [ProjectKey] { resolver.assign(keys).unmatched }

    /// "matches 1 path · 326 sessions: kompass".
    public func previewLine(_ draft: ProjectDefinition, at index: Int) -> String {
        let m = resolver.preview(draft, at: index, keys: keys)
        let names = m.map { $0.derivedName(home: home) }.joined(separator: ", ")
        let n = m.reduce(0) { $0 + $1.sessions }
        return "matches \(m.count) path\(m.count == 1 ? "" : "s") · \(n) sessions\(names.isEmpty ? "" : ": " + names)"
    }

    /// The pattern a path offers for a new definition: its remote URL, else its path with "~".
    public func pattern(for key: ProjectKey) -> String {
        if let r = key.remote, !r.isEmpty { return r }
        return Fmt.tilde(key.key, home: home)
    }

    // MARK: - actions

    public var actions: [Action] {
        let dir = tilde(status?.backupDir)
        let v = status?.schemaVersion ?? 0
        return [
            Action(name: "backup", description: "write a compressed copy to \(dir), keep 7 daily and all monthly backups", arguments: ["backup"]),
            Action(name: "maintenance (dry run)", description: "measure a rebuild with 16384-byte pages; changes nothing", arguments: ["maintenance", "--dry-run"]),
            Action(name: "maintenance", description: "rebuild the archive with 16384-byte pages after a backup; hooks skip meanwhile", arguments: ["maintenance"]),
            Action(name: "migrate", description: status?.migrationPending == true ? "schema v\(v) → v\(status!.binarySchemaVersion): back up, then migrate" : "schema v\(v) is current · nothing pending", arguments: ["migrate", "--yes"]),
            Action(name: "reindex actors (dry run)", description: "count the stored user messages whose actor the current rules would change", arguments: ["reindex", "--actors", "--dry-run"]),
            Action(name: "reindex actors", description: "apply the current actor rules to every stored user message", arguments: ["reindex", "--actors"]),
            Action(name: "ingest all", description: "read every transcript of every harness from the stored offset", arguments: ["ingest", "--all"]),
            Action(name: "doctor", description: "check the transcripts for format drift; reads only", arguments: ["doctor"]),
        ]
    }

    public func run(_ action: Action) async {
        actionRunning = true
        defer { actionRunning = false }
        do {
            actionOutput = try await archive.runAction(action.arguments)
        } catch {
            actionOutput = "\(error)"
        }
        await load()
    }

    // MARK: - setup

    /// The checks of the setup page, from the files of the home directory.
    /// GAP: the TUI gets them from internal/tui/configview; the CLI has no
    /// `setup --json`, so the app reads the files itself.
    public func setupChecks() -> [Check] {
        var out: [Check] = []
        let cfg = config
        out.append(Check(name: "config", ok: cfg?.loaded ?? false, detail: "\(tilde(cfg?.file)) \(cfg?.loaded == true ? "loaded" : "missing, defaults")"))

        let settingsPath = home + "/.claude/settings.json"
        var hookEvents: [String] = []
        if let s = readFile(settingsPath), let obj = try? JSONSerialization.jsonObject(with: Data(s.utf8)) as? [String: Any],
           let hooks = obj["hooks"] as? [String: Any] {
            hookEvents = hooks.filter { (_, v) in "\(v)".contains("total-recall") }.map(\.key)
        }
        let order = ["SessionStart", "Stop", "SubagentStop", "SessionEnd"]
        hookEvents.sort { (order.firstIndex(of: $0) ?? 9) < (order.firstIndex(of: $1) ?? 9) }
        out.append(Check(name: "hooks", ok: !hookEvents.isEmpty, detail: "\(tilde(settingsPath)): \(hookEvents.isEmpty ? "no hook block" : hookEvents.joined(separator: ", "))"))

        let claudeJSON = home + "/.claude.json"
        var mcp = ""
        if let s = readFile(claudeJSON), let obj = try? JSONSerialization.jsonObject(with: Data(s.utf8)) as? [String: Any],
           let servers = obj["mcpServers"] as? [String: Any], let tr = servers["total-recall"] as? [String: Any] {
            mcp = "total-recall " + ((tr["args"] as? [String]) ?? []).joined(separator: " ")
            if let d = try? JSONSerialization.data(withJSONObject: ["mcpServers": ["total-recall": tr]], options: [.prettyPrinted, .sortedKeys]) {
                mcpBlock = String(decoding: d, as: UTF8.self)
            }
        }
        out.append(Check(name: "mcp", ok: !mcp.isEmpty, detail: "\(tilde(claudeJSON)): \(mcp.isEmpty ? "no total-recall server" : mcp)"))

        out.append(Check(name: "binary", ok: !cliVersion.isEmpty, detail: cliVersion.isEmpty ? "total-recall not found" : "total-recall on PATH, \(cliVersion)"))
        if let s = status {
            out.append(Check(name: "backups", ok: s.lastBackup != nil, detail: "\(tilde(s.backupDir)) \(s.lastBackupAt.map { "last backup \(clock.ago($0))" } ?? "no backup yet")"))
        }
        out.append(Check(name: "transcripts", ok: true, detail: tilde(cfg?.value("harness.claude.projects_dir"))))
        out.append(Check(name: "archive", ok: status != nil, detail: tilde(status?.db)))
        return out
    }
}
