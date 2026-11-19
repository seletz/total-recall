import SwiftUI
import TotalRecallCore

/// The activity view: per day first and last activity, span, difference to
/// the target and a timeline bar over the day.
struct ActivityView: View {
    let app: AppState
    @Bindable var model: ActivityModel

    var body: some View {
        GeometryReader { geo in
            let compact = geo.size.width < 1000 || geo.size.height <= 600
            VStack(alignment: .leading, spacing: 8) {
                topBar(compact: compact)
                Text(model.headline)
                    .font(.system(compact ? .callout : .body, weight: .semibold))
                    .foregroundStyle(ratingColor)
                    .lineLimit(1).minimumScaleFactor(0.8)
                if let e = model.error { Text(e).foregroundStyle(.red).font(.callout) }
                ActivityRows(app: app, model: model, compact: compact)
                    .frame(maxHeight: .infinity)
                Divider()
                if model.projects { legend }
                ActivityFigures(model: model, compact: compact)
                status
            }
            .padding(12)
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(phases: .down) { press in
            switch press.characters {
            case "d": model.setKind(.day)
            case "w": model.setKind(.week)
            case "m": model.setKind(.month)
            case "h": model.allEvents.toggle()
            case "p": model.projects.toggle()
            case "o": model.newestTop.toggle(); return .handled
            case "c": model.current()
            case "l": model.last()
            case "K": model.select(-1); return .handled
            case "J": model.select(1); return .handled
            case "k": model.slide(-1)
            case "j": model.slide(1)
            default:
                switch press.key {
                case .upArrow: model.slide(-1)
                case .downArrow: model.slide(1)
                case .leftArrow: model.jump(-1)
                case .rightArrow: model.jump(1)
                case .return: Task { await app.openDay() }; return .handled
                default: return .ignored
                }
            }
            reload()
            return .handled
        }
    }

    func reload() { Task { await model.load() } }

    var ratingColor: Color {
        switch model.rating {
        case "under": .orange
        case "over": .red
        case "ok": .green
        default: .secondary
        }
    }

    func topBar(compact: Bool) -> some View {
        HStack(spacing: 12) {
            Picker("Period", selection: Binding(get: { model.kind }, set: { model.setKind($0); reload() })) {
                Text("day").tag(ActivityModel.Kind.day)
                Text("week").tag(ActivityModel.Kind.week)
                Text("month").tag(ActivityModel.Kind.month)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
            Picker("Count", selection: Binding(get: { model.allEvents }, set: { model.allEvents = $0; reload() })) {
                Text("human").tag(false)
                Text("all").tag(true)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
            Picker("Bars", selection: Binding(get: { model.projects }, set: { model.projects = $0; reload() })) {
                Text("hours").tag(false)
                Text("projects").tag(true)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
            if !compact { Toggle("Newest on top", isOn: $model.newestTop).toggleStyle(.switch).controlSize(.small) }
            Spacer()
            Button { model.jump(-1); reload() } label: { Image(systemName: "chevron.left") }
            Text(model.title).font(.callout.monospacedDigit()).lineLimit(1).minimumScaleFactor(0.7)
            Button { model.jump(1); reload() } label: { Image(systemName: "chevron.right") }
            Button("This") { model.current(); reload() }
            Button("Last") { model.last(); reload() }
        }
    }

    var legend: some View {
        let colors = model.projectColors
        return HStack(spacing: 14) {
            Text("projects").foregroundStyle(.secondary)
            ForEach(model.projectCells, id: \.project) { p in
                HStack(spacing: 4) {
                    Swatch(hex: colors[p.project])
                    Text("\(p.project.isEmpty ? "(none)" : p.project) \(p.cells)")
                }
            }
            Spacer(minLength: 0)
        }
        .font(.caption)
    }

    var status: some View {
        let before = model.shownDays.count - model.windowDays.count
        let hol = model.totals.holidays.count
        return Text("\(model.windowDays.count) days + \(max(0, before)) before · took \(model.tookMs) ms · \(model.allEvents ? "all events" : "human only") · \(hol) holiday\(hol == 1 ? "" : "s")")
            .font(.caption).foregroundStyle(.secondary)
    }
}

/// The rows with their column header and the hour axis.
struct ActivityRows: View {
    let app: AppState
    let model: ActivityModel
    let compact: Bool

    var colDay: CGFloat { compact ? 100 : 118 }
    var colTime: CGFloat { compact ? 50 : 56 }
    var colLast: CGFloat { compact ? 82 : 92 }
    var colSpan: CGFloat { compact ? 52 : 60 }

    var body: some View {
        let rowH: CGFloat = compact ? 17 : 19
        VStack(spacing: 2) {
            header
            ScrollView {
                VStack(spacing: 1) {
                    ForEach(model.shownDays, id: \.day) { d in
                        row(d, h: rowH)
                    }
                }
            }
            .scrollIndicators(.hidden)
            .defaultScrollAnchor(model.newestTop ? .top : .bottom)
        }
    }

    var header: some View {
        HStack(spacing: 8) {
            Text("day \(model.newestTop ? "↑" : "↓")").frame(width: colDay, alignment: .leading)
            Text("first").frame(width: colTime, alignment: .leading)
            Text("last").frame(width: colLast, alignment: .leading)
            Text("span").frame(width: colSpan, alignment: .trailing)
            Text("diff").frame(width: colSpan, alignment: .trailing)
            ActivityAxis(model: model, labels: true)
        }
        .font(.caption).foregroundStyle(.secondary)
        .padding(.horizontal, 6)
    }

    func row(_ d: Workdays.Day, h: CGFloat) -> some View {
        let selected = model.selected == d.day
        let inWin = model.inWindow(d)
        return HStack(spacing: 8) {
            Text(weekdayDay(d.weekday, d.day)).frame(width: colDay, alignment: .leading)
            Text(d.active ? model.clockLabel(d.first, day: d.day) : "-").frame(width: colTime, alignment: .leading)
            Text(d.active ? model.clockLabel(d.last, day: d.day) : "-").frame(width: colLast, alignment: .leading)
            Text(d.active ? Fmt.duration(d.spanSeconds) : "-").frame(width: colSpan, alignment: .trailing)
            Text(d.active ? Fmt.duration(d.diffSeconds, signed: true) : "-")
                .foregroundStyle(d.active ? (d.diffSeconds >= 0 ? Theme.added : Theme.removed) : Color.secondary)
                .frame(width: colSpan, alignment: .trailing)
            ActivityBar(model: model, day: d)
        }
        .font(.system(size: compact ? 11 : 12).monospacedDigit())
        .frame(height: h)
        .padding(.horizontal, 6)
        .background(RoundedRectangle(cornerRadius: 4).fill(rowTint(d, selected: selected)))
        .overlay(alignment: .leading) {
            if selected { Rectangle().fill(Color.accentColor).frame(width: 3) }
        }
        .opacity(inWin ? 1 : 0.5)
        .contentShape(Rectangle())
        .onTapGesture(count: 2) { model.selected = d.day; Task { await app.openDay(d.day) } }
        .onTapGesture { model.selected = d.day }
    }

    func rowTint(_ d: Workdays.Day, selected: Bool) -> Color {
        if selected { return Color.accentColor.opacity(0.16) }
        if d.holiday != nil { return Theme.holiday.opacity(0.10) }
        if d.weekend { return Theme.weekend }
        return .clear
    }
}

/// The hour axis: grid lines (and labels) every four hours from the day start.
struct ActivityAxis: View {
    let model: ActivityModel
    let labels: Bool

    var body: some View {
        GeometryReader { g in
            let ticks = stride(from: 0, to: 24, by: 4).map { model.dayStart / 60 + $0 }
            ForEach(ticks, id: \.self) { h in
                let x = CGFloat(h * 60 - model.dayStart) / 1440 * g.size.width
                Text(String(format: "%02d", h % 24))
                    .font(.caption).foregroundStyle(.secondary)
                    .position(x: x + 8, y: g.size.height / 2)
            }
        }
        .frame(height: 14)
    }
}

/// The timeline bar of one day.
struct ActivityBar: View {
    let model: ActivityModel
    let day: Workdays.Day

    var body: some View {
        Canvas { ctx, size in
            let total = 1440.0
            let start = Double(model.dayStart)
            func x(_ m: Double) -> CGFloat { CGFloat((m - start) / total) * size.width }
            let midY = size.height / 2
            // core hours band and the 4h grid
            ctx.fill(Path(CGRect(x: x(Double(model.workStart)), y: 1, width: x(Double(model.workEnd)) - x(Double(model.workStart)), height: size.height - 2)),
                     with: .color(Theme.core.opacity(0.08)))
            for i in 0..<6 {
                let gx = x(start + Double(i) * 240)
                ctx.fill(Path(CGRect(x: gx, y: 0, width: 0.5, height: size.height)), with: .color(.secondary.opacity(0.25)))
            }
            if !day.active {
                return
            }
            if let f = day.first, let l = day.last {
                let a = Double(model.minutes(f, day: day.day)), b = Double(model.minutes(l, day: day.day))
                ctx.fill(Path(CGRect(x: x(a), y: midY - 0.5, width: max(1, x(b) - x(a)), height: 1)), with: .color(.secondary.opacity(0.7)))
            }
            if model.projects, let slots = model.slots[day.day] {
                let colors = model.projectColors
                for (s, p) in slots {
                    let a = start + Double(s) * 15
                    ctx.fill(Path(CGRect(x: x(a), y: 3, width: max(1, x(a + 15) - x(a)), height: size.height - 6)),
                             with: .color(Color(hex: colors[p])))
                }
            } else {
                for b in day.blocks ?? [] {
                    let a = Double(model.minutes(b.start, day: day.day))
                    let e = max(Double(model.minutes(b.end, day: day.day)), a + 4)
                    var cuts = [a, e]
                    for c in [1440.0, Double(model.workStart), Double(model.workEnd)] where c > a && c < e { cuts.append(c) }
                    cuts.sort()
                    for i in 0..<(cuts.count - 1) {
                        let m = (cuts[i] + cuts[i + 1]) / 2
                        let color: Color = m >= 1440 ? Theme.night
                            : (m >= Double(model.workStart) && m < Double(model.workEnd) ? Theme.core : Theme.outside)
                        ctx.fill(Path(CGRect(x: x(cuts[i]), y: 3, width: max(1.5, x(cuts[i + 1]) - x(cuts[i])), height: size.height - 6)),
                                 with: .color(color))
                    }
                }
            }
        }
        .overlay {
            if !day.active, let h = day.holiday {
                Text(h).font(.caption).foregroundStyle(Theme.holiday).frame(maxWidth: .infinity, alignment: .leading).padding(.leading, 4)
            } else if day.active, let h = day.holiday {
                Text(h).font(.caption2).foregroundStyle(Theme.holiday).frame(maxWidth: .infinity, alignment: .leading).padding(.leading, 4)
            }
        }
    }
}

/// First, last and span of the window as median, mean, min and max.
struct ActivityFigures: View {
    let model: ActivityModel
    let compact: Bool

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 24, verticalSpacing: 1) {
            ForEach(model.figures, id: \.label) { f in
                GridRow {
                    Text(f.label).foregroundStyle(.secondary).frame(width: 40, alignment: .leading)
                    cell("median", f.median); cell("mean", f.mean); cell("min", f.min); cell("max", f.max)
                }
            }
        }
        .font(.system(size: compact ? 11 : 12).monospacedDigit())
    }

    func cell(_ k: String, _ v: String) -> some View {
        HStack(spacing: 6) { Text(k).foregroundStyle(.secondary); Text(v) }
    }
}
