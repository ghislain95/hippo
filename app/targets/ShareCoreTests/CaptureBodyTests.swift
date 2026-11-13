import Foundation
import Testing

@testable import ShareCore

/// Requirements: a link or a note is sent as the contract's JSON capture; an image is sent as
/// the contract's multipart capture; a text over 1 MB is refused before any send (design D6).
final class CaptureBodyTests {
  /// A directory of this test only (Swift Testing makes one instance per test), removed after it.
  private let directory: URL

  init() throws {
    directory = try TestFiles.directory()
  }

  deinit {
    try? FileManager.default.removeItem(at: directory)
  }

  private let clientId = Ulid(value: "01M7A5K2800000000000000000")
  private let server = URL(string: "https://hippo.example.ts.net")!
  private let token = "test-token-0123456789-abcdefghijklmnop"

  private func json(_ data: Data) throws -> [String: AnyHashable] {
    try #require(JSONSerialization.jsonObject(with: data) as? [String: AnyHashable])
  }

  private func jsonBody(_ result: CaptureBody.Result) throws -> Data {
    guard case .body(.json(let data)) = result else {
      Issue.record("expected a JSON body, got \(result)")
      throw BodyError.unexpected
    }
    return data
  }

  @Test("Link with a capture note")
  func linkWithACaptureNote() throws {
    let result = CaptureBody.json(
      form: .link("https://example.com/a"), userNote: " à tester ", isPrivate: false,
      clientId: clientId)
    guard case .body(let body) = result else {
      Issue.record("expected a body, got \(result)")
      return
    }

    let request = CaptureRequest(body: body).urlRequest(server: server, token: token)

    #expect(request.httpMethod == "POST")
    #expect(request.url?.absoluteString == "https://hippo.example.ts.net/api/items")
    #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer \(token)")
    #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
    let members = try json(try #require(request.httpBody))
    #expect(
      members == [
        "url": "https://example.com/a", "user_note": "à tester", "is_private": false,
        "client_id": clientId.value, "source": "share",
      ])
    #expect(Set(members.keys).isSubset(of: try Contract.properties(of: "CaptureRequest")))
  }

  @Test("Blank capture note is left out")
  func blankCaptureNoteIsLeftOut() throws {
    let data = try jsonBody(
      CaptureBody.json(
        form: .link("https://example.com/a"), userNote: "  ", isPrivate: false,
        clientId: clientId))

    #expect(try json(data)["user_note"] == nil)
  }

  @Test("Private note")
  func privateNote() throws {
    let data = try jsonBody(
      CaptureBody.json(
        form: .note("Code du portail"), userNote: "ignored", isPrivate: true, clientId: clientId))

    let members = try json(data)
    #expect(
      members == [
        "note": "Code du portail", "is_private": true, "client_id": clientId.value,
        "source": "share",
      ])
    #expect(Set(members.keys).isSubset(of: try Contract.properties(of: "CaptureRequest")))
  }

  @Test("Edited note text")
  func editedNoteText() throws {
    var flow = ShareFlow(
      choice: CaptureChoice(form: .note("Idée"), prefilledNote: ""), image: nil,
      clientId: clientId)

    flow.noteText = "Idée corrigée"
    let request = try #require(try flow.save(in: directory))

    let body = try #require(request.urlRequest(server: server, token: token).httpBody)
    #expect(try json(body)["note"] == AnyHashable("Idée corrigée"))
  }

  @Test("Image with a note, private")
  func imageWithANotePrivate() throws {
    let source = try TestFiles.image(.png, in: directory)
    let prepared = PreparedImage(
      file: source, format: .png,
      byteCount: Int64(try Data(contentsOf: source).count))

    let result = try MultipartWriter.write(
      image: prepared, userNote: "recette", isPrivate: true, clientId: clientId, in: directory)
    guard case .body(.multipart(let file, let boundary)) = result else {
      Issue.record("expected a multipart body, got \(result)")
      return
    }

    let request = CaptureRequest(body: .multipart(file: file, boundary: boundary))
      .urlRequest(server: server, token: token)
    #expect(
      request.value(forHTTPHeaderField: "Content-Type")
        == "multipart/form-data; boundary=\(boundary)")
    #expect(request.httpBody == nil)
    let parts = try Multipart.parts(of: Data(contentsOf: file), boundary: boundary)
    #expect(parts.map(\.name) == ["image", "user_note", "is_private", "client_id", "source"])
    #expect(parts[0].fileName == "capture.png")
    #expect(parts[0].contentType == "image/png")
    #expect(parts[0].body == (try Data(contentsOf: source)))
    #expect(parts[1].text == "recette")
    #expect(parts[2].text == "true")
    #expect(parts[3].text == clientId.value)
    #expect(parts[4].text == "share")
    #expect(Set(parts.map(\.name)).isSubset(of: try Contract.properties(of: "CaptureImageRequest")))
  }

  @Test("Image without a note")
  func imageWithoutANote() throws {
    let source = try TestFiles.image(.jpeg, in: directory)
    let prepared = PreparedImage(file: source, format: .jpeg, byteCount: 1)

    let result = try MultipartWriter.write(
      image: prepared, userNote: " \n", isPrivate: false, clientId: clientId, in: directory)
    guard case .body(.multipart(let file, let boundary)) = result else {
      Issue.record("expected a multipart body, got \(result)")
      return
    }

    let parts = try Multipart.parts(of: Data(contentsOf: file), boundary: boundary)
    #expect(!parts.map(\.name).contains("user_note"))
    #expect(parts.first { $0.name == "is_private" }?.text == "false")
  }

  @Test("Note text over 1 MB")
  func noteTextOver1MB() {
    let text = String(repeating: "a", count: 1_100_000)

    let result = CaptureBody.json(
      form: .note(text), userNote: "", isPrivate: false, clientId: clientId)

    guard case .tooLong(let byteCount) = result else {
      Issue.record("expected too-long, got a body")
      return
    }
    #expect(byteCount > 1_100_000)
    #expect(
      ShareState.tooLong(byteCount: byteCount).screen
        == ScreenText(
          title: "Texte trop long",
          message: "\(SizeText.megabytes(byteCount)), la limite est de 1 Mo."))
  }

  @Test("Note text just under the limit")
  func noteTextJustUnderTheLimit() throws {
    let empty = try jsonBody(
      CaptureBody.json(form: .note("a"), userNote: "", isPrivate: false, clientId: clientId))
    let text = String(repeating: "a", count: 1_048_576 - empty.count + 1)

    let data = try jsonBody(
      CaptureBody.json(form: .note(text), userNote: "", isPrivate: false, clientId: clientId))

    #expect(data.count == 1_048_576)
  }

  @Test("a capture note over 1 MiB keeps an image from being sent")
  func imageNoteOverLimit() throws {
    let prepared = PreparedImage(
      file: try TestFiles.file(byteCount: 10, in: directory), format: .jpeg, byteCount: 10)

    let result = try MultipartWriter.write(
      image: prepared, userNote: String(repeating: "é", count: 600_000), isPrivate: false,
      clientId: clientId, in: directory)

    #expect(result == .tooLong(byteCount: 1_200_000))
  }

  @Test("a large image is copied whole, across several chunks")
  func largeImageCopiedWhole() throws {
    let bytes = Data((0..<(2_500_000)).map { UInt8(truncatingIfNeeded: $0 &* 31) })
    let source = try TestFiles.file(bytes, in: directory, name: "large.jpg")
    let prepared = PreparedImage(file: source, format: .jpeg, byteCount: Int64(bytes.count))

    let result = try MultipartWriter.write(
      image: prepared, userNote: "", isPrivate: false, clientId: clientId, in: directory)
    guard case .body(.multipart(let file, let boundary)) = result else {
      Issue.record("expected a multipart body, got \(result)")
      return
    }

    let parts = try Multipart.parts(of: Data(contentsOf: file), boundary: boundary)
    #expect(parts.first?.body == bytes)
  }
}

enum BodyError: Error {
  case unexpected
}
