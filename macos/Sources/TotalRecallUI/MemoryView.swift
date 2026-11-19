import SwiftUI
import TotalRecallCore

struct MemoryView: View {
    let app: AppState
    @Bindable var model: MemoryModel
    var body: some View { Text("MemoryView").frame(maxWidth: .infinity, maxHeight: .infinity) }
}
