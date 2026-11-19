import AppKit
import SwiftUI
import TotalRecallCore

/// Options of the executable.
///
///   TotalRecall [--cli PATH] [--now YYYY-MM-DDTHH:MM] [--view search|stats|activity|config] [--query Q]
///   TotalRecall --shots DIR [--only NAME,...]
///
/// The CLI is found on PATH unless --cli names it. --now pins the clock
/// (the demo archive ends in October 2026). --shots renders the
/// screenshots of every view and exits.
public struct LaunchOptions {
    public var cli: String?
    public var now: Date?
    public var view: Tab = .search
    public var query = ""
    public var shots: String?
    public var only: Set<String> = []

    public init(arguments: [String]) {
        var it = arguments.dropFirst().makeIterator()
        while let a = it.next() {
            switch a {
            case "--cli": cli = it.next()
            case "--now": now = it.next().flatMap { CLIDecoder.parseTimestamp($0 + ":00+01:00") ?? CLIDecoder.parseTimestamp($0) }
            case "--view": view = it.next().flatMap(Tab.init(rawValue:)) ?? .search
            case "--query": query = it.next() ?? ""
            case "--shots": shots = it.next()
            case "--only": only = Set((it.next() ?? "").split(separator: ",").map(String.init))
            default: break
            }
        }
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let options: LaunchOptions
    var window: NSWindow?

    init(options: LaunchOptions) { self.options = options }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let home = ProcessInfo.processInfo.environment["HOME"] ?? NSHomeDirectory()
        guard let runner = Self.runner(options) else {
            let alert = NSAlert()
            alert.messageText = "total-recall not found"
            alert.informativeText = "Install it (brew install --cask seletz/tap/total-recall) or start the app with --cli PATH."
            alert.runModal()
            NSApp.terminate(nil)
            return
        }
        if let dir = options.shots {
            Task {
                let shots = Shots.all.filter { options.only.isEmpty || options.only.contains($0.name) }
                do {
                    let url = URL(fileURLWithPath: dir)
                    try await ShotRenderer.render(shots, runner: runner, home: home, to: url)
                    if options.only.isEmpty {
                        try ShotRenderer.index(shots).write(to: url.appendingPathComponent("README.md"), atomically: true, encoding: .utf8)
                    }
                } catch {
                    FileHandle.standardError.write(Data("shots: \(error)\n".utf8))
                    exit(1)
                }
                exit(0)
            }
            return
        }
        let clock = options.now.map { Clock.fixed($0) } ?? Clock()
        let app = AppState(archive: Archive(runner: runner), clock: clock, home: home, tab: options.view, query: options.query)
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1280, height: 800),
                         styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        w.title = "total-recall"
        w.contentView = NSHostingView(rootView: RootView(app: app))
        w.center()
        w.setFrameAutosaveName("main")
        w.makeKeyAndOrderFront(nil)
        window = w
        NSApp.activate()
        Task { await app.start() }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { options.shots == nil }

    static func runner(_ o: LaunchOptions) -> CommandRunner? {
        let url = o.cli.map { URL(fileURLWithPath: $0) } ?? ProcessRunner.locate()
        return url.map { ProcessRunner(executable: $0) }
    }
}

/// Starts the app (or the shot renderer).
@MainActor
public func runApp() {
    let options = LaunchOptions(arguments: CommandLine.arguments)
    let app = NSApplication.shared
    app.setActivationPolicy(options.shots == nil ? .regular : .accessory)
    let delegate = AppDelegate(options: options)
    app.delegate = delegate
    withExtendedLifetime(delegate) { app.run() }
}
