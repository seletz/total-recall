import Foundation
import Observation

/// The session viewer: header, events with or without tool calls, the hits
/// of the search query (n/N) and the attachments (TUI view session).
@MainActor @Observable
public final class SessionModel {
    public let archive: Archive
    public let sessionID: String
    public let query: String
    public var showTools: Bool
    public private(set) var detail: SessionDetail?
    public private(set) var error: String?
    /// The event the view scrolls to: the opening hit, then n/N.
    public var focus: String?

    public init(archive: Archive, sessionID: String, query: String = "", focus: String? = nil, showTools: Bool = false) {
        self.archive = archive
        self.sessionID = sessionID
        self.query = query
        self.focus = focus
        self.showTools = showTools
    }

    public func load() async {
        do {
            detail = try await archive.show(sessionID)
            if focus == nil { focus = hits.first }
        } catch {
            self.error = "\(error)"
        }
    }

    public var events: [SessionEvent] {
        guard let d = detail else { return [] }
        return showTools ? d.events : d.events.filter { !$0.isTool || $0.id == focus }
    }

    /// IDs of the events whose text contains every word of the query.
    public var hits: [String] {
        let words = query.lowercased().split(separator: " ").map(String.init).filter { !$0.isEmpty }
        guard !words.isEmpty, let d = detail else { return [] }
        return d.events.filter { e in
            let t = (e.text ?? "").lowercased()
            return words.allSatisfy { t.contains($0) }
        }.map(\.id)
    }

    public var hitPosition: Int? { focus.flatMap { hits.firstIndex(of: $0) } }

    public func nextHit(_ delta: Int) {
        let h = hits
        guard !h.isEmpty else { return }
        let i = hitPosition ?? -1
        focus = h[(i + delta + h.count) % h.count]
        if let f = focus, let e = detail?.events.first(where: { $0.id == f }), e.isTool { showTools = true }
    }

    /// Every attachment of the session in event order.
    public var attachments: [(event: SessionEvent, attachment: Attachment)] {
        (detail?.events ?? []).flatMap { e in e.attachments.map { (e, $0) } }
    }

    /// "1/8 events · hit 1/8 · tools" as the TUI's rule.
    public var position: String {
        let all = detail?.events.count ?? 0
        var parts = ["\(events.count)/\(all) events"]
        if let p = hitPosition { parts.append("hit \(p + 1)/\(hits.count)") }
        if showTools { parts.append("tools") }
        return parts.joined(separator: " · ")
    }
}

/// The attachment viewer: the attachments of a session as tabs, the
/// selected one's bytes (image or document) and its metadata.
@MainActor @Observable
public final class AttachmentModel {
    public let archive: Archive
    public let title: String
    public let attachments: [Attachment]
    public var index: Int
    public private(set) var data: Data?
    public private(set) var info: BlobInfo?
    public private(set) var error: String?

    public init(archive: Archive, title: String, attachments: [Attachment], index: Int = 0) {
        self.archive = archive
        self.title = title
        self.attachments = attachments
        self.index = index
    }

    public var current: Attachment? { attachments.indices.contains(index) ? attachments[index] : nil }

    /// The CLI line that writes the attachment to a file.
    public var blobCommand: String { "total-recall blob \(current?.shortHash ?? "") --out FILE" }

    public func select(_ i: Int) async {
        guard attachments.indices.contains(i) else { return }
        index = i
        await load()
    }

    public func load() async {
        guard let a = current else { return }
        do {
            async let d = archive.blob(a.sha256)
            async let i = archive.blobInfo(a.sha256)
            data = try await d
            info = try await i
            error = nil
        } catch {
            data = nil
            self.error = "\(error)"
        }
    }
}
