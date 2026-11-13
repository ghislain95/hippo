import UIKit

/// Principal class of the share extension (Info.plist). The form, the loading of the shared
/// content and the send come with the extension's interface (tasks 4.1 to 4.3); until then the
/// sheet closes at once and sends nothing.
final class ShareViewController: UIViewController {
  override func viewDidLoad() {
    super.viewDidLoad()
    extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
  }
}
