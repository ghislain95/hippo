import Observation
import UIKit

/// Runs the sheet on the main actor: loads the share, then hands every decision to `Core`
/// (`CaptureChoice`, `ImagePreparation`, `ShareFlow`) and only loads, sends and closes (design
/// D9). It keeps no copy of what `ShareFlow` holds.
@MainActor
@Observable
final class ShareModel {
  /// The capture id of this share, drawn once when the sheet opens (RG-08).
  let clientId = Ulid.make()

  /// The state before the form exists: loading, then nothing-to-save or too-large.
  private(set) var initialState: ShareState = .loading
  private(set) var flow: ShareFlow?
  private(set) var linkPreview: LinkPreview?
  private(set) var thumbnail: UIImage?
  private(set) var imageCaption: String?

  /// The sheet closes by itself this long after a confirmation (design D9, validated).
  static let autoCloseDelay: Duration = .milliseconds(1500)

  @ObservationIgnored private let directory: URL
  @ObservationIgnored private let sender = CaptureSender()
  @ObservationIgnored private var task: Task<Void, Never>?
  @ObservationIgnored private var closeTask: Task<Void, Never>?
  /// Closes the sheet: true once the capture is confirmed, false otherwise.
  @ObservationIgnored private let finish: (_ completed: Bool) -> Void

  init(finish: @escaping (_ completed: Bool) -> Void) {
    self.finish = finish
    directory = FileManager.default.temporaryDirectory
      .appendingPathComponent(clientId.value, isDirectory: true)
    // A failure is left to the steps that need the directory: a link or a note never writes to
    // it; an image that cannot be copied there is unreadable (nothing-to-save), and a multipart
    // body that cannot be written shows screen 13 (preparation failure).
    try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
  }

  var state: ShareState { flow?.state ?? initialState }
  var form: CaptureForm? { flow?.form }
  var isEditable: Bool { flow?.isEditable ?? false }
  var canSave: Bool { flow?.canSave ?? false }
  var copyAction: CopyAction? { flow?.copyAction }

  var userNote: String {
    get { flow?.userNote ?? "" }
    set { flow?.userNote = newValue }
  }

  var noteText: String {
    get { flow?.noteText ?? "" }
    set { flow?.noteText = newValue }
  }

  var isPrivate: Bool {
    get { flow?.isPrivate ?? false }
    set { flow?.isPrivate = newValue }
  }

  func start(_ items: [NSExtensionItem]) {
    task = Task { await load(items) }
  }

  private func load(_ items: [NSExtensionItem]) async {
    let share = await AttachmentLoader.load(items)
    guard let choice = CaptureChoice.choose(share.content) else {
      show(.nothingToSave)
      return
    }
    var image: PreparedImage?
    switch choice.form {
    case .link(let url):
      linkPreview = LinkPreview(url: url, title: share.title)
    case .image(let shared):
      guard let index = share.content.images.firstIndex(where: \.isImage), let load = shared.load
      else {
        show(.nothingToSave)
        return
      }
      let loaded = await AttachmentLoader.loadImage(
        share.imageProviders[index], as: load, clientId: clientId, in: directory)
      if let blocking = loaded.result.blockingState {
        show(blocking)
        return
      }
      guard case .ready(let prepared) = loaded.result else { return }
      image = prepared
      imageCaption = prepared.caption
      thumbnail = loaded.thumbnail.map { UIImage(cgImage: $0) }
    case .note:
      break
    }
    if Task.isCancelled { return }
    flow = ShareFlow(choice: choice, image: image, clientId: clientId)
  }

  /// « Enregistrer »: freezes the form and sends, or shows the too-long state.
  func save() {
    guard var flow, flow.canSave else { return }
    // No request: the body is too long (too-long state) or cannot be written on the phone
    // (screen 13); either way nothing is sent and the new screen is announced.
    let request = flow.save(in: directory)
    self.flow = flow
    if let request {
      send(request)
    } else {
      announce()
    }
  }

  /// « Réessayer »: the same request, byte for byte.
  func retry() {
    guard let request = flow?.retry() else { return }
    send(request)
  }

  /// « Copier le lien » or « Copier le texte » (RG-06).
  func copy() {
    guard let action = copyAction else { return }
    UIPasteboard.general.string = action.text
  }

  /// « Annuler »: stops any send in flight and closes (design D9).
  func cancel() {
    finish(false)
  }

  /// « Fermer », after a screen that sends nothing more.
  func close() {
    finish(false)
  }

  /// Called once, when the sheet closes: stops everything and removes the temporary files.
  func cleanUp() {
    task?.cancel()
    closeTask?.cancel()
    sender.invalidate()
    try? FileManager.default.removeItem(at: directory)
  }

  private func send(_ request: CaptureRequest) {
    task = Task {
      guard let answer = await sender.send(request) else { return }
      flow?.receive(answer)
      announce()
      if state.closesBySelf {
        closeTask = Task {
          try? await Task.sleep(for: Self.autoCloseDelay)
          if !Task.isCancelled { finish(true) }
        }
      }
    }
  }

  private func show(_ state: ShareState) {
    initialState = state
    announce()
  }

  /// Tells VoiceOver that the sheet shows another screen.
  private func announce() {
    UIAccessibility.post(notification: .screenChanged, argument: nil)
  }
}
