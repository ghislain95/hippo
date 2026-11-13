import Foundation

/// What the unreachable screen offers to copy (RG-06).
struct CopyAction: Equatable, Sendable {
  let label: String
  let text: String
}

/// The transitions of the sheet from the form on (design D9): the request is built once, at the
/// first « Enregistrer », and « Réessayer » sends it again unchanged, with the same capture id
/// (RG-08). The form is frozen from the first send. `ShareModel` only runs these transitions.
struct ShareFlow: Sendable {
  let clientId: Ulid
  let form: CaptureForm
  let image: PreparedImage?
  private(set) var state: ShareState
  /// 0 before the first send, then 1, plus one per « Réessayer ».
  private(set) var attempt = 0
  private(set) var request: CaptureRequest?

  private var editedUserNote: String
  private var editedNoteText: String
  private var editedIsPrivate = false

  /// - Parameter image: the prepared image, required for an image capture.
  init(choice: CaptureChoice, image: PreparedImage?, clientId: Ulid) {
    self.clientId = clientId
    self.form = choice.form
    self.image = image
    editedUserNote = choice.form.hasCaptureNote ? choice.prefilledNote : ""
    if case .note(let text) = choice.form { editedNoteText = text } else { editedNoteText = "" }
    if case .image = choice.form, image == nil { state = .nothingToSave } else { state = .form }
  }

  /// The form can be changed until the first send.
  var isEditable: Bool { state == .form }

  /// The capture note of a link or an image (RG-03).
  var userNote: String {
    get { editedUserNote }
    set { if isEditable { editedUserNote = newValue } }
  }

  /// The text of a note, editable (RG-07).
  var noteText: String {
    get { editedNoteText }
    set { if isEditable { editedNoteText = newValue } }
  }

  /// The Privé switch, off by default (RG-10).
  var isPrivate: Bool {
    get { editedIsPrivate }
    set { if isEditable { editedIsPrivate = newValue } }
  }

  /// « Enregistrer » is offered while the form is shown and, for a note, its text is not blank.
  var canSave: Bool {
    guard isEditable else { return false }
    if case .note = form { return CaptureBody.nonBlank(editedNoteText) != nil }
    return true
  }

  /// « Enregistrer »: freezes the form and returns the request to send, or nil when nothing is
  /// sent (too long, or not allowed now).
  mutating func save(in directory: URL) throws -> CaptureRequest? {
    guard canSave else { return nil }
    let result: CaptureBody.Result
    switch form {
    case .link(let url):
      result = CaptureBody.json(
        form: .link(url), userNote: editedUserNote, isPrivate: editedIsPrivate,
        clientId: clientId)
    case .note(_):
      result = CaptureBody.json(
        form: .note(editedNoteText), userNote: "", isPrivate: editedIsPrivate,
        clientId: clientId)
    case .image:
      guard let image else { return nil }
      result = try MultipartWriter.write(
        image: image, userNote: editedUserNote, isPrivate: editedIsPrivate, clientId: clientId,
        in: directory)
    }
    switch result {
    case .tooLong(let byteCount):
      state = .tooLong(byteCount: byteCount)
      return nil
    case .body(let body):
      let request = CaptureRequest(body: body)
      self.request = request
      attempt = 1
      state = .sending
      return request
    }
  }

  /// The answer to the request in flight.
  mutating func receive(_ answer: ServerAnswer) {
    guard state == .sending else { return }
    state = Outcome.classify(
      answer, form: form, attempt: attempt,
      sentNote: form.hasCaptureNote && CaptureBody.nonBlank(editedUserNote) != nil,
      isPrivate: editedIsPrivate, imageByteCount: image?.byteCount ?? 0)
  }

  /// « Réessayer », from the unreachable screen only: the same request, byte for byte.
  mutating func retry() -> CaptureRequest? {
    guard state == .unreachable, let request else { return nil }
    attempt += 1
    state = .sending
    return request
  }

  /// « Copier le lien » or « Copier le texte »; none for an image (RG-06).
  var copyAction: CopyAction? {
    switch form {
    case .link(let url): CopyAction(label: "Copier le lien", text: CaptureChoice.trimmed(url))
    case .note: CopyAction(label: "Copier le texte", text: CaptureChoice.trimmed(editedNoteText))
    case .image: nil
    }
  }
}
