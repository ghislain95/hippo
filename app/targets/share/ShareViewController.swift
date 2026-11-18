import SwiftUI
import UIKit

/// Principal class of the share extension (Info.plist): hosts `ShareView`, hands the shared
/// items to `ShareModel`, and closes the sheet once, removing the temporary files (design D5,
/// D9).
final class ShareViewController: UIViewController {
  private var model: ShareModel?
  private var isFinished = false

  override func viewDidLoad() {
    super.viewDidLoad()
    let model = ShareModel { [weak self] completed in self?.finish(completed: completed) }
    self.model = model

    let host = UIHostingController(rootView: ShareView(model: model))
    addChild(host)
    host.view.translatesAutoresizingMaskIntoConstraints = false
    view.addSubview(host.view)
    NSLayoutConstraint.activate([
      host.view.topAnchor.constraint(equalTo: view.topAnchor),
      host.view.bottomAnchor.constraint(equalTo: view.bottomAnchor),
      host.view.leadingAnchor.constraint(equalTo: view.leadingAnchor),
      host.view.trailingAnchor.constraint(equalTo: view.trailingAnchor),
    ])
    host.didMove(toParent: self)
    // The sheet is left by « Annuler », « Fermer » or by itself, never by a swipe that would
    // leave the temporary files and a send in flight.
    isModalInPresentation = true

    let items = (extensionContext?.inputItems ?? []).compactMap { $0 as? NSExtensionItem }
    model.start(items)
  }

  private func finish(completed: Bool) {
    guard !isFinished else { return }
    isFinished = true
    model?.cleanUp()
    if completed {
      extensionContext?.completeRequest(returningItems: nil)
    } else {
      extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
    }
  }
}
