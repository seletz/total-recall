import Foundation

/// Clock, calendar and formatting shared by the views. The time zone is
/// the archive's (`timezone` of the config, Europe/Berlin by default); the
/// clock is injected so that the screenshots render fixed dates.
public struct Clock: Sendable {
    public var now: @Sendable () -> Date
    public var timeZone: TimeZone

    public init(now: @escaping @Sendable () -> Date = { Date() }, timeZone: TimeZone = TimeZone(identifier: "Europe/Berlin")!) {
        self.now = now
        self.timeZone = timeZone
    }

    public static func fixed(_ date: Date, timeZone: TimeZone = TimeZone(identifier: "Europe/Berlin")!) -> Clock {
        Clock(now: { date }, timeZone: timeZone)
    }

    public var calendar: Calendar {
        var c = Calendar(identifier: .iso8601)
        c.timeZone = timeZone
        c.firstWeekday = 2
        return c
    }

    /// A local date at noon from "YYYY-MM-DD".
    public func date(_ day: String) -> Date? {
        let p = day.split(separator: "-").compactMap { Int($0) }
        guard p.count == 3 else { return nil }
        return calendar.date(from: DateComponents(year: p[0], month: p[1], day: p[2], hour: 12))
    }

    /// "YYYY-MM-DD" of a date in local time.
    public func day(_ date: Date) -> String {
        let c = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", c.year!, c.month!, c.day!)
    }

    public func addDays(_ n: Int, to day: String) -> String {
        guard let d = date(day), let r = calendar.date(byAdding: .day, value: n, to: d) else { return day }
        return self.day(r)
    }

    /// The local "work day" of now: a day begins at dayStart (04:00).
    public func today(dayStart: Int = 4) -> String {
        day(now().addingTimeInterval(TimeInterval(-dayStart * 3600)))
    }

    public func format(_ date: Date, _ pattern: String) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = timeZone
        f.dateFormat = pattern
        return f.string(from: date)
    }

    /// "2026-10-31 22:14".
    public func minute(_ date: Date) -> String { format(date, "yyyy-MM-dd HH:mm") }
    /// "22:14".
    public func time(_ date: Date) -> String { format(date, "HH:mm") }

    /// "7h00m ago", "12d17h ago", "0s ago".
    public func ago(_ date: Date) -> String {
        "\(Fmt.age(now().timeIntervalSince(date))) ago"
    }
}

/// Number and duration formats of the TUI.
public enum Fmt {
    /// "7h13m", "47m", "0h05m" with sign when asked: "+7h13m", "-0h25m".
    public static func duration(_ seconds: Int, signed: Bool = false, padHours: Bool = true) -> String {
        let neg = seconds < 0
        let s = abs(seconds)
        let h = s / 3600
        let m = (s % 3600) / 60
        var out = (h > 0 || padHours) ? "\(h)h\(String(format: "%02d", m))m" : "\(m)m"
        if signed { out = (neg ? "-" : "+") + out } else if neg { out = "-" + out }
        return out
    }

    /// Short durations of the day view: "47m", "2h59m".
    public static func short(_ seconds: Int) -> String { duration(seconds, padHours: false) }

    /// Age with two units: "0s", "45s", "12m", "7h00m", "12d17h".
    public static func age(_ interval: TimeInterval) -> String {
        let s = max(0, Int(interval))
        if s < 60 { return "\(s)s" }
        if s < 3600 { return "\(s / 60)m" }
        if s < 86400 { return "\(s / 3600)h\(String(format: "%02d", (s % 3600) / 60))m" }
        return "\(s / 86400)d\(String(format: "%02d", (s % 86400) / 3600))h"
    }

    /// Thousands with a thin space as in the TUI: "1 849".
    public static func count(_ n: Int) -> String {
        let s = String(abs(n))
        var out = ""
        for (i, ch) in s.enumerated() {
            if i > 0 && (s.count - i) % 3 == 0 { out.append(" ") }
            out.append(ch)
        }
        return n < 0 ? "-" + out : out
    }

    /// "956 B", "2.0 KiB", "11.0 MiB".
    public static func bytes(_ n: Int) -> String {
        if n < 1024 { return "\(n) B" }
        if n < 1024 * 1024 { return String(format: "%.1f KiB", Double(n) / 1024) }
        if n < 1024 * 1024 * 1024 { return String(format: "%.1f MiB", Double(n) / 1048576) }
        return String(format: "%.1f GiB", Double(n) / 1073741824)
    }

    /// Replaces the home directory with "~".
    public static func tilde(_ path: String?, home: String) -> String {
        guard let path else { return "" }
        guard !home.isEmpty, path.hasPrefix(home) else { return path }
        return "~" + path.dropFirst(home.count)
    }

    /// "git.invalid/mara/kompass" → "mara/kompass" as in the TUI headers.
    public static func repoShort(_ repo: String?) -> String {
        guard let repo, !repo.isEmpty else { return "" }
        var r = repo
        for p in ["https://", "http://", "ssh://", "git@"] where r.hasPrefix(p) { r.removeFirst(p.count) }
        let parts = r.split(separator: "/")
        if parts.count >= 3 { return parts.suffix(2).joined(separator: "/") }
        return r
    }

    /// The FTS snippet markers of the CLI: "[word]" → the parts in order,
    /// each with whether it is a hit.
    public static func snippetParts(_ s: String) -> [(text: String, hit: Bool)] {
        var out: [(String, Bool)] = []
        var cur = ""
        var inHit = false
        for ch in s {
            if ch == "[" && !inHit {
                if !cur.isEmpty { out.append((cur, false)) }
                cur = ""
                inHit = true
            } else if ch == "]" && inHit {
                out.append((cur, true))
                cur = ""
                inHit = false
            } else {
                cur.append(ch)
            }
        }
        if !cur.isEmpty { out.append((inHit ? "[" + cur : cur, false)) }
        return out
    }
}
