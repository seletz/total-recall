import Foundation
import Observation

/// The activity view: per day the first and last activity, the blocks and
/// the span against the working time, for a day, a week or a month, plus
/// the days before it to fill the rows (TUI view activity).
@MainActor @Observable
public final class ActivityModel {
    public enum Kind: String, CaseIterable, Sendable { case day, week, month }

    public let archive: Archive
    public let clock: Clock
    public var kind: Kind
    /// Last day of the window ("YYYY-MM-DD").
    public private(set) var end: String
    public private(set) var length: Int
    public var allEvents = false
    public var projects = false
    public var newestTop = false
    /// Rows to show: the window and the days before it.
    public var rows = 24
    public var selected: String?
    /// Work settings of the config ([work]).
    public var workStart = 9 * 60
    public var workEnd = 17 * 60
    public var dayStart = 4 * 60
    public var target = 8 * 3600
    public var weeklyLow = 32 * 3600
    public var weeklyHigh = 48 * 3600
    public var pinnedColors: [String: String] = [:]

    public private(set) var days: [Workdays.Day] = []
    /// Dominant project per day and quarter hour slot (0..<96 from day start).
    public private(set) var slots: [String: [Int: String]] = [:]
    public private(set) var projectCells: [(project: String, cells: Int)] = []
    /// Every project of the shown rows, most cells first (colour order).
    public private(set) var shownProjects: [String] = []
    public private(set) var error: String?
    public private(set) var tookMs = 0

    public init(archive: Archive, clock: Clock, kind: Kind = .week) {
        self.archive = archive
        self.clock = clock
        self.kind = kind
        self.end = ""
        self.length = 1
        setKind(kind, anchor: clock.today())
    }

    // MARK: - window

    public func setKind(_ k: Kind, anchor: String? = nil) {
        kind = k
        let a = anchor ?? end
        guard let d = clock.date(a) else { return }
        let cal = clock.calendar
        switch k {
        case .day:
            end = a; length = 1
        case .week:
            let start = cal.dateInterval(of: .weekOfYear, for: d)!.start
            end = clock.day(cal.date(byAdding: .day, value: 6, to: start)!); length = 7
        case .month:
            let iv = cal.dateInterval(of: .month, for: d)!
            end = clock.day(cal.date(byAdding: .day, value: -1, to: iv.end)!)
            length = cal.range(of: .day, in: .month, for: d)!.count
        }
        clampToCurrent()
        selected = min(end, clock.today())
    }

    /// The end of the current period: the window never runs past it.
    var currentEnd: String {
        let today = clock.today()
        guard let d = clock.date(today) else { return today }
        let cal = clock.calendar
        switch kind {
        case .day: return today
        case .week: return clock.day(cal.date(byAdding: .day, value: 6, to: cal.dateInterval(of: .weekOfYear, for: d)!.start)!)
        case .month: return clock.day(cal.date(byAdding: .day, value: -1, to: cal.dateInterval(of: .month, for: d)!.end)!)
        }
    }

    func clampToCurrent() { if end > currentEnd { end = currentEnd } }

    public var start: String { clock.addDays(1 - length, to: end) }

    /// ↑/↓: move the window by days (a sliding window).
    public func slide(_ days: Int) {
        end = clock.addDays(days, to: end)
        clampToCurrent()
    }

    /// ←/→: a whole period earlier or later.
    public func jump(_ n: Int) {
        switch kind {
        case .day, .week: slide(n * length)
        case .month:
            guard let d = clock.date(start), let m = clock.calendar.date(byAdding: .month, value: n, to: d) else { return }
            setKind(.month, anchor: clock.day(m))
        }
    }

    /// c: this period, l: the last one.
    public func current() { setKind(kind, anchor: clock.today()) }
    public func last() { current(); jump(-1) }

    /// K/J: the selected day moves within the shown rows.
    public func select(_ delta: Int) {
        let all = shownDays.map(\.day)
        guard !all.isEmpty else { return }
        let i = all.firstIndex(of: selected ?? end) ?? all.count - 1
        selected = all[min(max(i + delta, 0), all.count - 1)]
    }

    public var isCurrent: Bool { end == currentEnd }

    /// "this week · 2026-W23 · 2026-06-01 – 2026-06-07".
    public var title: String {
        var parts: [String] = []
        let aligned: Bool
        if let d = clock.date(start) {
            let cal = clock.calendar
            switch kind {
            case .day: aligned = true
            case .week: aligned = cal.component(.weekday, from: d) == 2
            case .month: aligned = cal.component(.day, from: d) == 1
            }
            if aligned && isCurrent { parts.append("this \(kind.rawValue)") }
            if aligned && kind == .week {
                parts.append(String(format: "%04d-W%02d", cal.component(.yearForWeekOfYear, from: d), cal.component(.weekOfYear, from: d)))
            }
            if aligned && kind == .month { parts.append(clock.format(d, "MMMM yyyy")) }
        }
        parts.append(length == 1 ? start : "\(start) – \(end)")
        return parts.joined(separator: " · ")
    }

    // MARK: - loading

    public func load() async {
        let started = Date()
        let first = clock.addDays(-(max(rows, length) - length), to: start)
        do {
            let w = try await archive.workdays(since: first, until: end, allEvents: allEvents)
            days = w.days
            if projects {
                let tl = try await archive.timeline(since: first, until: clock.addDays(2, to: end), allEvents: allEvents)
                computeSlots(tl)
            }
            error = nil
        } catch {
            self.error = "\(error)"
        }
        tookMs = Int(Date().timeIntervalSince(started) * 1000)
    }

    /// The local "work day" (day start 04:00) and quarter hour slot of a time.
    func slot(_ t: Date) -> (day: String, slot: Int) {
        let shifted = t.addingTimeInterval(TimeInterval(-dayStart * 60))
        let day = clock.day(shifted)
        let c = clock.calendar.dateComponents([.hour, .minute], from: shifted)
        return (day, (c.hour! * 60 + c.minute!) / 15)
    }

    func computeSlots(_ events: [TimelineEvent]) {
        var counts: [String: [Int: [String: Int]]] = [:]
        for e in events {
            let (d, s) = slot(e.ts)
            counts[d, default: [:]][s, default: [:]][e.project ?? "", default: 0] += 1
        }
        // A slot inside a block without own events takes the project of the
        // slot before it, so a bar is coloured from first to last event.
        var out: [String: [Int: String]] = [:]
        var cells: [String: Int] = [:]
        var all: [String: Int] = [:]
        let window = Set(windowDays.map(\.day))
        for (d, bySlot) in counts {
            var m: [Int: String] = [:]
            for (s, byProject) in bySlot {
                m[s] = byProject.max { a, b in a.value == b.value ? a.key > b.key : a.value < b.value }!.key
            }
            if let day = days.first(where: { $0.day == d }) {
                for b in day.blocks ?? [] {
                    let s0 = slot(b.start).slot, s1 = slot(b.end).slot
                    var cur: String?
                    for s in s0...max(s0, s1) {
                        if let p = m[s] { cur = p } else if let c = cur { m[s] = c }
                    }
                }
            }
            out[d] = m
            for p in m.values { all[p, default: 0] += 1 }
            if window.contains(d) { for p in m.values { cells[p, default: 0] += 1 } }
        }
        slots = out
        projectCells = cells.sorted { $0.value > $1.value }.map { ($0.key, $0.value) }
        shownProjects = projectCells.map(\.project) + all.sorted { $0.value > $1.value }.map(\.key).filter { cells[$0] == nil }
    }

    // MARK: - derived

    public var windowDays: [Workdays.Day] { days.filter { $0.day >= start && $0.day <= end } }

    /// The rows in display order: oldest first, or newest first with `o`.
    public var shownDays: [Workdays.Day] {
        let d = Array(days.suffix(max(rows, length)))
        return newestTop ? d.reversed() : d
    }

    public func inWindow(_ d: Workdays.Day) -> Bool { d.day >= start && d.day <= end }

    /// Minutes of a time from local midnight of `day` (may exceed 1440).
    public func minutes(_ t: Date, day: String) -> Int {
        guard let noon = clock.date(day) else { return 0 }
        let midnight = clock.calendar.startOfDay(for: noon)
        return Int(t.timeIntervalSince(midnight) / 60)
    }

    /// "00:45 (+1)" for a time after midnight.
    public func clockLabel(_ t: Date?, day: String) -> String {
        guard let t else { return "-" }
        let m = minutes(t, day: day)
        let s = String(format: "%02d:%02d", (m / 60) % 24, m % 60)
        return m >= 1440 ? "\(s) (+1)" : s
    }

    public struct Totals: Sendable {
        public let span: Int, activeDays: Int, workdays: Int, target: Int, balance: Int
        public let holidays: [(weekday: String, day: String, name: String)]
    }

    /// The headline of the window: span sum against target × workdays up to today.
    public var totals: Totals {
        let w = windowDays
        let today = clock.today()
        let workdays = w.filter { !$0.weekend && $0.holiday == nil && $0.day <= today }.count
        let span = w.reduce(0) { $0 + $1.spanSeconds }
        let hol = w.compactMap { d in d.holiday.map { (d.weekday, String(d.day.suffix(2)), $0) } }
        return Totals(span: span, activeDays: w.filter(\.active).count, workdays: workdays,
                      target: workdays * target, balance: span - workdays * target, holidays: hol)
    }

    public var headline: String {
        let t = totals
        var s = "\(kind.rawValue): \(Fmt.duration(t.span)) in \(t.activeDays) days · target \(Fmt.duration(t.target)) (\(t.workdays) workdays × \(Fmt.duration(target))) · balance \(Fmt.duration(t.balance, signed: true))"
        for h in t.holidays { s += " · \(h.weekday) \(h.day) \(h.name)" }
        return s
    }

    /// Rating of the window's span against the weekly bounds (scaled by workdays/5).
    public var rating: String {
        let t = totals
        guard t.workdays > 0 else { return "none" }
        let low = weeklyLow * t.workdays / 5, high = weeklyHigh * t.workdays / 5
        return t.span < low ? "under" : (t.span > high ? "over" : "ok")
    }

    public struct Figure: Sendable {
        public let label: String
        public let median: String, mean: String, min: String, max: String
    }

    /// Median, mean, min and max of first, last and span over the active days of the window.
    public var figures: [Figure] {
        let active = windowDays.filter(\.active)
        guard !active.isEmpty else { return [] }
        func stats(_ v: [Int], _ fmt: (Int) -> String) -> (String, String, String, String) {
            let s = v.sorted()
            let med = s.count % 2 == 1 ? s[s.count / 2] : (s[s.count / 2 - 1] + s[s.count / 2]) / 2
            return (fmt(med), fmt(v.reduce(0, +) / v.count), fmt(s.first!), fmt(s.last!))
        }
        let clockFmt: (Int) -> String = { m in
            let s = String(format: "%02d:%02d", (m / 60) % 24, m % 60)
            return m >= 1440 ? "\(s) (+1)" : s
        }
        let f = stats(active.map { minutes($0.first!, day: $0.day) }, clockFmt)
        let l = stats(active.map { minutes($0.last!, day: $0.day) }, clockFmt)
        let sp = stats(active.map(\.spanSeconds)) { Fmt.duration($0) }
        return [Figure(label: "first", median: f.0, mean: f.1, min: f.2, max: f.3),
                Figure(label: "last", median: l.0, mean: l.1, min: l.2, max: l.3),
                Figure(label: "span", median: sp.0, mean: sp.1, min: sp.2, max: sp.3)]
    }

    public var projectColors: [String: String] {
        Palette.assign(shownProjects, pinned: pinnedColors)
    }
}

/// The day view: one day chronologically with blocks, sessions, projects
/// and categories (TUI view day, `stats --day`).
@MainActor @Observable
public final class DayModel {
    public let archive: Archive
    public let clock: Clock
    public private(set) var day: String
    public var allEvents: Bool
    public var selectedBlock = 0
    public var expanded: Set<Int> = []
    public var pinnedColors: [String: String] = [:]
    public private(set) var report: DayReport?
    public private(set) var error: String?

    public init(archive: Archive, clock: Clock, day: String, allEvents: Bool = false) {
        self.archive = archive
        self.clock = clock
        self.day = day
        self.allEvents = allEvents
    }

    public func load() async {
        do {
            report = try await archive.day(day, allEvents: allEvents)
            selectedBlock = min(selectedBlock, max(0, (report?.blocks.count ?? 1) - 1))
            error = nil
        } catch {
            self.error = "\(error)"
        }
    }

    public func step(_ n: Int) async {
        day = clock.addDays(n, to: day)
        selectedBlock = 0
        expanded = []
        await load()
    }

    public func toggleExpanded() {
        if expanded.contains(selectedBlock) { expanded.remove(selectedBlock) } else { expanded.insert(selectedBlock) }
    }

    /// Projects in the day's order of time, with colours: configured ones
    /// pinned, the others from the palette.
    public var colors: [String: String] {
        Palette.assign(report?.projects.map(\.project) ?? [], pinned: pinnedColors)
    }

    /// "09:10–00:45 (+1) · span 15h35m · work 12h15m · breaks 3h20m · +7h35m vs 8h00m · booked 5h32m (6h00m) · 37 events".
    public var summary: String {
        guard let r = report, let f = r.first, let l = r.last else { return "no activity" }
        let lastLabel = minutesFromMidnight(l) >= 1440 ? "\(clock.time(l)) (+1)" : clock.time(l)
        return [
            "\(clock.time(f))–\(lastLabel)",
            "span \(Fmt.duration(r.spanSeconds))",
            "work \(Fmt.duration(r.blockSeconds))",
            "breaks \(Fmt.duration(r.breakSeconds))",
            "\(Fmt.duration(r.diffSeconds, signed: true)) vs \(Fmt.duration(r.targetSeconds))",
            "booked \(Fmt.duration(r.worktime.rawSeconds)) (\(Fmt.duration(r.worktime.roundedSeconds)))",
            "\(r.human + r.agent) events",
        ].joined(separator: " · ")
    }

    public func minutesFromMidnight(_ t: Date) -> Int {
        guard let noon = clock.date(day) else { return 0 }
        return Int(t.timeIntervalSince(clock.calendar.startOfDay(for: noon)) / 60)
    }

    public func label(_ t: Date) -> String {
        minutesFromMidnight(t) >= 1440 ? "\(clock.time(t)) (+1)" : clock.time(t)
    }
}
