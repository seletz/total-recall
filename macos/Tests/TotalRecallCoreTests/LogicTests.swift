import XCTest
@testable import TotalRecallCore

final class LogicTests: XCTestCase {
    func testDurations() {
        let cases: [(Int, Bool, String)] = [
            (141180, false, "39h13m"), (25980, true, "+7h13m"), (-1500, true, "-0h25m"), (0, true, "+0h00m"),
        ]
        for (s, signed, want) in cases { XCTAssertEqual(Fmt.duration(s, signed: signed), want) }
        XCTAssertEqual(Fmt.short(2820), "47m")
        XCTAssertEqual(Fmt.count(1849), "1 849")
        XCTAssertEqual(Fmt.age(25200), "7h00m")
        XCTAssertEqual(Fmt.tilde("/Users/mara/x", home: "/Users/mara"), "~/x")
        XCTAssertEqual(Fmt.repoShort("https://git.invalid/mara/kompass"), "mara/kompass")
    }

    func testSnippetParts() {
        let p = Fmt.snippetParts("Lade den [Cask] mit [goreleaser].")
        XCTAssertEqual(p.filter(\.hit).map(\.text), ["Cask", "goreleaser"])
        XCTAssertEqual(p.map(\.text).joined(), "Lade den Cask mit goreleaser.")
    }

    func testGlob() {
        let cases: [(String, String, Bool)] = [
            ("https://git.invalid/mara/kompass*", "https://git.invalid/mara/kompass-web", true),
            ("~/src/kompass*", "~/src/kompass", true),
            ("~/src/kompass", "~/src/kompass2", false),
            ("a?c", "abc", true),
            ("*", "", true),
        ]
        for (p, s, want) in cases { XCTAssertEqual(Glob.match(p, s), want, "\(p) \(s)") }
    }

    func testProjectDefinitionsFromToml() {
        let toml = """
        [git]
        resolve = true
        # [[project]]
        # name = "commented"
        [[project]]
        name = "kompass"
        description = "Wanderkarten, alle Repositories"
        color = "blue"
        match = ["https://git.invalid/mara/kompass*", "~/src/kompass*"]

        [[project]]
        name = "fahrtenbuch"
        color = "teal"
        match = "https://git.invalid/mara/fahrtenbuch"
        """
        let defs = ProjectDefinition.parse(toml: toml)
        XCTAssertEqual(defs.map(\.name), ["kompass", "fahrtenbuch"])
        XCTAssertEqual(defs[0].match.count, 2)
        XCTAssertEqual(defs[0].hex, "#4e79a7")
        XCTAssertEqual(defs[1].match, ["https://git.invalid/mara/fahrtenbuch"])
    }

    func testLineDiffHunks() {
        let old = (1...20).map { "line \($0)" }.joined(separator: "\n")
        var lines = (1...20).map { "line \($0)" }
        lines[4] = "changed 5"
        lines.append("line 21")
        let hunks = LineDiff.hunks(old: old, new: lines.joined(separator: "\n"))
        XCTAssertEqual(hunks.count, 2)
        XCTAssertEqual(hunks[0].header, "@@ -2,7 +2,7 @@")
        XCTAssertEqual(hunks[0].lines.filter { $0.op == .removed }.map(\.text), ["line 5"])
        let st = LineDiff.stat(old: old, new: lines.joined(separator: "\n"))
        XCTAssertEqual(st.added, 2)
        XCTAssertEqual(st.removed, 1)
        XCTAssertEqual(LineDiff.hunks(old: "a", new: "a"), [])
    }

    func testPeriodWindowMatchesTUI() {
        // The stats shot: 28 days up to 2026-11-01.
        let w = PeriodWindow(period: .day, anchor: Clocks.stats.now())
        XCTAssertEqual(w.flags(Clocks.stats).since, "2026-10-05")
        XCTAssertEqual(w.flags(Clocks.stats).until, "2026-11-01")
        let earlier = w.shifted(-1, Clocks.stats)
        XCTAssertEqual(earlier.flags(Clocks.stats).until, "2026-10-04")
        let weeks = PeriodWindow(period: .week, anchor: Clocks.stats.now())
        XCTAssertEqual(weeks.flags(Clocks.stats).until, "2026-11-01") // Sunday
        XCTAssertEqual(weeks.zoomed(1).count, 32)
    }
}
