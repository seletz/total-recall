import Foundation

/// Filters of the search view; each maps to one flag of `search`.
public struct SearchFilters: Equatable, Sendable {
    public enum Attached: String, Sendable, CaseIterable { case any, image, document }
    /// Searched types in this order: event, session, memory.
    public var types: [String] = ["event"]
    public var actor: String?
    public var since: String?
    public var repo: String?
    public var project: String?
    public var attached: Attached?
    public var raw = false
    public var limit = 50

    public init() {}
}

/// The typed client of the CLI: one method per read command, each builds
/// the arguments and decodes the `--json` output. Errors are ``CLIError``.
public struct Archive: Sendable {
    public let runner: CommandRunner

    public init(runner: CommandRunner) { self.runner = runner }

    func decode<T: Decodable>(_ type: T.Type, _ args: [String]) async throws -> T {
        let data = try await runner.run(args)
        if let e = CLIError.parse(data) { throw e }
        do {
            return try CLIDecoder.make().decode(T.self, from: data)
        } catch {
            throw CLIError(kind: "decode", message: "\(args.first ?? ""): \(error)")
        }
    }

    // MARK: - search, show

    public static func searchArguments(_ query: String, _ f: SearchFilters) -> [String] {
        var a = ["search"]
        if !query.isEmpty { a.append(query) }
        if let att = f.attached { a.append(att == .any ? "--has-attachment" : "--has-attachment=\(att.rawValue)") }
        if f.types != ["event"] { a += ["--type", f.types.joined(separator: ",")] }
        if let v = f.actor { a += ["--actor", v] }
        if let v = f.since { a += ["--since", v] }
        if let v = f.repo { a += ["--repo", v] }
        if let v = f.project { a += ["--project", v] }
        if f.raw { a.append("--raw") }
        a += ["--limit", String(f.limit), "--json"]
        return a
    }

    public func search(_ query: String, filters: SearchFilters = SearchFilters()) async throws -> [SearchHit] {
        try await decode([SearchHit].self, Self.searchArguments(query, filters))
    }

    public func show(_ session: String) async throws -> SessionDetail {
        try await decode(SessionDetail.self, ["show", session, "--all", "--json"])
    }

    /// Every session (subagents and empty ones included), for the project
    /// keys of the config view.
    public func sessions() async throws -> [SessionInfo] {
        try await decode([SessionInfo].self, ["sessions", "--limit", "0", "--all-sessions", "--json"])
    }

    // MARK: - stats

    public func stats(since: String, until: String) async throws -> StatsSummary {
        try await decode(StatsSummary.self, ["stats", "--since", since, "--until", until, "--json"])
    }

    public func series(since: String, until: String, period: String, by: String) async throws -> StatsSeries {
        try await decode(StatsSeries.self, ["stats", "--since", since, "--until", until, "--period", period, "--by", by, "--json"])
    }

    public func workdays(since: String, until: String, allEvents: Bool) async throws -> Workdays {
        var a = ["stats", "--workday", "--blocks", "--since", since, "--until", until]
        if allEvents { a.append("--all-events") }
        return try await decode(Workdays.self, a + ["--json"])
    }

    public func day(_ day: String, allEvents: Bool) async throws -> DayReport {
        var a = ["stats", "--day", day]
        if allEvents { a.append("--all-events") }
        return try await decode(DayReport.self, a + ["--json"])
    }

    /// The events of a window with their project, for the project colours
    /// of the activity bars. `until` is exclusive.
    public func timeline(since: String, until: String, allEvents: Bool) async throws -> [TimelineEvent] {
        var a = ["timeline", "--since", since, "--until", until]
        if allEvents { a += ["--actor", "all"] }
        return try await decode([TimelineEvent].self, a + ["--fields", "ts,project", "--json"])
    }

    // MARK: - memory, blobs

    public func memory(project: String? = nil) async throws -> [MemoryFile] {
        var a = ["memory", "--deleted"]
        if let p = project { a += ["--project", p] }
        return try await decode([MemoryFile].self, a + ["--json"])
    }

    public func memoryHistory(project: String, path: String) async throws -> [MemoryVersion] {
        try await decode([MemoryVersion].self, ["memory", "--project", project, "--history", path, "--json"])
    }

    public func blobInfo(_ hash: String) async throws -> BlobInfo {
        try await decode(BlobInfo.self, ["blob", hash, "--json"])
    }

    /// The content of a blob (memory text, image, document).
    public func blob(_ hash: String) async throws -> Data {
        try await runner.run(["blob", hash])
    }

    public func text(ofBlob hash: String) async throws -> String {
        String(decoding: try await blob(hash), as: UTF8.self)
    }

    // MARK: - status, config

    public func status() async throws -> ArchiveStatus {
        try await decode(ArchiveStatus.self, ["status", "--json"])
    }

    public func config() async throws -> ConfigShow {
        try await decode(ConfigShow.self, ["config", "show", "--json"])
    }

    public func hookBlock() async throws -> String {
        String(decoding: try await runner.run(["hooks", "print", "--harness", "claude"]), as: UTF8.self)
    }

    public func version() async throws -> String {
        String(decoding: try await runner.run(["version"]), as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Runs a writing command of the actions page and returns its text output.
    public func runAction(_ arguments: [String]) async throws -> String {
        String(decoding: try await runner.run(arguments), as: UTF8.self)
    }
}
