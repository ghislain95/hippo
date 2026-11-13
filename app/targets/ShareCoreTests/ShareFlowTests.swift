import Foundation
import Testing

@testable import ShareCore

/// Requirements: each answer of the server maps to one screen; « Déjà dans Hippo » is shown only
/// for a link on its first send; a private item is confirmed as private; each share carries its
/// own capture id, kept by « Réessayer »; the unreachable screen offers to copy what was shared
/// (design D8, D9).
final class ShareFlowTests {
  /// A directory of this test only (Swift Testing makes one instance per test), removed after it.
  private let directory: URL

  init() throws {
    directory = try TestFiles.directory()
  }

  deinit {
    try? FileManager.default.removeItem(at: directory)
  }

  private let clientId = Ulid(value: "01M7A5K2800000000000000000")

  private func item(isPrivate: Bool) -> Data {
    Data(#"{"id":"01M7A5K2800000000000000001","kind":"link","is_private":\#(isPrivate)}"#.utf8)
  }

  private func problem(type: String, status: Int, detail: String? = nil) -> Data {
    var members: [String: Any] = ["type": type, "title": "Problem", "status": status]
    if let detail { members["detail"] = detail }
    return (try? JSONSerialization.data(withJSONObject: members)) ?? Data()
  }

  private func linkFlow(note: String = "") -> ShareFlow {
    ShareFlow(
      choice: CaptureChoice(form: .link("https://example.com/a"), prefilledNote: note),
      image: nil, clientId: clientId)
  }

  private func noteFlow(_ text: String = "Idée") -> ShareFlow {
    ShareFlow(
      choice: CaptureChoice(form: .note(text), prefilledNote: ""), image: nil, clientId: clientId)
  }

  private func imageFlow() throws -> ShareFlow {
    let source = try TestFiles.image(.png, in: directory)
    let image = PreparedImage(file: source, format: .png, byteCount: 2_000)
    let flow = ShareFlow(
      choice: CaptureChoice(
        form: .image(SharedImage(typeIdentifiers: ["public.png"])), prefilledNote: ""),
      image: image, clientId: clientId)
    return flow
  }

  /// The state after a first send of `flow` answered by `answer`.
  private func answered(_ flow: ShareFlow, _ answer: ServerAnswer) throws -> ShareState {
    var flow = flow
    let saved = try flow.save(in: directory)
    _ = try #require(saved)
    flow.receive(answer)
    return flow.state
  }

  @Test("Server not reachable")
  func serverNotReachable() throws {
    let state = try answered(linkFlow(), .noAnswer)

    #expect(state == .unreachable)
    #expect(state.offersRetry)
    #expect(
      state.screen
        == ScreenText(
          title: "Serveur injoignable", message: "Vérifie que Tailscale est actif, puis réessaie."))
    #expect(ServerAnswer.failure(URLError(.timedOut)) == .noAnswer)
    #expect(ServerAnswer.failure(URLError(.cancelled)) == nil)
  }

  @Test("Service down behind Tailscale")
  func serviceDownBehindTailscale() throws {
    let state = try answered(linkFlow(), .http(status: 502, body: Data()))

    #expect(state == .unreachable)
    #expect(state.offersRetry)
    #expect(
      state.screen
        == ScreenText(
          title: "Serveur injoignable", message: "Vérifie que Tailscale est actif, puis réessaie."))
  }

  @Test("Token refused")
  func tokenRefused() throws {
    let state = try answered(
      linkFlow(), .http(status: 401, body: problem(type: "about:blank", status: 401)))

    #expect(state == .tokenRefused)
    #expect(!state.offersRetry)
    #expect(
      state.screen
        == ScreenText(
          title: "Accès refusé",
          message: "Le token de l'appli ne correspond plus au serveur. Reconstruis l'appli."))
  }

  @Test("Image refused as too large by the server")
  func imageRefusedAsTooLargeByTheServer() throws {
    let flow = try imageFlow()

    let state = try answered(
      flow,
      .http(status: 413, body: problem(type: "urn:hippo:problem:image-too-large", status: 413)))

    #expect(state == .tooLarge(byteCount: 2_000))
    #expect(
      state.screen
        == ScreenText(title: "Image trop lourde", message: "0,1 Mo, la limite est de 20 Mo."))
  }

  @Test("Image refused for its format")
  func imageRefusedForItsFormat() throws {
    let flow = try imageFlow()
    let body = problem(
      type: "urn:hippo:problem:unsupported-image", status: 400,
      detail: "Format d'image non pris en charge.")

    let state = try answered(flow, .http(status: 400, body: body))

    #expect(state == .refused(detail: "Format d'image non pris en charge.", status: 400))
    #expect(
      state.screen
        == ScreenText(
          title: "Hippo a refusé", message: "Format d'image non pris en charge.",
          caption: "Message renvoyé par le serveur"))
    #expect(!state.offersRetry)
  }

  @Test("Refusal without a detail")
  func refusalWithoutADetail() throws {
    let state = try answered(
      noteFlow(), .http(status: 415, body: problem(type: "about:blank", status: 415)))

    #expect(state == .refused(detail: nil, status: 415))
    #expect(
      state.screen
        == ScreenText(
          title: "Hippo a refusé", message: "Le serveur a refusé ce partage (HTTP 415)."))
    #expect(!state.offersRetry)
  }

  @Test("a blank detail is told with the fallback text")
  func blankDetail() {
    let state = Outcome.classify(
      .http(status: 400, body: problem(type: "about:blank", status: 400, detail: "  ")),
      form: .note("a"), attempt: 1, sentNote: false, isPrivate: false)

    #expect(state == .refused(detail: nil, status: 400))
  }

  @Test("Body over the server's limit")
  func bodyOverTheServersLimit() throws {
    let state = try answered(
      noteFlow(), .http(status: 413, body: problem(type: "about:blank", status: 413)))

    #expect(state == .refused(detail: nil, status: 413))
    #expect(!state.offersRetry)
  }

  @Test("New item")
  func newItem() throws {
    let state = try answered(linkFlow(), .http(status: 201, body: item(isPrivate: false)))

    #expect(state == .saved)
    #expect(state.closesBySelf)
    #expect(
      state.screen == ScreenText(title: "Enregistré", message: "Hippo le range dans la nuit."))
  }

  @Test("Duplicate link without a note")
  func duplicateLinkWithoutANote() throws {
    let state = try answered(linkFlow(), .http(status: 200, body: item(isPrivate: false)))

    #expect(state == .duplicate(withNote: false))
    #expect(
      state.screen
        == ScreenText(title: "Déjà dans Hippo", message: "Il était déjà dans ta bibliothèque."))
  }

  @Test("Duplicate link with a note")
  func duplicateLinkWithANote() throws {
    let state = try answered(
      linkFlow(note: "à tester"), .http(status: 200, body: item(isPrivate: false)))

    #expect(state == .duplicate(withNote: true))
    #expect(
      state.screen
        == ScreenText(
          title: "Déjà dans Hippo",
          message: "Il était déjà dans ta bibliothèque ; ta note l'accompagne."))
  }

  @Test("Replayed link after « Réessayer »")
  func replayedLinkAfterReessayer() throws {
    var flow = linkFlow()
    _ = try flow.save(in: directory)
    flow.receive(.noAnswer)

    let retried = flow.retry()

    _ = try #require(retried)
    flow.receive(.http(status: 200, body: item(isPrivate: false)))

    #expect(flow.state == .saved)
  }

  @Test("Replayed image")
  func replayedImage() throws {
    let flow = try imageFlow()

    let state = try answered(flow, .http(status: 200, body: item(isPrivate: false)))

    #expect(state == .saved)
  }

  @Test("New private item")
  func newPrivateItem() throws {
    let state = try answered(linkFlow(), .http(status: 201, body: item(isPrivate: true)))

    #expect(state == .savedPrivate)
    #expect(
      state.screen
        == ScreenText(
          title: "Enregistré",
          message: "Privé : il attend dans À trier et n'est jamais envoyé à l'IA."))
  }

  @Test("Replay of a private image")
  func replayOfAPrivateImage() throws {
    var flow = try imageFlow()
    flow.isPrivate = true
    _ = try flow.save(in: directory)
    flow.receive(.noAnswer)

    let retried = flow.retry()

    _ = try #require(retried)
    flow.receive(.http(status: 200, body: item(isPrivate: true)))

    #expect(flow.state == .savedPrivate)
  }

  @Test("« Réessayer » replays the same request")
  func reessayerReplaysTheSameRequest() throws {
    var flow = linkFlow(note: "à tester")
    let savedFirst = try flow.save(in: directory)
    let first = try #require(savedFirst)
    flow.receive(.noAnswer)

    let retried = flow.retry()

    let second = try #require(retried)

    #expect(second == first)
    #expect(flow.attempt == 2)
    #expect(flow.clientId == clientId)
    let body = try #require(
      second.urlRequest(server: URL(string: "https://h.example")!, token: "t").httpBody)
    #expect(String(decoding: body, as: UTF8.self).contains(clientId.value))
  }

  @Test("The form is frozen after the first send")
  func theFormIsFrozenAfterTheFirstSend() throws {
    var flow = linkFlow(note: "première")
    let savedFirst = try flow.save(in: directory)
    let first = try #require(savedFirst)
    flow.receive(.noAnswer)

    flow.userNote = "changée"
    flow.isPrivate = true
    let retried = flow.retry()
    let second = try #require(retried)

    #expect(second == first)
    #expect(flow.userNote == "première")
    #expect(!flow.isPrivate)
  }

  @Test("Copy a link")
  func copyALink() throws {
    var flow = linkFlow()
    _ = try flow.save(in: directory)
    flow.receive(.noAnswer)

    #expect(flow.copyAction == CopyAction(label: "Copier le lien", text: "https://example.com/a"))
  }

  @Test("Copy a text")
  func copyAText() throws {
    var flow = noteFlow("Idée")
    _ = try flow.save(in: directory)
    flow.receive(.noAnswer)

    #expect(flow.copyAction == CopyAction(label: "Copier le texte", text: "Idée"))
  }

  @Test("No copy for an image")
  func noCopyForAnImage() throws {
    var flow = try imageFlow()
    _ = try flow.save(in: directory)
    flow.receive(.noAnswer)

    #expect(flow.state == .unreachable)
    #expect(flow.copyAction == nil)
  }

  @Test("answers outside the contract are told as refused, with the fallback text")
  func unexpectedStatuses() {
    for status in [101, 204, 302] {
      #expect(
        Outcome.classify(
          .http(status: status, body: Data()), form: .note("a"), attempt: 1, sentNote: false,
          isPrivate: false) == .refused(detail: nil, status: status))
    }
  }

  @Test("an item that cannot be decoded is confirmed with the switch's value")
  func undecodableItem() {
    #expect(
      Outcome.classify(
        .http(status: 201, body: Data("{}".utf8)), form: .note("a"), attempt: 1, sentNote: false,
        isPrivate: true) == .savedPrivate)
  }

  @Test("a duplicate made private still shows « Déjà dans Hippo »")
  func privateDuplicate() throws {
    var flow = linkFlow()
    flow.isPrivate = true

    let state = try answered(flow, .http(status: 200, body: item(isPrivate: true)))

    #expect(state == .duplicate(withNote: false))
  }

  @Test("Blank note text cannot be saved")
  func blankNoteCannotBeSaved() throws {
    var flow = noteFlow()
    flow.noteText = "  \n"

    #expect(!flow.canSave)
    #expect(try flow.save(in: directory) == nil)
    #expect(flow.state == .form)
  }

  @Test("a note too long to send shows the too-long state and sends nothing")
  func noteTooLong() throws {
    var flow = noteFlow(String(repeating: "a", count: 1_100_000))

    #expect(try flow.save(in: directory) == nil)
    guard case .tooLong = flow.state else {
      Issue.record("expected too-long, got \(flow.state)")
      return
    }
    #expect(flow.request == nil)
  }

  @Test("the screens without a mockup read as validated")
  func textsOfDecisionsToConfirm() {
    #expect(
      ShareState.nothingToSave.screen
        == ScreenText(
          title: "Rien à enregistrer",
          message: "Ce partage ne contient ni lien, ni image, ni texte."))
    #expect(
      ShareState.tooLong(byteCount: 1_200_000).screen
        == ScreenText(title: "Texte trop long", message: "1,2 Mo, la limite est de 1 Mo."))
  }
}
