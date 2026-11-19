import Charts
import SwiftUI
import TotalRecallCore

/// The stats view: a stacked chart per period and key, the table per key
/// and the heatmap weekday × hour.
struct StatsView: View {
    let app: AppState
    @Bindable var model: StatsModel

    var body: some View {
        GeometryReader { geo in
            let compact = geo.size.height <= 600
            VStack(alignment: .leading, spacing: 10) {
                topBar(compact: compact)
                headline
                if let e = model.error {
                    Text(e).foregroundStyle(.red).font(.callout)
                }
                StatsChart(model: model)
                    .frame(minHeight: 120, maxHeight: .infinity)
                if model.heatmap { StatsHeatmap(model: model, compact: compact) }
                if !(model.heatmap && compact) { StatsTable(model: model) }
                Text("\(model.series?.buckets.count ?? 0) days · took \(model.tookMs) ms")
                    .font(.caption).foregroundStyle(.secondary)
            }
            .padding(12)
        }
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(phases: .down) { press in
            switch press.characters {
            case "d": model.setPeriod(.day)
            case "w": model.setPeriod(.week)
            case "m": model.setPeriod(.month)
            case "p": model.dimension = .project
            case "a": model.dimension = .agent
            case "v": model.dimension = .directory
            case "e": model.metric = StatsModel.Metric(rawValue: (model.metric.rawValue + 1) % 3)!
            case "h": model.heatmap.toggle(); return .handled
            case "-": model.window = model.window.zoomed(1)
            case "+", "=": model.window = model.window.zoomed(-1)
            default:
                if press.key == .leftArrow { model.window = model.window.shifted(-1, model.clock) }
                else if press.key == .rightArrow { model.window = model.window.shifted(1, model.clock) }
                else { return .ignored }
            }
            reload()
            return .handled
        }
    }

    func reload() { Task { await model.load() } }

    func topBar(compact: Bool) -> some View {
        HStack(spacing: 14) {
            Picker("Period", selection: Binding(get: { model.window.period }, set: { model.setPeriod($0); reload() })) {
                Text("day").tag(PeriodWindow.Period.day)
                Text("week").tag(PeriodWindow.Period.week)
                Text("month").tag(PeriodWindow.Period.month)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
            Picker("By", selection: Binding(get: { model.dimension }, set: { model.dimension = $0; reload() })) {
                Text("project").tag(StatsModel.Dimension.project)
                Text("agent").tag(StatsModel.Dimension.agent)
                Text("directory").tag(StatsModel.Dimension.directory)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
            Picker("Metric", selection: Binding(get: { model.metric }, set: { model.metric = $0 })) {
                ForEach(StatsModel.Metric.allCases, id: \.self) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
            if !compact { Toggle("Heatmap", isOn: $model.heatmap).toggleStyle(.switch).controlSize(.small) }
            Spacer()
            Button { model.window = model.window.shifted(-1, model.clock); reload() } label: { Image(systemName: "chevron.left") }
            Text(model.rangeLabel).font(.callout.monospacedDigit()).lineLimit(1).fixedSize()
            Button { model.window = model.window.shifted(1, model.clock); reload() } label: { Image(systemName: "chevron.right") }
            if !compact {
                Button { model.window = model.window.zoomed(1); reload() } label: { Image(systemName: "minus.magnifyingglass") }
                Button { model.window = model.window.zoomed(-1); reload() } label: { Image(systemName: "plus.magnifyingglass") }
            }
        }
        .controlSize(compact ? .small : .regular)
    }

    var headline: some View {
        HStack(alignment: .firstTextBaseline, spacing: 22) {
            ForEach(Array(model.headline.enumerated()), id: \.offset) { _, h in
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Text(h.value).font(.title2.weight(.semibold).monospacedDigit())
                    Text(h.label).font(.callout).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
        }
    }
}

/// One bar segment of the chart.
private struct StatsSegment: Identifiable {
    let label: String
    let key: String
    let value: Int
    var id: String { label + "\u{1}" + key }
}

struct StatsChart: View {
    let model: StatsModel

    var body: some View {
        let colors = model.colors
        let keys = model.series?.keys ?? []
        let segs: [StatsSegment] = (model.series?.buckets ?? []).flatMap { b in
            b.cells.map { c in
                StatsSegment(label: b.label, key: c.key,
                             value: model.metric.value(human: c.human, agent: c.agent, sessions: c.sessions))
            }
        }
        let labels = (model.series?.buckets ?? []).map(\.label)
        let step = max(1, labels.count / 10)
        let shown = labels.enumerated().filter { $0.offset % step == 0 }.map(\.element)
        Chart(segs) { s in
            BarMark(x: .value("Period", s.label), y: .value(model.metric.label, s.value))
                .foregroundStyle(by: .value("Key", s.key))
        }
        .chartForegroundStyleScale(domain: keys, range: keys.map { Color(hex: colors[$0]) })
        .chartXAxis {
            AxisMarks(values: shown) { v in
                AxisGridLine()
                AxisTick()
                AxisValueLabel {
                    if let l = v.as(String.self) { Text(l.count == 10 ? String(l.dropFirst(5)) : l).font(.caption) }
                }
            }
        }
        .chartLegend(position: .bottom, alignment: .leading)
    }
}

struct StatsTable: View {
    let model: StatsModel

    var body: some View {
        let colors = model.colors
        let rows = model.table
        Grid(alignment: .trailing, horizontalSpacing: 22, verticalSpacing: 3) {
            GridRow {
                Text("").gridColumnAlignment(.leading)
                Text("sessions"); Text("session-days"); Text("human"); Text("agent"); Text("share")
            }
            .font(.caption).foregroundStyle(.secondary)
            ForEach(rows, id: \.key) { r in
                GridRow {
                    HStack(spacing: 6) { Swatch(hex: colors[r.key]); Text(r.key).lineLimit(1) }
                        .frame(maxWidth: 260, alignment: .leading)
                    Text("\(r.sessions)"); Text("\(r.sessionDays)")
                    Text(Fmt.count(r.human)); Text(Fmt.count(r.agent))
                    Text(String(format: "%.0f %%", r.share * 100))
                }
                .font(.callout.monospacedDigit())
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Weekday × hour as coloured cells; the intensity is the metric.
struct StatsHeatmap: View {
    let model: StatsModel
    let compact: Bool
    static let days = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]

    var body: some View {
        let heat = model.heat
        let peak = max(1, heat.flatMap { $0 }.max() ?? 1)
        VStack(alignment: .leading, spacing: 2) {
            Text("weekday × hour · \(model.metric.label)").font(.caption).foregroundStyle(.secondary)
            Grid(horizontalSpacing: 2, verticalSpacing: 2) {
                GridRow {
                    Text("")
                    ForEach(0..<24, id: \.self) { h in
                        Text(h % 3 == 0 ? String(format: "%02d", h) : "")
                            .font(.system(size: 9).monospacedDigit()).foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity)
                    }
                }
                ForEach(0..<7, id: \.self) { d in
                    GridRow {
                        Text(Self.days[d]).font(.caption).foregroundStyle(.secondary).frame(width: 30, alignment: .leading)
                        ForEach(0..<24, id: \.self) { h in
                            let v = heat[d][h]
                            RoundedRectangle(cornerRadius: 2)
                                .fill(v == 0 ? Color.secondary.opacity(0.08) : Color.accentColor.opacity(0.15 + 0.85 * Double(v) / Double(peak)))
                                .frame(height: compact ? 14 : 20)
                                .help("\(Self.days[d]) \(String(format: "%02d", h)):00 · \(v)")
                        }
                    }
                }
            }
        }
    }
}
