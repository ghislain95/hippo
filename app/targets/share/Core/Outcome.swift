import Foundation

/// What came back from `POST /api/items`.
enum ServerAnswer: Equatable, Sendable {
  /// No HTTP answer: no network, Tailscale off, time limit reached.
  case noAnswer
  case http(status: Int, body: Data)

  /// The answer for a failed task, or nil when the user cancelled it (« Annuler »).
  static func failure(_ error: Error) -> ServerAnswer? {
    if let error = error as? URLError, error.code == .cancelled { return nil }
    return .noAnswer
  }
}

/// Maps an answer to the screen that tells it (RG-04, RG-06, RG-08, RG-15, design D8).
enum Outcome {
  static let imageTooLargeType = "urn:hippo:problem:image-too-large"

  private struct Problem: Decodable {
    let type: String?
    let detail: String?
  }

  private struct AnsweredItem: Decodable {
    let isPrivate: Bool

    enum CodingKeys: String, CodingKey {
      case isPrivate = "is_private"
    }
  }

  /// - Parameters:
  ///   - attempt: 1 for the first send, then one more per « Réessayer ».
  ///   - sentNote: whether a capture note was sent.
  ///   - isPrivate: the Privé switch as sent, used when an item cannot be decoded.
  static func classify(
    _ answer: ServerAnswer, form: CaptureForm, attempt: Int, sentNote: Bool, isPrivate: Bool,
    imageByteCount: Int64 = 0
  ) -> ShareState {
    guard case .http(let status, let body) = answer else { return .unreachable }
    let decoder = JSONDecoder()
    switch status {
    case 500...599:
      return .unreachable
    case 401:
      return .tokenRefused
    case 400...499:
      let problem = try? decoder.decode(Problem.self, from: body)
      if status == 413, problem?.type == imageTooLargeType {
        return .tooLarge(byteCount: imageByteCount)
      }
      // A blank `detail` tells nothing: the fallback text is shown instead.
      return .refused(detail: problem?.detail.flatMap(CaptureBody.nonBlank), status: status)
    case 200 where isFirstSendOfLink(form, attempt):
      return .duplicate(withNote: sentNote)
    case 200, 201:
      let item = try? decoder.decode(AnsweredItem.self, from: body)
      return (item?.isPrivate ?? isPrivate) ? .savedPrivate : .saved
    default:
      return .refused(detail: nil, status: status)
    }
  }

  private static func isFirstSendOfLink(_ form: CaptureForm, _ attempt: Int) -> Bool {
    if case .link = form { return attempt == 1 }
    return false
  }
}
