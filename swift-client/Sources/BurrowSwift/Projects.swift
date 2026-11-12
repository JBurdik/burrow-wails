import AppKit
import SwiftUI

// MARK: - Project icon

/// workspaces.icon is a data: URL (png / x-icon / svg) — NSImage reads all three.
struct ProjectIcon: View {
    let ws: JSON
    var size: CGFloat = 16

    static var cache: [String: NSImage] = [:]

    var body: some View {
        if let img = image {
            Image(nsImage: img).resizable().scaledToFill()
                .frame(width: size, height: size)
                .clipShape(RoundedRectangle(cornerRadius: size * 0.28))
        } else {
            // Same fallback idea as the Sidebar: a letter tile.
            Text(String(ws.s("name").prefix(1)).uppercased())
                .font(.system(size: size * 0.55, weight: .bold, design: .rounded))
                .foregroundStyle(Theme.base)
                .frame(width: size, height: size)
                .background(Theme.accent.opacity(0.85), in: RoundedRectangle(cornerRadius: size * 0.28))
        }
    }

    var image: NSImage? {
        let icon = ws.s("icon")
        guard icon.hasPrefix("data:"), let comma = icon.firstIndex(of: ",") else { return nil }
        let key = "\(ws.i("id"))-\(icon.count)"
        if let hit = Self.cache[key] { return hit }
        let img = Data(base64Encoded: String(icon[icon.index(after: comma)...])).flatMap(NSImage.init(data:))
        Self.cache[key] = img
        return img
    }
}
