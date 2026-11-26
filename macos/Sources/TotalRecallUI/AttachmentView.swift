import AppKit
import PDFKit
import SwiftUI
import TotalRecallCore

/// The attachment viewer: tabs, the image or document, metadata, the CLI
/// line that writes the attachment to a file.
struct AttachmentView: View {
    let app: AppState
    @Bindable var model: AttachmentModel

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Image(systemName: "paperclip").foregroundStyle(.secondary)
                    Text(model.title).font(.headline).lineLimit(1)
                    Text("attachments").foregroundStyle(.secondary)
                    Spacer()
                }
                tabs
                meta
            }
            .padding(12)
            Divider()
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            Divider()
            footer
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(phases: .down) { p in
            switch p.key {
            case .leftArrow: Task { await model.select(model.index - 1) }; return .handled
            case .rightArrow: Task { await model.select(model.index + 1) }; return .handled
            default: break
            }
            if p.characters == "o" { openExternally(); return .handled }
            return .ignored
        }
    }

    var tabs: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(Array(model.attachments.enumerated()), id: \.offset) { i, a in
                    Button { Task { await model.select(i) } } label: {
                        Text("\(i + 1) \(a.mediaType) \(Fmt.bytes(a.bytes))")
                            .padding(.horizontal, 10).padding(.vertical, 3)
                            .background(Capsule().fill(i == model.index ? Color.accentColor : Color.secondary.opacity(0.12)))
                            .foregroundStyle(i == model.index ? Color.white : Color.primary)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    var meta: some View {
        HStack(spacing: 14) {
            if let a = model.current {
                Text(a.mediaType).fontWeight(.medium)
                Text(Fmt.bytes(a.bytes))
                Text("\(model.index + 1) of \(model.attachments.count)")
                Text(a.shortHash).font(.callout.monospaced())
            }
        }
        .font(.callout).foregroundStyle(.secondary)
    }

    @ViewBuilder var content: some View {
        if let e = model.error {
            Text(e).foregroundStyle(.red).padding()
        } else if let data = model.data, let a = model.current {
            if a.isImage, let img = NSImage(data: data) {
                Image(nsImage: img).resizable().interpolation(.high).scaledToFit()
                    .frame(maxWidth: min(img.size.width * 4, 900), maxHeight: min(img.size.height * 4, 600))
                    .padding(16)
            } else if a.mediaType == "application/pdf", let doc = PDFDocument(data: data) {
                AttachmentPDFInfo(doc: doc)
            } else {
                VStack(spacing: 6) {
                    Image(systemName: "doc").font(.largeTitle).foregroundStyle(.secondary)
                    Text("\(a.mediaType), \(Fmt.bytes(a.bytes))").foregroundStyle(.secondary)
                    Text("o opens it with the system viewer").font(.caption).foregroundStyle(.secondary)
                }
            }
        } else {
            ProgressView()
        }
    }

    var footer: some View {
        HStack(spacing: 8) {
            Text(model.blobCommand).font(.callout.monospaced()).textSelection(.enabled)
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(model.blobCommand, forType: .string)
            } label: { Image(systemName: "doc.on.doc") }
            .buttonStyle(.borderless).help("Copy")
            Spacer()
            Text("\(model.attachments.count) attachment\(model.attachments.count == 1 ? "" : "s")")
                .font(.caption).foregroundStyle(.secondary)
            Button("Open with system viewer", action: openExternally).controlSize(.small)
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
    }

    /// Writes the bytes to a temporary file and opens it.
    func openExternally() {
        guard let data = model.data, let a = model.current else { return }
        let ext = a.mediaType == "application/pdf" ? "pdf" : (a.mediaType.split(separator: "/").last.map(String.init) ?? "bin")
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("total-recall", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent("\(a.shortHash).\(ext)")
        do {
            try data.write(to: url)
            NSWorkspace.shared.open(url)
        } catch {}
    }
}

/// A PDF: thumbnail of the first page and the metadata of the TUI.
struct AttachmentPDFInfo: View {
    let doc: PDFDocument

    var attrs: [(String, String)] {
        let a = doc.documentAttributes ?? [:]
        var rows: [(String, String)] = [("pages", "\(doc.pageCount)")]
        if let t = a[PDFDocumentAttribute.titleAttribute] as? String, !t.isEmpty { rows.append(("title", t)) }
        if let t = a[PDFDocumentAttribute.authorAttribute] as? String, !t.isEmpty { rows.append(("author", t)) }
        if let t = a[PDFDocumentAttribute.producerAttribute] as? String, !t.isEmpty { rows.append(("producer", t)) }
        return rows
    }

    var body: some View {
        HStack(alignment: .top, spacing: 24) {
            if let page = doc.page(at: 0) {
                Image(nsImage: page.thumbnail(of: NSSize(width: 260, height: 340), for: .mediaBox))
                    .resizable().scaledToFit().frame(maxWidth: 260, maxHeight: 340)
                    .overlay(Rectangle().stroke(Color.secondary.opacity(0.4)))
                    .shadow(radius: 2)
            }
            VStack(alignment: .leading, spacing: 6) {
                Text("PDF \(doc.majorVersion).\(doc.minorVersion)").font(.title3.bold()).padding(.bottom, 4)
                ForEach(attrs, id: \.0) { k, v in
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        Text(k).foregroundStyle(.secondary).frame(width: 70, alignment: .leading)
                        Text(v).textSelection(.enabled)
                    }
                }
            }
            Spacer(minLength: 0)
        }
        .padding(24)
        .frame(maxHeight: .infinity, alignment: .top)
    }
}
