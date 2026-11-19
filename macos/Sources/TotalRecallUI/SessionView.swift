import AppKit
import SwiftUI
import TotalRecallCore

/// The session viewer: header, the events (tool calls on T), hits of the
/// search query with n/N, attachments with v.
struct SessionView: View {
    let app: AppState
    @Bindable var model: SessionModel

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let d = model.detail {
                SessionHeader(app: app, session: d.session, counts: true).padding(12)
                Divider()
                HStack(spacing: 12) {
                    Text(model.position).font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                    Spacer()
                    Toggle("Tool calls", isOn: $model.showTools).toggleStyle(.switch).controlSize(.small)
                    Button { model.nextHit(-1) } label: { Image(systemName: "chevron.up") }.disabled(model.hits.isEmpty)
                    Button { model.nextHit(1) } label: { Image(systemName: "chevron.down") }.disabled(model.hits.isEmpty)
                    if !model.attachments.isEmpty {
                        Button { Task { await app.openAttachments() } } label: {
                            Label("\(model.attachments.count) attachments", systemImage: "paperclip")
                        }
                    }
                }
                .padding(.horizontal, 12).padding(.vertical, 6)
                Divider()
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 8) {
                            ForEach(model.events) { e in
                                EventView(app: app, event: e, isHit: model.hits.contains(e.id), showDate: true).id(e.id)
                            }
                        }
                        .padding(12)
                    }
                    .onChange(of: model.focus) { _, f in if let f { withAnimation { proxy.scrollTo(f, anchor: .top) } } }
                    .onAppear { if let f = model.focus { proxy.scrollTo(f, anchor: .top) } }
                }
            } else if let e = model.error {
                ContentUnavailableView("Session not loaded", systemImage: "exclamationmark.triangle", description: Text(e))
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(phases: .down) { press in
            switch press.characters {
            case "n": model.nextHit(1)
            case "N": model.nextHit(-1)
            case "T": model.showTools.toggle()
            case "v": Task { await app.openAttachments() }
            case "y":
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(model.sessionID, forType: .string)
            default: return .ignored
            }
            return .handled
        }
    }
}
