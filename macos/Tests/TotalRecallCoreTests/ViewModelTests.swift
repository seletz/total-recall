import XCTest
@testable import TotalRecallCore

@MainActor
final class ViewModelTests: XCTestCase {
    func testSearchModelSelectsFirstHitAndPreviewsContext() async throws {
        let m = SearchModel(archive: Archive(runner: FixtureRunner()), clock: Clocks.search, query: "goreleaser cask")
        m.filters.limit = 50
        await m.search()
        XCTAssertNil(m.error)
        XCTAssertEqual(m.hits.count, 10)
        XCTAssertEqual(m.selectedHit?.sessionName, "Release v0.3.0")
        guard case let .events(session, events, hit)? = m.preview else { return XCTFail("no event preview") }
        XCTAssertEqual(session.displayName, "Release v0.3.0")
        XCTAssertEqual(events.first?.id, hit)
        XCTAssertTrue(events.allSatisfy { !$0.isTool })
        XCTAssertEqual(m.summary.hasPrefix("10 hits"), true)
    }

    func testSearchTogglesBuildFlags() {
        let m = SearchModel(archive: Archive(runner: FixtureRunner()), clock: Clocks.search)
        m.toggleType("session")
        m.toggleType("memory")
        XCTAssertEqual(m.filters.types, ["event", "session", "memory"])
        m.toggleType("event")
        XCTAssertEqual(m.filters.types, ["session", "memory"])
        m.toggleAttached()
        XCTAssertEqual(m.filters.types, ["session"])
        m.cycleActor()
        m.cycleSince()
        let args = Archive.searchArguments("x", m.filters)
        XCTAssertEqual(args, ["search", "x", "--has-attachment", "--type", "session", "--actor", "human", "--since", "2026-11-01", "--limit", "50", "--json"])
    }

    func testToolResultContextHasItsCall() async throws {
        let d = try await Archive(runner: FixtureRunner()).show("claude:7d1e4c20-0001-4000-8000-000000000001")
        let result = try XCTUnwrap(d.events.first { $0.kind == "tool_result" })
        let ctx = SearchModel.context(of: result.id, in: d.events)
        XCTAssertEqual(ctx.map(\.kind), ["tool_use", "tool_result"])
    }

    func testSessionModelHitsAndTools() async {
        let m = SessionModel(archive: Archive(runner: FixtureRunner()), sessionID: "claude:7d1e4c20-0001-4000-8000-000000000001", query: "goreleaser cask")
        await m.load()
        XCTAssertEqual(m.detail?.events.count, 8)
        XCTAssertFalse(m.hits.isEmpty)
        XCTAssertEqual(m.focus, m.hits.first)
        let without = m.events.count
        m.showTools = true
        XCTAssertGreaterThan(m.events.count, without)
        XCTAssertTrue(m.position.hasPrefix("8/8 events · hit 1/"))
    }

    func testStatsModelTableAndHeadline() async {
        let m = StatsModel(archive: Archive(runner: FixtureRunner()), clock: Clocks.stats)
        m.pinnedColors = ["kompass": "#4e79a7", "wetterwarte": "#b07aa1", "fahrtenbuch": "#76b7b2"]
        await m.load()
        XCTAssertNil(m.error)
        XCTAssertEqual(m.table.map(\.key), ["kompass", "wetterwarte", "fahrtenbuch", "/home/mara/notizen", "dotfiles"])
        XCTAssertEqual(m.table.first?.sessions, 93)
        XCTAssertEqual(m.headline.first?.value, "2 374")
        XCTAssertEqual(m.headline.last?.value, "24/28")
        XCTAssertEqual(m.colors["kompass"], "#4e79a7")
        XCTAssertEqual(m.colors["/home/mara/notizen"], "#f28e2b") // first free palette colour
        XCTAssertEqual(m.heat.flatMap { $0 }.reduce(0, +), 2374)
        XCTAssertEqual(m.table.reduce(0.0) { $0 + $1.share }, 1.0, accuracy: 0.0001)
    }

    func testActivityWeekMatchesTUIHeadline() async {
        let m = ActivityModel(archive: Archive(runner: FixtureRunner()), clock: Clocks.week)
        XCTAssertEqual(m.start, "2026-06-01")
        XCTAssertEqual(m.end, "2026-06-07")
        XCTAssertEqual(m.title, "this week · 2026-W23 · 2026-06-01 – 2026-06-07")
        await m.load()
        XCTAssertNil(m.error)
        XCTAssertEqual(m.shownDays.count, 24)
        XCTAssertEqual(m.headline, "week: 39h13m in 6 days · target 32h00m (4 workdays × 8h00m) · balance +7h13m · Thu 04 Fronleichnam")
        XCTAssertEqual(m.figures.map(\.median), ["09:20", "16:50", "5h40m"])
        XCTAssertEqual(m.figures[1].max, "00:45 (+1)")
        XCTAssertEqual(m.selected, "2026-06-07")
        m.select(-5)
        XCTAssertEqual(m.selected, "2026-06-02")
    }

    func testActivityProjectsColourSlots() async {
        let m = ActivityModel(archive: Archive(runner: FixtureRunner()), clock: Clocks.week)
        m.projects = true
        await m.load()
        XCTAssertNil(m.error)
        XCTAssertEqual(m.projectCells.first?.project, "fahrtenbuch")
        XCTAssertFalse(m.slots["2026-06-02", default: [:]].isEmpty)
    }

    func testActivityNavigation() {
        let m = ActivityModel(archive: Archive(runner: FixtureRunner()), clock: Clocks.week)
        m.slide(1)
        XCTAssertEqual(m.end, "2026-06-07") // never past the current week
        m.slide(-3)
        XCTAssertEqual(m.start, "2026-05-29")
        m.jump(-1)
        XCTAssertEqual(m.end, "2026-05-28")
        m.current()
        m.setKind(.month)
        XCTAssertEqual(m.start, "2026-06-01")
        XCTAssertEqual(m.end, "2026-06-30")
        XCTAssertEqual(m.length, 30)
    }

    func testDayModelSummary() async {
        let m = DayModel(archive: Archive(runner: FixtureRunner()), clock: Clocks.week, day: "2026-06-02")
        await m.load()
        XCTAssertEqual(m.summary, "09:10–00:45 (+1) · span 15h35m · work 12h15m · breaks 3h20m · +7h35m vs 8h00m · booked 5h32m (6h00m) · 37 events")
        m.allEvents = true
        await m.load()
        XCTAssertTrue(m.summary.hasSuffix("177 events"))
    }

    func testMemoryModelOrderHistoryTimeline() async throws {
        let archive = Archive(runner: FixtureRunner())
        let m = MemoryModel(archive: archive, project: "kompass", projectKey: "-home-mara-src-kompass")
        await m.load()
        XCTAssertEqual(m.files.map(\.path), ["MEMORY.md", "conventions.md", "release-process.md", "old-notes.md"])
        m.selectedFile = "MEMORY.md"
        // The history of MEMORY.md is the only one recorded as a fixture.
        await m.openHistory()
        XCTAssertEqual(m.mode, .history)
        XCTAssertEqual(m.history.count, 4)
        XCTAssertGreaterThan(m.history[0].ts, m.history[1].ts)
    }

    func testConfigModelPagesFromFixtures() async {
        let toml = """
        [[project]]
        name = "kompass"
        description = "Wanderkarten, alle Repositories"
        color = "blue"
        match = ["https://git.invalid/mara/kompass*", "~/src/kompass*"]
        [[project]]
        name = "wetterwarte"
        color = "purple"
        match = "https://git.invalid/mara/wetterwarte"
        [[project]]
        name = "fahrtenbuch"
        color = "teal"
        match = "https://git.invalid/mara/fahrtenbuch"
        """
        let files: [String: String] = [
            "/Users/mara/.config/total-recall/config.toml": toml,
            "/Users/mara/.claude/settings.json": #"{"hooks":{"SessionStart":[{"hooks":[{"command":"total-recall ingest --hook"}]}],"Stop":[{"hooks":[{"command":"total-recall ingest --hook"}]}]}}"#,
            "/Users/mara/.claude.json": #"{"mcpServers":{"total-recall":{"type":"stdio","command":"/Users/mara/.local/bin/total-recall","args":["mcp"]}}}"#,
        ]
        let runner = FixtureRunner(extra: [
            ["hooks", "print", "--harness", "claude"]: Data("{}".utf8),
            ["version"]: Data("total-recall 1.8.1\n".utf8),
        ])
        let m = ConfigModel(archive: Archive(runner: runner), clock: Clocks.search, home: demoHome, readFile: { files[$0] })
        await m.load()
        XCTAssertNil(m.error)
        XCTAssertEqual(m.statusRows.first?.value, "~/Library/Application Support/total-recall/archive.db")
        XCTAssertEqual(m.statusRows.first { $0.label == "last backup" }?.note, "7h00m ago")
        XCTAssertEqual(m.projectRows.map(\.sessions), [326, 218, 88])
        XCTAssertEqual(m.unmatched.map { $0.derivedName(home: "/home/mara") }, ["~/notizen", "dotfiles"])
        XCTAssertEqual(m.previewLine(m.definitions[0], at: 0), "matches 1 path · 326 sessions: kompass")
        XCTAssertEqual(m.checks.first { $0.name == "hooks" }?.detail, "~/.claude/settings.json: SessionStart, Stop")
        XCTAssertEqual(m.checks.first { $0.name == "mcp" }?.ok, true)
        XCTAssertTrue(m.settingGroups.contains { $0.table == "backup" })
        XCTAssertEqual(m.actions.first { $0.name == "migrate" }?.description, "schema v10 is current · nothing pending")
    }
}
