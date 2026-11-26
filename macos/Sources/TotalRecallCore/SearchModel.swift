import Foundation
import Observation

/// The search view: query, type toggles, filters, grouped hits and the
/// preview of the selected hit (TUI view search).
@MainActor @Observable
public final class SearchModel {
    public static let allTypes = ["event", "session", "memory"]
    public static let actors: [String?] = [nil, "human", "agent", "tool"]
    public static let sinceChoices: [(label: String, days: Int?)] = [("all", nil), ("1d", 1), ("7d", 7), ("30d", 30), ("90d", 90)]

    public let archive: Archive
    public let clock: Clock
    public var query: String
    public var filters = SearchFilters()
    public var sinceIndex = 0
    public private(set) var hits: [SearchHit] = []
    public private(set) var tookMs = 0
    public private(set) var error: String?
    public private(set) var loading = false
    public var selection: SearchHit.ID?
    /// The preview of the selection.
    public private(set) var preview: Preview?

    public enum Preview: Sendable {
        case events(SessionInfo, [SessionEvent], hit: String)
        case session(SessionInfo, [SessionEvent])
        case memory(MemoryFile?, path: String, project: String, text: String)
    }

    private var sessionCache: [String: SessionDetail] = [:]

    public init(archive: Archive, clock: Clock, query: String = "") {
        self.archive = archive
        self.clock = clock
        self.query = query
    }

    /// Hits per searched type, in the order of the types.
    public var groups: [(type: String, hits: [SearchHit])] {
        filters.types.map { t in (t, hits.filter { $0.type == t }) }
    }

    public var selectedHit: SearchHit? { hits.first { $0.id == selection } }

    public var summary: String {
        if filters.types.count == 1 {
            return hits.count == 1 ? "1 hit (\(tookMs) ms)" : "\(hits.count) hits (\(tookMs) ms)"
        }
        return groups.map { "\($0.type) \($0.hits.count)" }.joined(separator: ", ") + " (\(tookMs) ms)"
    }

    // MARK: - toggles (e/s/m, a, t, i, p, ^r)

    public func toggleType(_ t: String) {
        var ts = Set(filters.types)
        if ts.contains(t) { if ts.count > 1 { ts.remove(t) } } else { ts.insert(t) }
        filters.types = Self.allTypes.filter { ts.contains($0) }
        if filters.attached != nil { filters.types.removeAll { $0 == "memory" } }
        if filters.types.isEmpty { filters.types = ["event"] }
    }

    public func cycleActor() {
        let i = Self.actors.firstIndex(of: filters.actor) ?? 0
        filters.actor = Self.actors[(i + 1) % Self.actors.count]
    }

    public func cycleSince() {
        sinceIndex = (sinceIndex + 1) % Self.sinceChoices.count
        if let d = Self.sinceChoices[sinceIndex].days {
            filters.since = clock.addDays(-d, to: clock.day(clock.now()))
        } else {
            filters.since = nil
        }
    }

    /// Only events and sessions with an attachment; memory has none.
    public func toggleAttached() {
        filters.attached = filters.attached == nil ? .any : nil
        if filters.attached != nil {
            filters.types.removeAll { $0 == "memory" }
            if filters.types.isEmpty { filters.types = ["event"] }
        }
    }

    /// Pins the project of the selected hit; again unpins.
    public func togglePin() {
        if filters.project != nil { filters.project = nil; return }
        filters.project = selectedHit?.project
    }

    // MARK: - loading

    public func search() async {
        loading = true
        defer { loading = false }
        let started = Date()
        do {
            // An empty query only lists with the attachment filter.
            if query.trimmingCharacters(in: .whitespaces).isEmpty && filters.attached == nil {
                hits = []
            } else {
                hits = try await archive.search(query, filters: filters)
            }
            error = nil
        } catch {
            hits = []
            self.error = "\(error)"
        }
        tookMs = Int(Date().timeIntervalSince(started) * 1000)
        if selection == nil || !hits.contains(where: { $0.id == selection }) {
            selection = groups.first { !$0.hits.isEmpty }?.hits.first?.id
        }
        await loadPreview()
    }

    public func move(_ delta: Int) async {
        let flat = groups.flatMap(\.hits)
        guard !flat.isEmpty else { return }
        let i = flat.firstIndex { $0.id == selection } ?? 0
        selection = flat[min(max(i + delta, 0), flat.count - 1)].id
        await loadPreview()
    }

    public func detail(_ session: String) async throws -> SessionDetail {
        if let d = sessionCache[session] { return d }
        let d = try await archive.show(session)
        sessionCache[session] = d
        return d
    }

    public func loadPreview() async {
        guard let hit = selectedHit else { preview = nil; return }
        do {
            switch hit.type {
            case "memory":
                var text = ""
                if let h = hit.sha256 { text = try await archive.text(ofBlob: h) }
                preview = .memory(nil, path: hit.path ?? "", project: hit.project ?? "", text: text)
            case "session":
                guard let sid = hit.sessionId else { preview = nil; return }
                let d = try await detail(sid)
                preview = .session(d.session, Array(d.events.filter { !$0.isTool }.prefix(8)))
            default:
                guard let sid = hit.sessionId, let eid = hit.eventId else { preview = nil; return }
                let d = try await detail(sid)
                preview = .events(d.session, Self.context(of: eid, in: d.events), hit: eid)
            }
        } catch {
            preview = nil
            self.error = "\(error)"
        }
    }

    /// The events around a hit: a tool result with its call in front, then
    /// the hit and the next messages of the session.
    public static func context(of eventID: String, in events: [SessionEvent], after: Int = 4) -> [SessionEvent] {
        guard let i = events.firstIndex(where: { $0.id == eventID }) else { return [] }
        var out: [SessionEvent] = []
        if events[i].kind == "tool_result", i > 0, events[i - 1].kind == "tool_use" { out.append(events[i - 1]) }
        out.append(events[i])
        if events[i].isTool { return out }
        out += events[(i + 1)...].filter { !$0.isTool }.prefix(after)
        return out
    }
}
