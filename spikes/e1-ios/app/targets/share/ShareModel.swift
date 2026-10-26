import Foundation
import SwiftUI
import UIKit

/// State of the share form, from reading the shared item to the server's answer.
@MainActor
final class ShareModel: ObservableObject {
  enum Phase {
    case loading
    case ready
    case sending
    case sent(Measures)
    case failed(String)
  }

  /// What the confirmation shows (design D8).
  struct Measures {
    /// From opening the extension to the server's answer, typing included.
    let elapsed: TimeInterval
    /// From tapping Enregistrer to the server's answer: the network part alone.
    let sending: TimeInterval
    let footprintMB: Double
    let serverAnswer: String
  }

  @Published var phase: Phase = .loading
  @Published var capture: Capture?
  @Published var userNote = ""
  @Published var isPrivate = false  // unchecked by default (RG-10)

  var onDone: () -> Void = {}
  var onCancel: () -> Void = {}

  private let openedAt = Date()

  func load(from providers: [NSItemProvider]) async {
    do {
      let capture = try await CaptureReader.read(providers)
      if case .link(_, let sharedText) = capture { userNote = sharedText }
      self.capture = capture
      phase = .ready
    } catch {
      phase = .failed(error.localizedDescription)
    }
  }

  func send() async {
    guard let capture else { return }
    phase = .sending
    let sentAt = Date()
    do {
      let answer = try await CaptureSender.send(capture, userNote: userNote, isPrivate: isPrivate)
      phase = .sent(Measures(
        elapsed: Date().timeIntervalSince(openedAt),
        sending: Date().timeIntervalSince(sentAt),
        footprintMB: Footprint.currentMB(),
        serverAnswer: answer))
    } catch {
      phase = .failed(error.localizedDescription)
    }
  }

  /// The link to copy when the capture fails (RG-06).
  var linkToCopy: String? {
    if case .link(let url, _) = capture { return url.absoluteString }
    return nil
  }
}

/// Builds the requests of the real contract (design D3): JSON for a link or a note, multipart
/// for an image, both with the Bearer token injected at build time (ADR-0014).
enum CaptureSender {
  static func send(_ capture: Capture, userNote: String, isPrivate: Bool) async throws -> String {
    var request = URLRequest(url: SpikeConfig.serverURL.appendingPathComponent("api/items"))
    request.httpMethod = "POST"
    request.timeoutInterval = 15
    request.setValue("Bearer \(SpikeConfig.token)", forHTTPHeaderField: "Authorization")
    let note = userNote.trimmingCharacters(in: .whitespacesAndNewlines)

    switch capture {
    case .link(let url, _):
      var body: [String: Any] = ["url": url.absoluteString, "is_private": isPrivate]
      if !note.isEmpty { body["user_note"] = note }
      request.setValue("application/json", forHTTPHeaderField: "Content-Type")
      request.httpBody = try JSONSerialization.data(withJSONObject: body)
    case .note(let text):
      // A note holds no capture note (RG-07): the form hides the field.
      request.setValue("application/json", forHTTPHeaderField: "Content-Type")
      request.httpBody = try JSONSerialization.data(withJSONObject: ["note": text, "is_private": isPrivate])
    case .image(let data, let mimeType, let fileExtension, _):
      let boundary = "hippo-\(UUID().uuidString)"
      request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
      var body = Data()
      func field(_ name: String, _ value: String) {
        body.append("--\(boundary)\r\nContent-Disposition: form-data; name=\"\(name)\"\r\n\r\n\(value)\r\n")
      }
      body.append("--\(boundary)\r\n")
      body.append("Content-Disposition: form-data; name=\"image\"; filename=\"capture.\(fileExtension)\"\r\n")
      body.append("Content-Type: \(mimeType)\r\n\r\n")
      body.append(data)
      body.append("\r\n")
      if !note.isEmpty { field("user_note", note) }
      field("is_private", isPrivate ? "true" : "false")
      body.append("--\(boundary)--\r\n")
      request.httpBody = body
    }

    let data: Data
    let response: URLResponse
    do {
      (data, response) = try await URLSession.shared.data(for: request)
    } catch let error as URLError {
      throw error.code == .cancelled ? error : ShareError.unreachable
    }
    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
    guard status == 201 else { throw ShareError.rejected(status: status) }
    return String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
  }
}

enum Footprint {
  /// Physical memory footprint of the extension, the figure iOS checks against its limit.
  static func currentMB() -> Double {
    var info = task_vm_info_data_t()
    var count = mach_msg_type_number_t(MemoryLayout<task_vm_info_data_t>.size / MemoryLayout<natural_t>.size)
    let result = withUnsafeMutablePointer(to: &info) {
      $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) {
        task_info(mach_task_self_, task_flavor_t(TASK_VM_INFO), $0, &count)
      }
    }
    return result == KERN_SUCCESS ? Double(info.phys_footprint) / 1_048_576 : -1
  }
}

extension Data {
  fileprivate mutating func append(_ string: String) {
    append(Data(string.utf8))
  }
}
