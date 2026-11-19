import Foundation
@testable import TotalRecallCore

/// A runner over the recorded CLI outputs in Fixtures/: NAME.args holds the
/// arguments of one call (one per line), NAME.json its stdout. A call with
/// other arguments fails, so the tests also check the argument building.
struct FixtureRunner: CommandRunner {
    let calls: [[String]: Data]
    /// Extra outputs keyed by the first arguments (blob contents).
    let extra: [[String]: Data]

    init(extra: [[String]: Data] = [:]) {
        let dir = Bundle.module.url(forResource: "Fixtures", withExtension: nil)!
        var calls: [[String]: Data] = [:]
        let names = (try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? []
        for n in names where n.hasSuffix(".args") {
            let base = String(n.dropLast(5))
            let args = (try! String(contentsOf: dir.appendingPathComponent(n), encoding: .utf8))
                .split(separator: "\n", omittingEmptySubsequences: false).map(String.init).dropLast()
            calls[Array(args)] = try! Data(contentsOf: dir.appendingPathComponent(base + ".json"))
        }
        self.calls = calls
        self.extra = extra
    }

    func run(_ arguments: [String]) async throws -> Data {
        if let d = calls[arguments] ?? extra[arguments] { return d }
        throw CLIError(kind: "fixture", message: "no fixture for \(arguments)")
    }
}

/// The demo archive's clocks (internal/screenshot/frames.go).
enum Clocks {
    static let berlin = TimeZone(identifier: "Europe/Berlin")!
    static func at(_ y: Int, _ m: Int, _ d: Int, _ h: Int) -> Clock {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = berlin
        return .fixed(c.date(from: DateComponents(year: y, month: m, day: d, hour: h))!)
    }
    static let search = at(2026, 11, 2, 10)
    static let stats = at(2026, 11, 1, 12)
    static let week = at(2026, 6, 7, 12)
}

let demoHome = "/Users/mara"
