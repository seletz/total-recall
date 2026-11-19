import Foundation
import Observation

/// The range of whole periods a view shows (internal/tui/window): Count
/// periods ending with the period of the anchor.
public struct PeriodWindow: Equatable, Sendable {
    public enum Period: String, CaseIterable, Sendable { case day, week, month }
    public var period: Period
    public var anchor: Date
    public var count: Int

    public static func defaultCount(_ p: Period) -> Int {
        switch p { case .day: 28; case .week: 16; case .month: 12 }
    }

    public init(period: Period, anchor: Date, count: Int? = nil) {
        self.period = period
        self.anchor = anchor
        self.count = count ?? Self.defaultCount(period)
    }

    func start(_ d: Date, _ cal: Calendar) -> Date {
        switch period {
        case .day: return cal.startOfDay(for: d)
        case .week: return cal.dateInterval(of: .weekOfYear, for: d)!.start
        case .month: return cal.dateInterval(of: .month, for: d)!.start
        }
    }

    func add(_ d: Date, _ n: Int, _ cal: Calendar) -> Date {
        switch period {
        case .day: return cal.date(byAdding: .day, value: n, to: d)!
        case .week: return cal.date(byAdding: .day, value: 7 * n, to: d)!
        case .month: return cal.date(byAdding: .month, value: n, to: d)!
        }
    }

    public func since(_ clock: Clock) -> Date { add(start(anchor, clock.calendar), 1 - count, clock.calendar) }
    public func until(_ clock: Clock) -> Date { add(start(anchor, clock.calendar), 1, clock.calendar) }

    /// The CLI flags: --since the first day, --until the last day (a date
    /// includes the whole day).
    public func flags(_ clock: Clock) -> (since: String, until: String) {
        let last = clock.calendar.date(byAdding: .day, value: -1, to: until(clock))!
        return (clock.day(since(clock)), clock.day(last))
    }

    public func shifted(_ n: Int, _ clock: Clock) -> PeriodWindow {
        var w = self
        w.anchor = add(anchor, n * count, clock.calendar)
        return w
    }

    public func zoomed(_ n: Int) -> PeriodWindow {
        var w = self
        w.count = n > 0 ? w.count * 2 : w.count / 2
        w.count = min(max(w.count, 1), 8 * Self.defaultCount(period))
        return w
    }
}

/// The stats view: a stacked chart per period and key, the table per key,
/// the heatmap weekday × hour (TUI view stats).
@MainActor @Observable
public final class StatsModel {
    public enum Dimension: String, CaseIterable, Sendable { case project, agent, directory }
    public enum Metric: Int, CaseIterable, Sendable {
        case events, human, sessions
        public var label: String { ["events", "human", "sessions"][rawValue] }
        public func value(human: Int, agent: Int, sessions: Int) -> Int {
            switch self { case .events: human + agent; case .human: human; case .sessions: sessions }
        }
    }

    public let archive: Archive
    public let clock: Clock
    public var window: PeriodWindow
    public var dimension: Dimension = .project
    public var metric: Metric = .events
    public var heatmap = false
    /// Configured colours of project names (#rrggbb).
    public var pinnedColors: [String: String] = [:]
    public private(set) var series: StatsSeries?
    public private(set) var summary: StatsSummary?
    public private(set) var error: String?
    public private(set) var tookMs = 0

    public init(archive: Archive, clock: Clock) {
        self.archive = archive
        self.clock = clock
        self.window = PeriodWindow(period: .day, anchor: clock.now())
    }

    public func setPeriod(_ p: PeriodWindow.Period) {
        window = PeriodWindow(period: p, anchor: window.anchor)
    }

    public func load() async {
        let started = Date()
        let f = window.flags(clock)
        do {
            async let s = archive.series(since: f.since, until: f.until, period: window.period.rawValue, by: dimension.rawValue)
            async let sum = archive.stats(since: f.since, until: f.until)
            series = try await s
            summary = try await sum
            error = nil
        } catch {
            self.error = "\(error)"
        }
        tookMs = Int(Date().timeIntervalSince(started) * 1000)
    }

    /// Keys in volume order (the CLI's order) with their colour.
    public var colors: [String: String] {
        Palette.assign(series?.keys ?? [], pinned: dimension == .project ? pinnedColors : [:])
    }

    public struct Row: Hashable, Sendable {
        public let key: String
        public let sessions: Int
        public let sessionDays: Int
        public let human: Int
        public let agent: Int
        public let share: Double
    }

    /// The table: per key sessions, session-days (sessions summed over the
    /// periods), human and agent events, share of all events.
    public var table: [Row] {
        guard let s = series else { return [] }
        let total = max(1, s.total.human + s.total.agent)
        return s.byKey.map { c in
            let days = s.buckets.reduce(0) { acc, b in acc + (b.cells.first { $0.key == c.key }?.sessions ?? 0) }
            return Row(key: c.key, sessions: c.sessions, sessionDays: days, human: c.human, agent: c.agent,
                       share: Double(c.human + c.agent) / Double(total))
        }
    }

    /// "2 374 events  525 human  1 849 agent  161 sessions  24/28 days active".
    public var headline: [(label: String, value: String)] {
        guard let s = series else { return [] }
        let active = s.buckets.filter { $0.human + $0.agent > 0 }.count
        return [
            ("events", Fmt.count(s.total.human + s.total.agent)),
            ("human", Fmt.count(s.total.human)),
            ("agent", Fmt.count(s.total.agent)),
            ("sessions", Fmt.count(s.total.sessions)),
            ("\(window.period.rawValue)s active", "\(active)/\(s.buckets.count)"),
        ]
    }

    /// The heatmap cells: [weekday 0 = Monday][hour] of the metric.
    public var heat: [[Int]] {
        var m = Array(repeating: Array(repeating: 0, count: 24), count: 7)
        for c in summary?.byWeekdayHour ?? [] where (0..<7).contains(c.weekday) && (0..<24).contains(c.hour) {
            m[c.weekday][c.hour] = metric.value(human: c.human, agent: c.agent, sessions: c.sessions)
        }
        return m
    }

    public var rangeLabel: String {
        let f = window.flags(clock)
        return "\(f.since) – \(f.until)"
    }
}
