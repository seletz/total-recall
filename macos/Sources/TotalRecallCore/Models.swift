import Foundation

// The JSON outputs of the CLI the app reads, decoded with
// ``CLIDecoder/make()`` (snake_case → camelCase). Field lists follow
// docs/reference/10-befehle.md; every documented key is always present,
// strings without value are null.

/// One attachment of an event (`search`, `show`).
public struct Attachment: Decodable, Hashable, Sendable {
    public let sha256: String
    public let mediaType: String
    public let bytes: Int
    public let position: Int
    public var isImage: Bool { mediaType.hasPrefix("image/") }
    public var shortHash: String { String(sha256.prefix(12)) }
}

/// One hit of `search --json` (all types; the fields of the other types
/// are nil).
public struct SearchHit: Decodable, Hashable, Sendable, Identifiable {
    public let type: String
    public let sessionId: String?
    public let sessionName: String?
    public let repo: String?
    public let project: String?
    public let ts: Date
    public let snippet: String
    public let rank: Double
    // event
    public let eventId: String?
    public let actor: String?
    public let inputKind: String?
    public let kind: String?
    public let attachments: [Attachment]?
    public let attachmentCount: Int?
    // session
    public let harness: String?
    public let endedAt: Date?
    public let events: Int?
    // memory
    public let projectKey: String?
    public let path: String?
    public let sha256: String?

    public var id: String {
        switch type {
        case "memory": return "memory:\(projectKey ?? ""):\(path ?? "")"
        case "session": return "session:\(sessionId ?? "")"
        default: return "event:\(eventId ?? "")"
        }
    }
}

/// The header of a session (`sessions --json`, the `session` of `show`).
public struct SessionInfo: Decodable, Hashable, Sendable, Identifiable {
    public let id: String
    public let harness: String?
    public let parentId: String?
    public let displayName: String?
    public let cwd: String?
    public let gitRoot: String?
    public let gitRemoteUrl: String?
    public let repo: String?
    public let gitBranch: String?
    public let model: String?
    public let startedAt: Date?
    public let endedAt: Date?
    public let durationSeconds: Int?
    public let humanEvents: Int?
    public let agentEvents: Int?
    public let events: Int?
    public let attachmentCount: Int?
    public let continuedIn: String?
    public let continuedFrom: String?
    public let account: String?
}

/// One event of `show --json`.
public struct SessionEvent: Decodable, Hashable, Sendable, Identifiable {
    public let id: String
    public let ts: Date
    public let kind: String
    public let role: String?
    public let actor: String?
    public let inputKind: String?
    public let text: String?
    public let toolName: String?
    public let model: String?
    public let attachments: [Attachment]

    public var isTool: Bool { kind == "tool_use" || kind == "tool_result" }
}

/// `show SESSION --json`.
public struct SessionDetail: Decodable, Sendable {
    public let session: SessionInfo
    public let events: [SessionEvent]
}

/// `stats --json` (plain form): per day, per repo, per weekday × hour.
public struct StatsSummary: Decodable, Sendable {
    public struct Counts: Decodable, Hashable, Sendable {
        public let sessions: Int
        public let human: Int
        public let agent: Int
    }
    public struct Day: Decodable, Hashable, Sendable {
        public let day: String
        public let sessions: Int
        public let human: Int
        public let agent: Int
    }
    public struct Repo: Decodable, Hashable, Sendable {
        public let repo: String
        public let sessions: Int
        public let human: Int
        public let agent: Int
    }
    public struct WeekdayHour: Decodable, Hashable, Sendable {
        /// 0 = Monday.
        public let weekday: Int
        public let day: String
        public let hour: Int
        public let sessions: Int
        public let human: Int
        public let agent: Int
    }
    public let since: Date
    public let until: Date
    public let total: Counts
    public let byDay: [Day]
    public let byRepo: [Repo]
    public let byWeekdayHour: [WeekdayHour]
}

/// `stats --period P --by D --json`: a time series period × key.
public struct StatsSeries: Decodable, Sendable {
    public struct Cell: Decodable, Hashable, Sendable {
        public let key: String
        public let sessions: Int
        public let human: Int
        public let agent: Int
    }
    public struct Bucket: Decodable, Hashable, Sendable {
        public let start: Date
        public let label: String
        public let sessions: Int
        public let human: Int
        public let agent: Int
        public let cells: [Cell]
    }
    public let period: String
    public let by: String
    public let since: Date
    public let until: Date
    public let keys: [String]
    public let buckets: [Bucket]
    public let byKey: [Cell]
    public let total: StatsSummary.Counts
}

/// One block of activity of a day (`stats --workday --blocks`, `stats --day`).
public struct ActivityBlock: Decodable, Hashable, Sendable {
    public struct ProjectTime: Decodable, Hashable, Sendable {
        public let project: String
        public let seconds: Int
        public let human: Int
        public let agent: Int
    }
    public struct BlockSession: Decodable, Hashable, Sendable {
        public let id: String
        public let title: String?
        public let project: String
        public let firstEvent: String?
        public let start: Date
        public let end: Date
        public let human: Int
        public let agent: Int
    }
    public let start: Date
    public let end: Date
    public let startLocal: String
    public let endLocal: String
    public let seconds: Int
    public let events: Int
    public let single: Bool
    // only in `stats --day`
    public let pauseSeconds: Int?
    public let human: Int?
    public let agent: Int?
    public let projects: [ProjectTime]?
    public let sessions: [BlockSession]?
}

/// `stats --workday [--blocks] --json`: first and last activity per day.
public struct Workdays: Decodable, Sendable {
    public struct Day: Decodable, Hashable, Sendable {
        public let day: String
        public let weekday: String
        public let first: Date?
        public let last: Date?
        public let firstLocal: String?
        public let lastLocal: String?
        public let spanSeconds: Int
        public let weekend: Bool
        public let holiday: String?
        public let targetSeconds: Int
        public let diffSeconds: Int
        public let blocks: [ActivityBlock]?
        public let blockSeconds: Int?
        public let breakSeconds: Int?
        public let sessions: Int
        public let human: Int
        public let agent: Int
        public var active: Bool { first != nil }
    }
    public struct Total: Decodable, Hashable, Sendable {
        public let days: Int
        public let workdays: Int
        public let holidays: Int
        public let spanSeconds: Int
        public let targetSeconds: Int
        public let balanceSeconds: Int
    }
    public let since: Date
    public let until: Date
    public let dayStart: String
    public let humanOnly: Bool
    public let workHours: String
    public let targetSeconds: Int
    public let holidayRegion: String?
    public let blockGapSeconds: Int
    public let days: [Day]
    public let total: Total
}

/// `stats --day DATE --json`: one day chronologically.
public struct DayReport: Decodable, Sendable {
    public struct ProjectTime: Decodable, Hashable, Sendable {
        public let project: String
        public let rawSeconds: Int
        public let roundedSeconds: Int
        public let human: Int
        public let agent: Int
    }
    public struct CategoryTime: Decodable, Hashable, Sendable {
        public let category: String
        public let rawSeconds: Int
        public let roundedSeconds: Int
    }
    public struct Worktime: Decodable, Hashable, Sendable {
        public let rawSeconds: Int
        public let roundedSeconds: Int
        public let gridSeconds: Int
        public let rounding: String
        public let roundingScope: String
    }
    public let day: String
    public let weekday: String
    public let humanOnly: Bool
    public let first: Date?
    public let last: Date?
    public let firstLocal: String?
    public let lastLocal: String?
    public let spanSeconds: Int
    public let blockSeconds: Int
    public let breakSeconds: Int
    public let targetSeconds: Int
    public let diffSeconds: Int
    public let holiday: String?
    public let weekend: Bool
    public let human: Int
    public let agent: Int
    public let sessions: Int
    public let blocks: [ActivityBlock]
    public let projects: [ProjectTime]
    public let categories: [CategoryTime]
    public let worktime: Worktime
}

/// One event of `timeline --fields ts,project --json`.
public struct TimelineEvent: Decodable, Hashable, Sendable {
    public let ts: Date
    public let project: String?
}

/// One memory file of `memory --json` (now or at a time).
public struct MemoryFile: Decodable, Hashable, Sendable, Identifiable {
    public let project: String
    public let projectKey: String
    public let harness: String
    public let path: String
    public let sha256: String?
    public let bytes: Int
    public let deleted: Bool
    public let changed: Date
    public let versions: Int
    public var id: String { "\(projectKey)/\(path)" }
}

/// One version of `memory --history FILE --json`, oldest first.
public struct MemoryVersion: Decodable, Hashable, Sendable, Identifiable {
    public let project: String
    public let projectKey: String
    public let path: String
    public let ts: Date
    public let observedAt: Date?
    public let sha256: String?
    public let bytes: Int
    public let deleted: Bool
    public var id: String { "\(projectKey)/\(path)@\(ts.timeIntervalSince1970)" }
}

/// `blob HASH --json`: metadata of an attachment.
public struct BlobInfo: Decodable, Sendable {
    public let sha256: String
    public let mediaType: String
    public let bytes: Int
    public let storedBytes: Int
}

/// `status --json`.
public struct ArchiveStatus: Decodable, Sendable {
    public let db: String?
    public let dbSizeBytes: Int
    public let pageSize: Int
    public let schemaVersion: Int
    public let binarySchemaVersion: Int
    public let migrationPending: Bool
    public let sessions: Int
    public let events: Int
    public let sources: Int
    public let rawRows: Int
    public let rawBytes: Int
    public let rawRatio: Double
    public let blobs: Int
    public let blobBytes: Int
    public let metaRows: Int
    public let memoryProjects: Int
    public let memoryFiles: Int
    public let memoryVersions: Int
    public let lastIngest: [String: Date]
    public let backupDir: String?
    public let lastBackup: String?
    public let lastBackupAt: Date?
    public let lastBackupState: String?
    public let log: String?
    public let recentErrorsTotal: Int
    public let olderErrors: Int
}

/// `config show --json`: the effective configuration with the origin of
/// each value.
public struct ConfigShow: Decodable, Sendable {
    public struct Value: Decodable, Hashable, Sendable, Identifiable {
        public let key: String
        public let value: String?
        public let origin: String
        public var id: String { key }
        /// The table of the key (`backup` of `backup.dir`), empty for the top level.
        public var table: String {
            guard let dot = key.lastIndex(of: ".") else { return "" }
            return String(key[..<dot])
        }
        public var name: String {
            guard let dot = key.lastIndex(of: ".") else { return key }
            return String(key[key.index(after: dot)...])
        }
    }
    public let file: String
    public let fileOrigin: String
    public let loaded: Bool
    public let problems: [String]
    public let values: [Value]

    public func value(_ key: String) -> String? { values.first { $0.key == key }?.value }
}
