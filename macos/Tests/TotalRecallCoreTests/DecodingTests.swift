import XCTest
@testable import TotalRecallCore

final class DecodingTests: XCTestCase {
    let archive = Archive(runner: FixtureRunner())

    func testSearchHitsDecodeWithFields() async throws {
        let hits = try await archive.search("goreleaser cask")
        XCTAssertEqual(hits.count, 10)
        let h = try XCTUnwrap(hits.first)
        XCTAssertEqual(h.type, "event")
        XCTAssertEqual(h.sessionName, "Release v0.3.0")
        XCTAssertEqual(h.project, "kompass")
        XCTAssertEqual(h.actor, "human")
        XCTAssertEqual(h.attachments, [])
        XCTAssertEqual(Clocks.search.minute(h.ts), "2026-10-31 22:14")
    }

    func testSearchTypesGroupsMemoryHit() async throws {
        var f = SearchFilters()
        f.types = ["event", "session", "memory"]
        let hits = try await archive.search("goreleaser cask", filters: f)
        let mem = try XCTUnwrap(hits.first { $0.type == "memory" })
        XCTAssertEqual(mem.path, "release-process.md")
        XCTAssertEqual(mem.projectKey, "-home-mara-src-kompass")
        XCTAssertNotNil(mem.sha256)
        XCTAssertEqual(hits.filter { $0.type == "session" }.count, 0)
    }

    func testAttachmentFilterWithoutQuery() async throws {
        var f = SearchFilters()
        f.types = ["event", "session"]
        f.attached = .any
        let hits = try await archive.search("", filters: f)
        XCTAssertFalse(hits.isEmpty)
        XCTAssertTrue(hits.allSatisfy { ($0.attachmentCount ?? 0) > 0 })
    }

    func testShowDecodesSessionAndEvents() async throws {
        let d = try await archive.show("claude:7d1e4c20-0001-4000-8000-000000000001")
        XCTAssertEqual(d.session.displayName, "Release v0.3.0")
        XCTAssertEqual(d.session.continuedFrom, "claude:7d1e4c20-0006-4000-8000-000000000006")
        XCTAssertEqual(d.events.count, 8)
        XCTAssertEqual(d.events[1].kind, "tool_use")
        XCTAssertEqual(d.events[1].toolName, "Read")
    }

    func testCLIErrorObjectBecomesError() async throws {
        do {
            _ = try await archive.show("claude:does-not-exist")
            XCTFail("expected an error")
        } catch let e as CLIError {
            XCTAssertEqual(e.kind, "not_found")
        }
    }

    func testStatsAndSeries() async throws {
        let s = try await archive.stats(since: "2026-10-05", until: "2026-11-01")
        XCTAssertEqual(s.byWeekdayHour.count, 7 * 24)
        let series = try await archive.series(since: "2026-10-05", until: "2026-11-01", period: "day", by: "project")
        XCTAssertEqual(series.buckets.count, 28)
        XCTAssertEqual(series.keys.first, "kompass")
        // The parts add up to the total.
        XCTAssertEqual(series.byKey.reduce(0) { $0 + $1.human }, series.total.human)
        XCTAssertEqual(series.buckets.reduce(0) { $0 + $1.agent }, series.total.agent)
    }

    func testWorkdaysAndDay() async throws {
        let w = try await archive.workdays(since: "2026-05-15", until: "2026-06-07", allEvents: false)
        XCTAssertEqual(w.days.count, 24)
        let thu = try XCTUnwrap(w.days.first { $0.day == "2026-06-04" })
        XCTAssertEqual(thu.holiday, "Fronleichnam")
        XCTAssertEqual(thu.targetSeconds, 0)
        let d = try await archive.day("2026-06-02", allEvents: false)
        XCTAssertEqual(d.blocks.count, 2)
        XCTAssertEqual(d.projects.map(\.project), ["fahrtenbuch", "wetterwarte", "kompass"])
        // Projects of a block add up to the block.
        for b in d.blocks { XCTAssertEqual(b.projects?.reduce(0) { $0 + $1.seconds }, b.seconds) }
        let all = try await archive.day("2026-06-02", allEvents: true)
        XCTAssertGreaterThan(all.human + all.agent, d.human + d.agent)
    }

    func testMemoryStatusConfig() async throws {
        let m = try await archive.memory()
        XCTAssertEqual(m.count, 7)
        XCTAssertEqual(m.filter(\.deleted).map(\.path), ["old-notes.md"])
        let h = try await archive.memoryHistory(project: "kompass", path: "MEMORY.md")
        XCTAssertEqual(h.count, 4)
        let st = try await archive.status()
        XCTAssertEqual(st.sessions, 936)
        XCTAssertEqual(st.lastIngest["claude"].map { Clocks.search.minute($0) }, "2026-11-02 10:00")
        let c = try await archive.config()
        XCTAssertEqual(c.value("timezone"), "Europe/Berlin")
        XCTAssertEqual(c.value("work.hours"), "09:00-17:00")
    }
}
