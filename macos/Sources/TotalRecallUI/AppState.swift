import Foundation
import Observation
import TotalRecallCore

/// The four views of the app, as the tabs 1–4 of the TUI.
public enum Tab: String, CaseIterable, Sendable {
    case search, stats, activity, config
    public var title: String { rawValue.capitalized }
    public var key: Character { Character(String(Tab.allCases.firstIndex(of: self)! + 1)) }
    public var symbol: String {
        switch self {
        case .search: "magnifyingglass"
        case .stats: "chart.bar.xaxis"
        case .activity: "calendar.day.timeline.left"
        case .config: "gearshape"
        }
    }
}

/// A view pushed on top of a tab (the TUI's "› session", "› memory", …).
public enum Route {
    case session(SessionModel)
    case memory(MemoryModel)
    case attachments(AttachmentModel)
    case day(DayModel)

    public var title: String {
        switch self {
        case .session: "session"
        case .memory: "memory"
        case .attachments: "attachments"
        case .day: "day"
        }
    }
}

/// Everything the window shows: one model per view, the pushed routes of
/// each tab and the help overlay. The views only read it and call its
/// methods; the shots drive the same methods.
@MainActor @Observable
public final class AppState {
    public let archive: Archive
    public let clock: Clock
    public let home: String
    public var tab: Tab
    public var routes: [Tab: [Route]] = [:]
    public var showHelp = false
    public private(set) var updated: Date

    public let search: SearchModel
    public let stats: StatsModel
    public let activity: ActivityModel
    public let config: ConfigModel
    /// Configured project colours, from the [[project]] definitions.
    public private(set) var projectColors: [String: String] = [:]

    public init(archive: Archive, clock: Clock, home: String, tab: Tab = .search, query: String = "") {
        self.archive = archive
        self.clock = clock
        self.home = home
        self.tab = tab
        self.updated = clock.now()
        search = SearchModel(archive: archive, clock: clock, query: query)
        stats = StatsModel(archive: archive, clock: clock)
        activity = ActivityModel(archive: archive, clock: clock)
        config = ConfigModel(archive: archive, clock: clock, home: home)
    }

    public var route: Route? { routes[tab]?.last }

    public func push(_ r: Route) { routes[tab, default: []].append(r) }

    /// esc: back one route.
    public func pop() {
        if showHelp { showHelp = false; return }
        if !(routes[tab]?.isEmpty ?? true) { routes[tab]!.removeLast() }
    }

    /// Loads what every view needs at the start: the config (project colours,
    /// work hours) first, then the start view.
    public func start() async {
        await config.load()
        projectColors = Dictionary(uniqueKeysWithValues: config.definitions.compactMap { d in d.hex.map { (d.name, $0) } })
        stats.pinnedColors = projectColors
        activity.pinnedColors = projectColors
        applyWorkSettings()
        await reload()
    }

    func applyWorkSettings() {
        guard let c = config.config else { return }
        func hm(_ s: Substring) -> Int? {
            let p = s.split(separator: ":").compactMap { Int($0) }
            return p.count == 2 ? p[0] * 60 + p[1] : nil
        }
        if let h = c.value("work.hours") {
            let p = h.split(separator: "-")
            if p.count == 2, let a = hm(p[0]), let b = hm(p[1]) { activity.workStart = a; activity.workEnd = b }
        }
        if let d = c.value("day_start"), let m = hm(Substring(d)) { activity.dayStart = m }
        activity.allEvents = c.value("work.count") == "all"
        activity.projects = c.value("tui.activity_projects") == "true"
        activity.newestTop = c.value("tui.activity_newest") == "top"
    }

    /// Reloads the visible view (R in the TUI).
    public func reload() async {
        updated = clock.now()
        switch tab {
        case .search: await search.search()
        case .stats: await stats.load()
        case .activity: await activity.load()
        case .config: await config.load()
        }
        switch route {
        case .session(let m)?: await m.load()
        case .memory(let m)?: await m.load()
        case .attachments(let m)?: await m.load()
        case .day(let m)?: await m.load()
        case nil: break
        }
    }

    public func select(_ t: Tab) async {
        tab = t
        await reload()
    }

    // MARK: - navigation from the search view (enter, v)

    /// enter on a hit: a memory hit opens the memory viewer of its project,
    /// any other the session at the hit.
    public func openSelectedHit() async {
        guard let hit = search.selectedHit else { return }
        if hit.type == "memory", let key = hit.projectKey {
            let m = MemoryModel(archive: archive, project: hit.project ?? "", projectKey: key)
            m.selectedFile = hit.path
            push(.memory(m))
            await m.load()
            return
        }
        guard let sid = hit.sessionId else { return }
        await openSession(sid, focus: hit.type == "event" ? hit.eventId : nil, query: search.query)
    }

    public func openSession(_ id: String, focus: String? = nil, query: String = "") async {
        let m = SessionModel(archive: archive, sessionID: id, query: query, focus: focus)
        push(.session(m))
        await m.load()
    }

    /// v: the attachments of the selected hit's session (or of the open session).
    public func openAttachments() async {
        var sid: String?
        var first: String?
        if case .session(let s)? = route {
            sid = s.sessionID
            first = s.focus.flatMap { f in s.detail?.events.first { $0.id == f }?.attachments.first?.sha256 }
        } else if let hit = search.selectedHit {
            sid = hit.sessionId
            first = hit.attachments?.first?.sha256
        }
        guard let sid, let d = try? await search.detail(sid) else { return }
        let atts = d.events.flatMap(\.attachments)
        guard !atts.isEmpty else { return }
        let m = AttachmentModel(archive: archive, title: d.session.displayName ?? sid, attachments: atts,
                                index: atts.firstIndex { $0.sha256 == first } ?? 0)
        push(.attachments(m))
        await m.load()
    }

    /// enter in the activity view: the selected day.
    public func openDay(_ day: String? = nil) async {
        guard let d = day ?? activity.selected else { return }
        let m = DayModel(archive: archive, clock: clock, day: d, allEvents: activity.allEvents)
        m.pinnedColors = projectColors
        push(.day(m))
        await m.load()
    }
}
