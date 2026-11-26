import Foundation

// SPIKE STOPGAP. The projects page of the config view needs the project
// definitions as data (name, description, colour, patterns), the raw
// project keys of the archive (store.ProjectKeys) and a preview of what a
// draft definition matches. The CLI has none of these as --json yet:
// `config show --json` has [[project]] only as one flattened string, without
// the description. So the app reads the [[project]] tables of the config
// file itself and re-implements the glob matching of internal/project.
// That is a second reader of the config and a copy of the resolver; a real
// product needs `config projects --json` (definitions + keys + matches) and
// writing commands (`config project add|set|rm|move`) instead.

/// The series palette of internal/project (`project.Palette`).
public enum Palette {
    public static let colors: [(name: String, hex: String)] = [
        ("blue", "#4e79a7"), ("orange", "#f28e2b"), ("green", "#59a14f"),
        ("red", "#e15759"), ("purple", "#b07aa1"), ("teal", "#76b7b2"),
    ]

    /// A palette name or #rrggbb → #rrggbb; nil for empty or invalid.
    public static func hex(_ s: String?) -> String? {
        guard let s = s?.trimmingCharacters(in: .whitespaces), !s.isEmpty else { return nil }
        if let c = colors.first(where: { $0.name.caseInsensitiveCompare(s) == .orderedSame }) { return c.hex }
        if s.count == 7, s.hasPrefix("#"), s.dropFirst().allSatisfy(\.isHexDigit) { return s.lowercased() }
        return nil
    }

    public static func name(ofHex hex: String) -> String? { colors.first { $0.hex == hex }?.name }

    /// Colours of a list of series names: configured colours are pinned, the
    /// others take the free palette colours in order (chart.Colors.Fix).
    public static func assign(_ names: [String], pinned: [String: String]) -> [String: String] {
        var out: [String: String] = [:]
        let used = Set(pinned.values)
        var free = colors.map(\.hex).filter { !used.contains($0) }
        if free.isEmpty { free = colors.map(\.hex) }
        var i = 0
        for n in names {
            if let p = pinned[n] { out[n] = p; continue }
            out[n] = free[i % free.count]
            i += 1
        }
        return out
    }
}

/// One `[[project]]` definition.
public struct ProjectDefinition: Equatable, Sendable, Identifiable {
    public var name: String
    public var description: String
    public var color: String
    public var match: [String]
    public var id: String { name }

    public init(name: String, description: String = "", color: String = "", match: [String] = []) {
        self.name = name
        self.description = description
        self.color = color
        self.match = match
    }

    public var hex: String? { Palette.hex(color) }

    /// Whether any pattern matches any target (globs: * and ?, anchored).
    public func matches(_ targets: [String]) -> Bool {
        match.contains { pattern in targets.contains { Glob.match(pattern, $0) } }
    }

    /// Reads the [[project]] tables of a config file. Only the four keys of
    /// a definition, strings and string lists; everything else is skipped.
    public static func parse(toml: String) -> [ProjectDefinition] {
        var out: [ProjectDefinition] = []
        var cur: ProjectDefinition?
        for raw in toml.components(separatedBy: "\n") {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.hasPrefix("[") {
                if let c = cur { out.append(c) }
                cur = line.hasPrefix("[[project]]") ? ProjectDefinition(name: "") : nil
                continue
            }
            guard cur != nil, !line.hasPrefix("#"), let eq = line.firstIndex(of: "=") else { continue }
            let key = line[..<eq].trimmingCharacters(in: .whitespaces)
            let value = line[line.index(after: eq)...].trimmingCharacters(in: .whitespaces)
            switch key {
            case "name": cur!.name = unquote(value)
            case "description": cur!.description = unquote(value)
            case "color": cur!.color = unquote(value)
            case "match": cur!.match = value.hasPrefix("[") ? list(value) : [unquote(value)]
            default: break
            }
        }
        if let c = cur { out.append(c) }
        return out.filter { !$0.name.isEmpty }
    }

    static func unquote(_ v: String) -> String {
        var s = Substring(v)
        if let hash = s.range(of: " #"), !s.hasPrefix("\"") || s[..<hash.lowerBound].hasSuffix("\"") { s = s[..<hash.lowerBound] }
        s = Substring(s.trimmingCharacters(in: .whitespaces))
        if s.hasPrefix("\""), s.hasSuffix("\""), s.count >= 2 { s = s.dropFirst().dropLast() }
        return String(s)
    }

    static func list(_ v: String) -> [String] {
        var items: [String] = []
        var cur = ""
        var inString = false
        for ch in v {
            if ch == "\"" {
                if inString { items.append(cur); cur = "" }
                inString.toggle()
            } else if inString {
                cur.append(ch)
            }
        }
        return items
    }
}

/// Glob matching as internal/project: `*` any run, `?` one character,
/// the whole string.
public enum Glob {
    public static func match(_ pattern: String, _ s: String) -> Bool {
        let p = Array(pattern), t = Array(s)
        var pi = 0, ti = 0, star = -1, mark = 0
        while ti < t.count {
            if pi < p.count && (p[pi] == "?" || p[pi] == t[ti]) {
                pi += 1; ti += 1
            } else if pi < p.count && p[pi] == "*" {
                star = pi; mark = ti; pi += 1
            } else if star >= 0 {
                pi = star + 1; mark += 1; ti = mark
            } else {
                return false
            }
        }
        while pi < p.count && p[pi] == "*" { pi += 1 }
        return pi == p.count
    }
}

/// A raw project key of the archive: the git root of a top-level session,
/// else its working directory, with remote URL, session count and last
/// activity (store.ProjectKeys, computed from `sessions --json`).
public struct ProjectKey: Equatable, Sendable, Identifiable {
    public let key: String
    public let isDir: Bool
    public let remote: String?
    public var sessions: Int
    public var last: Date?
    public var id: String { key }

    /// The name without definitions: base name of a git root, a directory as "~" path.
    public func derivedName(home: String) -> String {
        isDir ? Fmt.tilde(key, home: home) : (key as NSString).lastPathComponent
    }

    /// What the patterns are matched against (project.Targets).
    public func targets(home: String) -> [String] {
        var t = [key, Fmt.tilde(key, home: home), derivedName(home: home)]
        if let r = remote, !r.isEmpty { t.append(r) }
        return t
    }

    /// Groups the top-level sessions by key.
    public static func from(_ sessions: [SessionInfo]) -> [ProjectKey] {
        var byKey: [String: ProjectKey] = [:]
        var order: [String] = []
        for s in sessions where s.parentId == nil {
            let isDir = (s.gitRoot ?? "").isEmpty
            guard let k = isDir ? s.cwd : s.gitRoot, !k.isEmpty else { continue }
            let last = s.endedAt ?? s.startedAt
            if var e = byKey[k] {
                e.sessions += 1
                if let l = last, e.last.map({ l > $0 }) ?? true { e.last = l }
                byKey[k] = e
            } else {
                byKey[k] = ProjectKey(key: k, isDir: isDir, remote: s.gitRemoteUrl, sessions: 1, last: last)
                order.append(k)
            }
        }
        return order.compactMap { byKey[$0] }.sorted { $0.sessions > $1.sessions }
    }
}

/// The definitions applied to the keys: first matching definition wins.
public struct ProjectResolver: Sendable {
    public let definitions: [ProjectDefinition]
    public let home: String

    public init(definitions: [ProjectDefinition], home: String) {
        self.definitions = definitions
        self.home = home
    }

    public func name(of key: ProjectKey) -> String? {
        definitions.first { $0.matches(key.targets(home: home)) }?.name
    }

    /// Keys per definition name, and the keys without a definition.
    public func assign(_ keys: [ProjectKey]) -> (matched: [String: [ProjectKey]], unmatched: [ProjectKey]) {
        var matched: [String: [ProjectKey]] = [:]
        var unmatched: [ProjectKey] = []
        for k in keys {
            if let n = name(of: k) { matched[n, default: []].append(k) } else { unmatched.append(k) }
        }
        return (matched, unmatched)
    }

    /// What a draft at position `index` would match, the other definitions
    /// in front of it taking their keys first ("first match wins").
    public func preview(_ draft: ProjectDefinition, at index: Int, keys: [ProjectKey]) -> [ProjectKey] {
        var defs = definitions
        if index < defs.count { defs[index] = draft } else { defs.append(draft) }
        let r = ProjectResolver(definitions: defs, home: home)
        return keys.filter { r.name(of: $0) == draft.name && draft.matches($0.targets(home: home)) }
    }
}
