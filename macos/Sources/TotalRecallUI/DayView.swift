import SwiftUI
import TotalRecallCore

struct DayView: View {
    let app: AppState
    @Bindable var model: DayModel
    var body: some View { Text("DayView").frame(maxWidth: .infinity, maxHeight: .infinity) }
}
