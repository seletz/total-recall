import SwiftUI
import TotalRecallCore

struct ConfigView: View {
    let app: AppState
    @Bindable var model: ConfigModel
    var body: some View { Text("ConfigView").frame(maxWidth: .infinity, maxHeight: .infinity) }
}
