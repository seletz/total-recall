import Foundation

/// The error object every read command writes to stdout with `--json`:
/// `{"error":{"kind":"<kind>","message":"<message>"}}` (design: Ausgabevertrag
/// der Lesebefehle). `kind` is one of usage, config, db, sql, not_found,
/// ambiguous, internal.
public struct CLIError: Error, Equatable, Sendable, Decodable, CustomStringConvertible {
    public let kind: String
    public let message: String

    public init(kind: String, message: String) {
        self.kind = kind
        self.message = message
    }

    public var description: String { "\(kind): \(message)" }

    private struct Envelope: Decodable { let error: CLIError }

    /// Decodes the error object from the stdout of a failed call, if it is one.
    static func parse(_ data: Data) -> CLIError? {
        try? JSONDecoder().decode(Envelope.self, from: data).error
    }
}

/// Runs one `total-recall` command and returns its stdout. The arguments
/// never include the binary. Implementations: ``ProcessRunner`` (the real
/// CLI as a subprocess) and, in the tests, a runner over recorded fixtures.
public protocol CommandRunner: Sendable {
    func run(_ arguments: [String]) async throws -> Data
}

/// Runs the CLI as a subprocess. The environment is the app's, plus the
/// overrides (the demo sets HOME and PATH so that the CLI finds the
/// synthetic home).
public struct ProcessRunner: CommandRunner {
    public let executable: URL
    public let environment: [String: String]

    public init(executable: URL, environment: [String: String] = [:]) {
        self.executable = executable
        self.environment = environment
    }

    /// Finds `total-recall` on PATH (of the overrides, else of the process),
    /// then in the usual Homebrew locations.
    public static func locate(environment: [String: String] = [:]) -> URL? {
        let env = ProcessInfo.processInfo.environment.merging(environment) { _, new in new }
        var dirs = (env["PATH"] ?? "").split(separator: ":").map(String.init)
        dirs += ["/opt/homebrew/bin", "/usr/local/bin"]
        for dir in dirs {
            let url = URL(fileURLWithPath: dir).appendingPathComponent("total-recall")
            if FileManager.default.isExecutableFile(atPath: url.path) { return url }
        }
        return nil
    }

    public func run(_ arguments: [String]) async throws -> Data {
        let executable = self.executable
        let env = ProcessInfo.processInfo.environment.merging(environment) { _, new in new }
        return try await withCheckedThrowingContinuation { cont in
            let p = Process()
            p.executableURL = executable
            p.arguments = arguments
            p.environment = env
            let out = Pipe()
            let err = Pipe()
            p.standardOutput = out
            p.standardError = err
            p.standardInput = FileHandle.nullDevice
            // Read stdout and stderr concurrently, so a large output never
            // blocks the child on a full pipe.
            let group = DispatchGroup()
            nonisolated(unsafe) var stdout = Data()
            nonisolated(unsafe) var stderr = Data()
            group.enter()
            DispatchQueue.global().async {
                stdout = out.fileHandleForReading.readDataToEndOfFile()
                group.leave()
            }
            group.enter()
            DispatchQueue.global().async {
                stderr = err.fileHandleForReading.readDataToEndOfFile()
                group.leave()
            }
            p.terminationHandler = { proc in
                group.notify(queue: .global()) {
                    if proc.terminationStatus == 0 {
                        cont.resume(returning: stdout)
                    } else if let e = CLIError.parse(stdout) {
                        cont.resume(throwing: e)
                    } else {
                        let msg = String(decoding: stderr, as: UTF8.self)
                            .trimmingCharacters(in: .whitespacesAndNewlines)
                        cont.resume(throwing: CLIError(kind: "internal", message: msg.isEmpty ? "exit \(proc.terminationStatus)" : msg))
                    }
                }
            }
            do {
                try p.run()
            } catch {
                cont.resume(throwing: CLIError(kind: "internal", message: "cannot start \(executable.path): \(error.localizedDescription)"))
            }
        }
    }
}

/// The JSON decoder for every CLI output: snake_case keys, timestamps as
/// ISO 8601 UTC with milliseconds (`2026-10-31T22:21:58.123Z`) or with an
/// offset (`ts_local`).
public enum CLIDecoder {
    public static func make() -> JSONDecoder {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        d.dateDecodingStrategy = .custom { decoder in
            let c = try decoder.singleValueContainer()
            let s = try c.decode(String.self)
            if let date = parseTimestamp(s) { return date }
            throw DecodingError.dataCorruptedError(in: c, debugDescription: "not a timestamp: \(s)")
        }
        return d
    }

    /// Parses an ISO 8601 timestamp with or without fractional seconds.
    public static func parseTimestamp(_ s: String) -> Date? {
        let withFraction = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
        if let d = try? withFraction.parse(s) { return d }
        return try? Date.ISO8601FormatStyle().parse(s)
    }
}
