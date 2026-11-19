import Foundation

/// A line diff of two texts as unified hunks (the memory diff and the
/// timeline). The CLI has no diff output, so the app computes it; the TUI
/// uses internal/linediff for the same.
public enum LineDiff {
    public enum Op: Equatable, Sendable { case same, added, removed }

    public struct Line: Equatable, Sendable {
        public let op: Op
        public let text: String
    }

    public struct Hunk: Equatable, Sendable {
        public let oldStart: Int, oldCount: Int, newStart: Int, newCount: Int
        public let lines: [Line]
        public var header: String { "@@ -\(oldStart),\(oldCount) +\(newStart),\(newCount) @@" }
    }

    /// The edit script from a to b (longest common subsequence).
    public static func lines(_ a: [String], _ b: [String]) -> [Line] {
        let n = a.count, m = b.count
        var lcs = Array(repeating: Array(repeating: 0, count: m + 1), count: n + 1)
        for i in stride(from: n - 1, through: 0, by: -1) {
            for j in stride(from: m - 1, through: 0, by: -1) {
                lcs[i][j] = a[i] == b[j] ? lcs[i + 1][j + 1] + 1 : max(lcs[i + 1][j], lcs[i][j + 1])
            }
        }
        var out: [Line] = []
        var i = 0, j = 0
        while i < n && j < m {
            if a[i] == b[j] {
                out.append(Line(op: .same, text: a[i])); i += 1; j += 1
            } else if lcs[i + 1][j] >= lcs[i][j + 1] {
                out.append(Line(op: .removed, text: a[i])); i += 1
            } else {
                out.append(Line(op: .added, text: b[j])); j += 1
            }
        }
        while i < n { out.append(Line(op: .removed, text: a[i])); i += 1 }
        while j < m { out.append(Line(op: .added, text: b[j])); j += 1 }
        return out
    }

    /// Hunks with `context` unchanged lines around each change.
    public static func hunks(old: String, new: String, context: Int = 3) -> [Hunk] {
        let a = split(old), b = split(new)
        let script = lines(a, b)
        // Positions (1-based) of every script line in old and new.
        var oldPos: [Int] = [], newPos: [Int] = []
        var o = 1, nn = 1
        for l in script {
            oldPos.append(o); newPos.append(nn)
            if l.op != .added { o += 1 }
            if l.op != .removed { nn += 1 }
        }
        let changed = script.indices.filter { script[$0].op != .same }
        guard !changed.isEmpty else { return [] }
        var ranges: [ClosedRange<Int>] = []
        for c in changed {
            let r = max(0, c - context)...min(script.count - 1, c + context)
            if let last = ranges.last, r.lowerBound <= last.upperBound + 1 {
                ranges[ranges.count - 1] = last.lowerBound...max(last.upperBound, r.upperBound)
            } else {
                ranges.append(r)
            }
        }
        return ranges.map { r in
            let ls = Array(script[r])
            return Hunk(oldStart: oldPos[r.lowerBound], oldCount: ls.filter { $0.op != .added }.count,
                        newStart: newPos[r.lowerBound], newCount: ls.filter { $0.op != .removed }.count,
                        lines: ls)
        }
    }

    /// Added and removed line counts ("+9 -1").
    public static func stat(old: String, new: String) -> (added: Int, removed: Int) {
        let s = lines(split(old), split(new))
        return (s.filter { $0.op == .added }.count, s.filter { $0.op == .removed }.count)
    }

    static func split(_ s: String) -> [String] {
        if s.isEmpty { return [] }
        var parts = s.components(separatedBy: "\n")
        if parts.last == "" { parts.removeLast() }
        return parts
    }
}
