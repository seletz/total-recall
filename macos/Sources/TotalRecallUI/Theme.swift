import SwiftUI
import TotalRecallCore

extension Color {
    /// A colour from "#rrggbb"; grey when invalid.
    init(hex: String?) {
        guard let hex, hex.count == 7, let v = Int(hex.dropFirst(), radix: 16) else {
            self = .gray
            return
        }
        self = Color(red: Double((v >> 16) & 0xff) / 255, green: Double((v >> 8) & 0xff) / 255, blue: Double(v & 0xff) / 255)
    }
}

/// Colours of the app: actors, diff lines, activity bars. System colours,
/// so light and dark mode follow the system.
enum Theme {
    static func actor(_ a: String?) -> Color {
        switch a {
        case "human": .blue
        case "agent": .purple
        case "tool": .orange
        case "peer": .teal
        case "automation": .brown
        default: .secondary
        }
    }
    static let core = Color.accentColor
    static let outside = Color.orange
    static let night = Color.indigo
    static let holiday = Color.green
    static let weekend = Color.secondary.opacity(0.08)
    static let added = Color.green
    static let removed = Color.red
    static let hit = Color.yellow.opacity(0.35)
}

/// A small coloured square as the TUI's "■" project marker.
struct Swatch: View {
    let hex: String?
    var size: CGFloat = 10
    var body: some View {
        RoundedRectangle(cornerRadius: 2).fill(Color(hex: hex)).frame(width: size, height: size)
    }
}

/// Snippet text with the FTS hits highlighted.
struct SnippetText: View {
    let snippet: String
    var body: some View {
        Fmt.snippetParts(snippet.replacingOccurrences(of: "\n", with: " ")).reduce(Text("")) { acc, p in
            acc + (p.hit ? Text(p.text).bold().foregroundColor(.primary) : Text(p.text).foregroundColor(.secondary))
        }
    }
}

/// A key hint chip of the footer and the help: "⌘1", "e".
struct KeyCap: View {
    let key: String
    var body: some View {
        Text(key)
            .font(.system(size: 10, weight: .medium, design: .monospaced))
            .padding(.horizontal, 4).padding(.vertical, 1)
            .background(RoundedRectangle(cornerRadius: 3).fill(Color.secondary.opacity(0.15)))
    }
}

/// A row of key hints at the bottom of a view, as the TUI's footer.
struct KeyHints: View {
    let hints: [(String, String)]
    var body: some View {
        HStack(spacing: 12) {
            ForEach(Array(hints.enumerated()), id: \.offset) { _, h in
                HStack(spacing: 4) {
                    KeyCap(key: h.0)
                    Text(h.1).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
        }
        .font(.caption)
        .lineLimit(1)
    }
}

/// A capsule toggle as the TUI's "[event] session memory".
struct ChipToggle: View {
    let title: String
    let on: Bool
    var key: String?
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            HStack(spacing: 4) {
                Text(title)
                if let key { Text(key).font(.caption2.monospaced()).foregroundStyle(on ? Color.white.opacity(0.8) : Color.secondary) }
            }
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background(Capsule().fill(on ? Color.accentColor : Color.secondary.opacity(0.12)))
            .foregroundStyle(on ? Color.white : Color.primary)
        }
        .buttonStyle(.plain)
    }
}

/// A labelled filter value as "actor: all".
struct FilterLabel: View {
    let label: String
    let value: String
    var key: String?
    var body: some View {
        HStack(spacing: 4) {
            Text(label + ":").foregroundStyle(.secondary)
            Text(value).fontWeight(.medium)
            if let key { KeyCap(key: key) }
        }
        .font(.callout)
    }
}

/// A clock time and a weekday from an ISO day ("Mon 2026-06-01").
func weekdayDay(_ wd: String, _ day: String) -> String { "\(wd) \(day)" }
