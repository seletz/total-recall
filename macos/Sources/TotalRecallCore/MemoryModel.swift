import Foundation
import Observation

/// The memory viewer of one project: its files (MEMORY.md first, deleted
/// last), a file's content, its history with diffs, and the timeline of
/// all changes of the project (TUI view memory).
@MainActor @Observable
public final class MemoryModel {
    public enum Mode: Sendable, Equatable { case files, history, timeline }

    public struct Change: Identifiable, Sendable {
        public enum Kind: String, Sendable { case added = "+", changed = "~", deleted = "✗" }
        public let path: String
        public let kind: Kind
        public let version: MemoryVersion
        public let previous: MemoryVersion?
        public var id: String { version.id }
        public var delta: Int { version.bytes - (previous?.bytes ?? 0) }
    }

    public let archive: Archive
    public let project: String
    public let projectKey: String
    public var mode: Mode = .files
    public private(set) var files: [MemoryFile] = []
    public var selectedFile: String?
    public private(set) var history: [MemoryVersion] = []
    /// Index into `history` (newest first).
    public var selectedVersion = 0
    public var showDiff = false
    public private(set) var changes: [Change] = []
    public var selectedChange = 0
    public private(set) var error: String?
    private var texts: [String: String] = [:]

    public init(archive: Archive, project: String, projectKey: String) {
        self.archive = archive
        self.project = project
        self.projectKey = projectKey
    }

    /// Orders files as the TUI: MEMORY.md (the index) first, the others by
    /// name, deleted files last.
    public static func order(_ files: [MemoryFile]) -> [MemoryFile] {
        files.sorted { a, b in
            if a.deleted != b.deleted { return !a.deleted }
            if (a.path == "MEMORY.md") != (b.path == "MEMORY.md") { return a.path == "MEMORY.md" }
            return a.path < b.path
        }
    }

    public func load() async {
        do {
            files = Self.order(try await archive.memory().filter { $0.projectKey == projectKey })
            if selectedFile == nil { selectedFile = files.first?.path }
            error = nil
        } catch {
            self.error = "\(error)"
        }
    }

    public func text(_ hash: String?) async -> String {
        guard let hash else { return "" }
        if let t = texts[hash] { return t }
        let t = (try? await archive.text(ofBlob: hash)) ?? ""
        texts[hash] = t
        return t
    }

    public var currentFile: MemoryFile? { files.first { $0.path == selectedFile } }

    /// Opens the history of the selected file, newest version first.
    public func openHistory() async {
        guard let f = selectedFile else { return }
        do {
            history = try await archive.memoryHistory(project: project, path: f).reversed()
            selectedVersion = 0
            showDiff = false
            mode = .history
        } catch {
            self.error = "\(error)"
        }
    }

    /// The selected version and the one before it.
    public var versionPair: (MemoryVersion, MemoryVersion?)? {
        guard history.indices.contains(selectedVersion) else { return nil }
        let prev = history.indices.contains(selectedVersion + 1) ? history[selectedVersion + 1] : nil
        return (history[selectedVersion], prev)
    }

    public func diff(_ v: MemoryVersion, _ prev: MemoryVersion?) async -> [LineDiff.Hunk] {
        LineDiff.hunks(old: await text(prev?.sha256), new: await text(v.sha256))
    }

    /// Builds the timeline from the history of every file of the project.
    public func openTimeline() async {
        var all: [Change] = []
        do {
            for f in files {
                let h = try await archive.memoryHistory(project: project, path: f.path)
                for (i, v) in h.enumerated() {
                    let prev = i > 0 ? h[i - 1] : nil
                    let kind: Change.Kind = v.deleted ? .deleted : (prev == nil || prev!.deleted ? .added : .changed)
                    all.append(Change(path: f.path, kind: kind, version: v, previous: prev))
                }
            }
            changes = all.sorted { $0.version.ts > $1.version.ts }
            selectedChange = 0
            mode = .timeline
        } catch {
            self.error = "\(error)"
        }
    }
}
