import SwiftUI
import TotalRecallCore

/// The day view: one day chronologically with its blocks and sessions, the
/// projects and the categories.
struct DayView: View {
    let app: AppState
    @Bindable var model: DayModel

    var body: some View {
        GeometryReader { geo in
            let compact = geo.size.height <= 600
            VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: 6) {
                    header
                    Text(model.summary).font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                    if let e = model.error { Text(e).foregroundStyle(.red) }
                }
                .padding(12)
                Divider()
                if let r = model.report {
                    ScrollViewReader { proxy in
                        ScrollView {
                            VStack(alignment: .leading, spacing: 16) {
                                if !compact && !r.blocks.isEmpty { DayStrip(model: model, report: r) }
                                blocks(r)
                                DayProjects(model: model, report: r)
                                DayCategories(report: r)
                            }
                            .padding(12)
                        }
                        .onChange(of: model.selectedBlock) { _, b in withAnimation { proxy.scrollTo("block\(b)") } }
                    }
                } else {
                    ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(phases: .down) { press in
            let n = model.report?.blocks.count ?? 0
            switch press.key {
            case .upArrow: model.selectedBlock = max(0, model.selectedBlock - 1)
            case .downArrow: model.selectedBlock = min(max(0, n - 1), model.selectedBlock + 1)
            case .leftArrow: Task { await model.step(-1) }
            case .rightArrow: Task { await model.step(1) }
            case .space: model.toggleExpanded()
            case .return: openSelected()
            default:
                switch press.characters {
                case "k": model.selectedBlock = max(0, model.selectedBlock - 1)
                case "j": model.selectedBlock = min(max(0, n - 1), model.selectedBlock + 1)
                case "h": model.allEvents.toggle(); Task { await model.load() }
                default: return .ignored
                }
            }
            return .handled
        }
    }

    /// enter: the first session of the selected block when it is expanded,
    /// otherwise expand it.
    func openSelected() {
        guard let r = model.report, r.blocks.indices.contains(model.selectedBlock) else { return }
        let b = r.blocks[model.selectedBlock]
        if model.expanded.contains(model.selectedBlock), let s = b.sessions?.first {
            Task { await app.openSession(s.id, focus: s.firstEvent) }
        } else {
            model.toggleExpanded()
        }
    }

    var header: some View {
        HStack(spacing: 10) {
            Button { Task { await model.step(-1) } } label: { Image(systemName: "chevron.left") }
            Text("\(model.report?.weekday ?? "") \(model.day)").font(.title3.weight(.semibold).monospacedDigit())
            Button { Task { await model.step(1) } } label: { Image(systemName: "chevron.right") }
            if let h = model.report?.holiday { Text(h).foregroundStyle(Theme.holiday) }
            Spacer()
            Picker("Count", selection: Binding(get: { model.allEvents }, set: { model.allEvents = $0; Task { await model.load() } })) {
                Text("human events only").tag(false)
                Text("all events").tag(true)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
        }
    }

    func blocks(_ r: DayReport) -> some View {
        let colors = model.colors
        return VStack(alignment: .leading, spacing: 2) {
            ForEach(Array(r.blocks.enumerated()), id: \.offset) { i, b in
                let selected = i == model.selectedBlock
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 12) {
                        Text("\(i + 1)").frame(width: 18, alignment: .trailing).foregroundStyle(.secondary)
                        Text("\(model.label(b.start))–\(model.label(b.end))").frame(width: 150, alignment: .leading)
                        Text(Fmt.short(b.seconds)).frame(width: 56, alignment: .trailing)
                        Text("\(b.events) ev").frame(width: 56, alignment: .trailing)
                        Text("\(b.sessions?.count ?? 0) sessions").frame(width: 80, alignment: .leading).foregroundStyle(.secondary)
                        HStack(spacing: 10) {
                            ForEach(b.projects ?? [], id: \.project) { p in
                                HStack(spacing: 4) {
                                    Swatch(hex: colors[p.project])
                                    Text("\(p.project) \(Fmt.short(p.seconds))")
                                }
                            }
                        }
                        .lineLimit(1)
                        Spacer(minLength: 0)
                        Image(systemName: model.expanded.contains(i) ? "chevron.down" : "chevron.right")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                    .font(.callout.monospacedDigit())
                    .padding(.vertical, 4).padding(.horizontal, 6)
                    .background(RoundedRectangle(cornerRadius: 5).fill(selected ? Color.accentColor.opacity(0.16) : Color.clear))
                    .overlay(alignment: .leading) { if selected { Rectangle().fill(Color.accentColor).frame(width: 3) } }
                    .contentShape(Rectangle())
                    .onTapGesture { model.selectedBlock = i; model.toggleExpanded() }
                    if model.expanded.contains(i) {
                        VStack(alignment: .leading, spacing: 1) {
                            ForEach(b.sessions ?? [], id: \.id) { s in
                                sessionRow(s, colors: colors)
                            }
                        }
                        .padding(.leading, 40)
                    }
                    if i < r.blocks.count - 1, let p = r.blocks[i + 1].pauseSeconds ?? b.pauseSeconds, p > 0 {
                        Text("· \(Fmt.short(p))").font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                            .padding(.leading, 40)
                    }
                }
                .id("block\(i)")
            }
        }
    }

    func sessionRow(_ s: ActivityBlock.BlockSession, colors: [String: String]) -> some View {
        HStack(spacing: 10) {
            Text("\(model.label(s.start))–\(model.label(s.end))").frame(width: 150, alignment: .leading).foregroundStyle(.secondary)
            Text(s.title ?? s.id).lineLimit(1)
            Spacer(minLength: 8)
            Swatch(hex: colors[s.project])
            Text(s.project).foregroundStyle(.secondary)
            Text("\(s.human) human · \(s.agent) agent").foregroundStyle(.secondary).frame(width: 130, alignment: .trailing)
        }
        .font(.callout.monospacedDigit())
        .padding(.vertical, 1).padding(.horizontal, 6)
        .contentShape(Rectangle())
        .onTapGesture(count: 2) { Task { await app.openSession(s.id, focus: s.firstEvent) } }
    }
}

/// The strip of the day: the blocks from the first to the last activity,
/// each split into its projects.
struct DayStrip: View {
    let model: DayModel
    let report: DayReport

    var body: some View {
        let colors = model.colors
        let t0 = (report.first ?? report.blocks.first!.start).timeIntervalSince1970
        let t1 = max(t0 + 1, (report.last ?? report.blocks.last!.end).timeIntervalSince1970)
        VStack(spacing: 3) {
            Canvas { ctx, size in
                let span = t1 - t0
                ctx.fill(Path(CGRect(x: 0, y: size.height / 2 - 0.5, width: size.width, height: 1)), with: .color(.secondary.opacity(0.4)))
                for (i, b) in report.blocks.enumerated() {
                    let x0 = CGFloat((b.start.timeIntervalSince1970 - t0) / span) * size.width
                    let w = max(3, CGFloat((b.end.timeIntervalSince1970 - b.start.timeIntervalSince1970) / span) * size.width)
                    let total = Double(max(1, (b.projects ?? []).reduce(0) { $0 + $1.seconds }))
                    let dim = i == model.selectedBlock ? 1.0 : 0.6
                    var x = x0
                    for p in b.projects ?? [] {
                        let pw = w * CGFloat(Double(p.seconds) / total)
                        ctx.fill(Path(CGRect(x: x, y: 6, width: pw, height: size.height - 12)),
                                 with: .color(Color(hex: colors[p.project]).opacity(dim)))
                        x += pw
                    }
                    if (b.projects ?? []).isEmpty {
                        ctx.fill(Path(CGRect(x: x0, y: 6, width: w, height: size.height - 12)), with: .color(Color.accentColor.opacity(dim)))
                    }
                    if i == model.selectedBlock {
                        ctx.stroke(Path(roundedRect: CGRect(x: x0 - 1, y: 4, width: w + 2, height: size.height - 8), cornerRadius: 3),
                                   with: .color(.primary), lineWidth: 1.5)
                    }
                }
            }
            .frame(height: 38)
            HStack {
                Text(report.first.map(model.label) ?? "")
                Spacer()
                Text(report.last.map(model.label) ?? "")
            }
            .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
        }
    }
}

struct DayProjects: View {
    let model: DayModel
    let report: DayReport

    var body: some View {
        let colors = model.colors
        let ps = report.projects
        VStack(alignment: .leading, spacing: 4) {
            Text("projects").font(.headline)
            Grid(alignment: .trailing, horizontalSpacing: 26, verticalSpacing: 3) {
                GridRow {
                    Text("").gridColumnAlignment(.leading)
                    Text("time"); Text("rounded"); Text("human"); Text("agent")
                }
                .font(.caption).foregroundStyle(.secondary)
                ForEach(ps, id: \.project) { p in
                    GridRow {
                        HStack(spacing: 6) { Swatch(hex: colors[p.project]); Text(p.project) }
                        Text(Fmt.short(p.rawSeconds)); Text(Fmt.short(p.roundedSeconds))
                        Text("\(p.human)"); Text("\(p.agent)")
                    }
                }
                Divider().gridCellUnsizedAxes(.horizontal)
                GridRow {
                    Text("total").fontWeight(.semibold)
                    Text(Fmt.short(ps.reduce(0) { $0 + $1.rawSeconds }))
                    Text("-")
                    Text("\(ps.reduce(0) { $0 + $1.human })"); Text("\(ps.reduce(0) { $0 + $1.agent })")
                }
            }
            .font(.callout.monospacedDigit())
        }
    }
}

struct DayCategories: View {
    let report: DayReport

    var body: some View {
        let cs = report.categories
        VStack(alignment: .leading, spacing: 4) {
            Text("categories").font(.headline)
            Grid(alignment: .trailing, horizontalSpacing: 26, verticalSpacing: 3) {
                GridRow {
                    Text("").gridColumnAlignment(.leading)
                    Text("time"); Text("rounded")
                }
                .font(.caption).foregroundStyle(.secondary)
                ForEach(cs, id: \.category) { c in
                    GridRow { Text(c.category); Text(Fmt.short(c.rawSeconds)); Text(Fmt.short(c.roundedSeconds)) }
                }
                Divider().gridCellUnsizedAxes(.horizontal)
                GridRow {
                    Text("total").fontWeight(.semibold)
                    Text(Fmt.short(report.worktime.rawSeconds)); Text(Fmt.short(report.worktime.roundedSeconds))
                }
            }
            .font(.callout.monospacedDigit())
            Text("rounded is per category; per project it does not add up").font(.caption).foregroundStyle(.secondary)
        }
    }
}
