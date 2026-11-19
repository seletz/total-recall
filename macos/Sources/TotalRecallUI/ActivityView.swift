import SwiftUI
import TotalRecallCore

struct ActivityView: View {
    let app: AppState
    @Bindable var model: ActivityModel
    var body: some View { Text("ActivityView").frame(maxWidth: .infinity, maxHeight: .infinity) }
}
