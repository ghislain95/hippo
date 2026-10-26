import SwiftUI
import UIKit

/// The mini form of ADR-0014: preview, capture note, Privé, Enregistrer.
struct ShareView: View {
  @ObservedObject var model: ShareModel
  @State private var copied = false

  var body: some View {
    NavigationStack {
      Form {
        switch model.phase {
        case .loading:
          ProgressView("Lecture du partage…")
        case .ready, .sending:
          preview
          if case .note = model.capture {
            // A note holds no capture note (RG-07).
          } else {
            Section("Note de capture") {
              TextField("Pourquoi tu gardes ça ?", text: $model.userNote, axis: .vertical)
                .lineLimit(2...5)
            }
          }
          Section {
            Toggle("Privé", isOn: $model.isPrivate)
          } footer: {
            Text("Un élément privé n'est jamais envoyé à l'IA.")
          }
        case .sent(let measures):
          Section("Enregistré") {
            Label("Capture reçue par le serveur", systemImage: "checkmark.circle.fill")
              .foregroundStyle(.green)
          }
          Section("Mesures") {
            LabeledContent("Temps depuis l'ouverture", value: String(format: "%.2f s", measures.elapsed))
            LabeledContent("Durée de l'envoi", value: String(format: "%.2f s", measures.sending))
            LabeledContent("Mémoire de l'extension", value: String(format: "%.1f Mo", measures.footprintMB))
            Text(measures.serverAnswer).font(.footnote.monospaced()).foregroundStyle(.secondary)
          }
        case .failed(let message):
          Section {
            Label(message, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            if let link = model.linkToCopy {
              Button(copied ? "Lien copié" : "Copier le lien", systemImage: "doc.on.doc") {
                UIPasteboard.general.string = link
                copied = true
              }
            }
            if model.capture != nil {
              Button("Réessayer", systemImage: "arrow.clockwise") { Task { await model.send() } }
            }
          }
        }
      }
      .navigationTitle("Hippo")
      .navigationBarTitleDisplayMode(.inline)
      .toolbar { toolbar }
    }
  }

  @ViewBuilder private var preview: some View {
    Section {
      switch model.capture {
      case .link(let url, _):
        Label {
          VStack(alignment: .leading) {
            Text(url.host() ?? url.absoluteString).font(.headline)
            Text(url.absoluteString).font(.caption).foregroundStyle(.secondary).lineLimit(2)
          }
        } icon: {
          Image(systemName: "link")
        }
      case .note(let text):
        Label(text, systemImage: "note.text").lineLimit(6)
      case .image(let data, let mimeType, _, let preview):
        HStack {
          if let preview {
            Image(uiImage: preview).resizable().scaledToFit().frame(maxHeight: 120)
              .clipShape(RoundedRectangle(cornerRadius: 8))
          }
          Text("\(mimeType), \(ByteCountFormatter.string(fromByteCount: Int64(data.count), countStyle: .file))")
            .font(.caption).foregroundStyle(.secondary)
        }
      case nil:
        EmptyView()
      }
    }
  }

  @ToolbarContentBuilder private var toolbar: some ToolbarContent {
    switch model.phase {
    case .sent:
      ToolbarItem(placement: .confirmationAction) { Button("OK") { model.onDone() } }
    default:
      ToolbarItem(placement: .cancellationAction) { Button("Annuler") { model.onCancel() } }
      ToolbarItem(placement: .confirmationAction) {
        if case .sending = model.phase {
          ProgressView()
        } else {
          Button("Enregistrer") { Task { await model.send() } }
            .disabled(model.capture == nil)
        }
      }
    }
  }
}
