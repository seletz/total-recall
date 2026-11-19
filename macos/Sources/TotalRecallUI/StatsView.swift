import SwiftUI
import TotalRecallCore

struct StatsView: View {
    let app: AppState
    @Bindable var model: StatsModel
    var body: some View { Text("StatsView").frame(maxWidth: .infinity, maxHeight: .infinity) }
}
