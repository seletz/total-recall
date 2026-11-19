import SwiftUI
import TotalRecallCore

struct AttachmentView: View {
    let app: AppState
    @Bindable var model: AttachmentModel
    var body: some View { Text("AttachmentView").frame(maxWidth: .infinity, maxHeight: .infinity) }
}
