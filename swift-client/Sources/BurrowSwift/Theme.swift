import AppKit
import SwiftUI

/// Burrow's design tokens (src/styles/app.css + src/themes.ts) — no new colours here.
/// Members are computed from the selected theme (Settings → Appearance, `@AppStorage("theme")`);
/// ContentView re-keys its tree on the theme so every view repaints.
enum Theme {
    struct Palette {
        let key, label: String
        let isDark: Bool
        let base, panel, hover, selected, border, text, text2, muted, accent, accentDim, green, yellow, red, terminal: Color
        var sidebar: Color { Color(nsColor: NSColor(base).blended(withFraction: 0.12, of: .black) ?? NSColor(base)) }
    }

    /// Ported 1:1 from THEMES in src/themes.ts (vars + xterm background). Wallpapers / animated backdrops are not ported.
    static let all: [Palette] = rows.map { r in
        let c = r.3.map(Color.css)
        return Palette(key: r.0, label: r.1, isDark: r.2, base: c[0], panel: c[1], hover: c[2], selected: c[3], border: c[4],
                       text: c[5], text2: c[6], muted: c[7], accent: c[8], accentDim: c[9], green: c[10], yellow: c[11],
                       red: c[12], terminal: c[13])
    }

    // ponytail: cached palette, re-read only when the key changes; a theme switch re-keys the view tree.
    nonisolated(unsafe) private static var cached: Palette?
    static var current: Palette {
        let key = UserDefaults.standard.string(forKey: "theme") ?? "plum"
        if let c = cached, c.key == key { return c }
        let p = all.first { $0.key == key } ?? all[0]
        cached = p
        return p
    }

    static var isDark: Bool { current.isDark }
    static var base: Color { current.base }
    static var sidebar: Color { current.sidebar }
    static var terminal: Color { current.terminal }
    static var panel: Color { current.panel }
    static var hover: Color { current.hover }
    static var selected: Color { current.selected }
    static var border: Color { current.border }
    static var text: Color { current.text }
    static var text2: Color { current.text2 }
    static var muted: Color { current.muted }
    static var accent: Color { current.accent }
    static var accentDim: Color { current.accentDim }
    static var green: Color { current.green }
    static var yellow: Color { current.yellow }
    static var red: Color { current.red }

    static func status(_ state: String, unread: Bool = false) -> Color {
        switch state {
        case "running": accent
        case "waiting_input", "waiting_approval": yellow
        case "failed": red
        case "done" where unread: green
        default: muted.opacity(0.5)
        }
    }

    // key, label, isDark, [bg-base, bg-panel, bg-hover, bg-selected, border, text-primary, text-secondary,
    //                      text-muted, accent, accent-dim, green, yellow, red, terminal]
    private static let rows: [(String, String, Bool, [String])] = [
        ("plum", "Plum", true, ["#241A20", "#2C2127", "#352A30", "#3A2E34", "#3D3137", "#ECE6E9", "#A79CA2", "#7E7379", "#A895F5", "#7C6FD8", "#7FBF8E", "#F2C14E", "#D9827A", "#1E161B"]),
        ("plum-light", "Plum Light", false, ["#F7F4F6", "#FFFFFF", "#EFE9ED", "#E6DDE3", "#DDD3D9", "#2A1F25", "#6B5F66", "#93878E", "#6E5BD8", "#5646B8", "#2F8A4A", "#B07F0E", "#C0503F", "#FFFFFF"]),
        ("dark", "Dark", true, ["#0A0B0F", "#14161C", "#1B1E26", "#241f38", "#262A33", "#F2F3F5", "#8B909C", "#5C6270", "#7C6FF0", "#5B4FC9", "#4ADE80", "#FBBF24", "#F87171", "#0a0810"]),
        ("light", "Light", false, ["#ffffff", "#f5f5f5", "#e8e8ec", "#cfe3ff", "#c2c2cc", "#0f172a", "#3a4759", "#64748b", "#2563eb", "#1d4ed8", "#16a34a", "#ca8a04", "#dc2626", "#ffffff"]),
        ("ps-gray", "Photoshop Gray", false, ["#bebebe", "#d6d6d6", "#c8c8c8", "#b8cce4", "#9a9a9a", "#1e1e1e", "#3c3c3c", "#6e6e6e", "#3875d7", "#2b5cab", "#2e7d32", "#a07800", "#c62828", "#e2e2e2"]),
        ("monokai", "Monokai", true, ["#1e1f1c", "#272822", "#3e3d32", "#49483e", "#4d4b3e", "#f8f8f2", "#d3d2c6", "#9d9883", "#fd971f", "#cc7a16", "#a6e22e", "#e6db74", "#f92672", "#272822"]),
        ("cobalt2", "Cobalt2", true, ["#193549", "#15232d", "#1f4662", "#0d3a58", "#2d6088", "#ffffff", "#bcd3e0", "#8aa6bb", "#ffc600", "#cc9e00", "#3ad900", "#ffc600", "#ff628c", "#193549"]),
        ("solarized-light", "Solarized Light", false, ["#fdf6e3", "#eee8d5", "#e4ddc8", "#d7e7e9", "#c7bd9f", "#073642", "#3f5358", "#7c8a8a", "#268bd2", "#1f6f9f", "#859900", "#b58900", "#dc322f", "#fdf6e3"]),
        ("dracula", "Dracula", true, ["#282a36", "#21222c", "#343746", "#44475a", "#4a4d63", "#f8f8f2", "#d2d4e0", "#8b94bb", "#bd93f9", "#9a73d4", "#50fa7b", "#f1fa8c", "#ff5555", "#282a36"]),
        ("tokyo-night", "Tokyo Night", true, ["#1a1b26", "#1f2030", "#292e42", "#2a2d4a", "rgba(255,255,255,0.08)", "#c0caf5", "#9aa5ce", "#6b7599", "#7aa2f7", "#5271c4", "#9ece6a", "#e0af68", "#f7768e", "#1a1b26"]),
        ("caffeine", "Caffeine", true, ["#111111", "#191919", "#2a2a2a", "#393028", "#201e18", "#eeeeee", "#c4c4c4", "#b4b4b4", "#ffe0c2", "#cc9e80", "#b5c98f", "#ffd9a0", "#e06c75", "#111111"]),
        ("tide", "Tide", true, ["#14191c", "#1a2024", "#232c30", "#283330", "rgba(205,217,218,0.09)", "#cdd9da", "#a8b8ba", "#93a3a4", "#6cb6b0", "#4a8f8a", "#8fc99a", "#d9bd80", "#e07a6a", "#14191c"]),
        ("tide-light", "Tide Light", false, ["#f2f6f5", "#ffffff", "#e3ecea", "#dde7e4", "rgba(43,58,57,0.12)", "#2b3a39", "#3d5352", "#5f6e6d", "#2f7d76", "#1f5f58", "#3a8a4f", "#a9802a", "#c0392b", "#f2f6f5"]),
        ("stonks", "Stonks 🚀 (meme)", true, ["#0a0012", "rgba(22, 0, 38, 0.62)", "rgba(255, 32, 121, 0.18)", "rgba(0, 240, 255, 0.22)", "#ff2079", "#f5e9ff", "#c9a8ff", "#8a6bb0", "#39ff14", "#00f0ff", "#39ff14", "#ffe600", "#ff2079", "rgba(10, 0, 18, 0.45)"]),
        ("lime-void", "Lime Void 🟢", true, ["#060a06", "#0c140c", "rgba(174, 255, 0, 0.14)", "rgba(174, 255, 0, 0.22)", "rgba(174, 255, 0, 0.35)", "#eaffd6", "#b6e89a", "#7fa86a", "#aeff00", "#7ab800", "#aeff00", "#eaff00", "#ff5470", "#060a06"]),
        ("nebula", "Nebula", true, ["#0d0e1f", "#13152b", "#1e2040", "#2d2060", "#252745", "#e4e7f5", "#8d96c0", "#5a6390", "#7c5af5", "#5a3dd0", "#22c55e", "#f59e0b", "#f0506e", "#0b0c1e"]),
        ("ps-gray-dark", "Photoshop Gray Dark", true, ["#1e1e1e", "#2b2b2b", "#373737", "#1f4f8f", "#3f3f3f", "#e8e8e8", "#b4b4b4", "#828282", "#4a90d9", "#2f6fb3", "#5bb75b", "#d9a13a", "#d95c5c", "#1e1e1e"]),
        ("monokai-light", "Monokai Light", false, ["#fdfcf6", "#f5f3ea", "#eae7da", "#ded9c6", "#dcd7c6", "#2c2a25", "#5b574c", "#8b8577", "#d5187c", "#a81161", "#5c9c14", "#b58900", "#d7301f", "#fdfcf6"]),
        ("cobalt2-light", "Cobalt2 Light", false, ["#f7fbff", "#e9f2fb", "#dae8f6", "#bcd9f2", "#c5daee", "#0b2b45", "#345a7a", "#6b89a3", "#e6841a", "#b8620c", "#2f8f4e", "#c08b00", "#d33a2c", "#f7fbff"]),
        ("solarized-dark", "Solarized Dark", true, ["#002b36", "#073642", "#0c4855", "#125666", "#0e4a58", "#eee8d5", "#93a1a1", "#657b83", "#268bd2", "#1d6fa8", "#859900", "#b58900", "#dc322f", "#002b36"]),
        ("alucard", "Alucard (Dracula Light)", false, ["#fffbeb", "#f5f1de", "#ebe6d0", "#dcd7bf", "#e0dac2", "#1f1f1f", "#4c4f5a", "#7b7f8b", "#a34d9c", "#7f3b7a", "#14710a", "#a3701a", "#cb3a2a", "#fffbeb"]),
        ("tokyo-night-light", "Tokyo Night Light", false, ["#e1e2e7", "#d5d6db", "#c8c9ce", "#b7c1e3", "#c4c8da", "#343b58", "#4c505e", "#787c99", "#34548a", "#2a4372", "#385f0d", "#8f5e15", "#8c4351", "#e1e2e7"]),
        ("caffeine-light", "Caffeine Light", false, ["#faf4ec", "#f1e7d9", "#e6d8c5", "#dcc7ab", "#dfd0bb", "#33261a", "#5e4a37", "#8b7460", "#a2561d", "#7d4114", "#5b7d16", "#a5791b", "#b2402b", "#faf4ec"]),
        ("stonks-light", "Stonks ☀️ (meme)", false, ["#fff6fb", "#ffe9f5", "#ffd9ee", "#ffc2e3", "#f6c8e2", "#2a0a1c", "#6b2049", "#a1668a", "#d1006b", "#a30053", "#0f8f2a", "#b57c00", "#d81b2f", "#fff6fb"]),
        ("lime-glow", "Lime Glow 🟢", false, ["#f7fdf4", "#ecf8e6", "#ddf0d3", "#c7e6b7", "#d3e9c8", "#12240c", "#3a5230", "#6d855f", "#3f8f0c", "#2f6d09", "#2f8f2a", "#9c8309", "#c0392b", "#f7fdf4"]),
        ("nebula-light", "Nebula Light", false, ["#faf8ff", "#f0ecfb", "#e4ddf6", "#d2c6ef", "#ded6f2", "#1b1630", "#463c68", "#776d95", "#6d3ecb", "#5229a5", "#0f8a63", "#a37a10", "#cb3355", "#faf8ff"]),
        ("fire", "Fire", true, ["#221614", "#2A1B18", "#33221E", "#3D2722", "#3F2B26", "#F0E6E2", "#B09C96", "#85716B", "#FF7A45", "#E0532E", "#8FBF7F", "#F5B84A", "#F0655A", "#1C1210"]),
        ("fire-light", "Fire Light", false, ["#FBF5F2", "#FFFFFF", "#F4E9E4", "#F7DACD", "#E8D6CE", "#2B1A15", "#6E564E", "#98827A", "#D9481C", "#B5380F", "#2F8A4A", "#B07A0E", "#C8362B", "#FFFFFF"]),
        ("matcha", "Matcha", true, ["#171E15", "#1D2519", "#252E20", "#2D3826", "#303B29", "#E6EDE0", "#9DAA94", "#75816D", "#B5E35A", "#8EBF3A", "#9BD46A", "#E8C95A", "#E07F70", "#131910"]),
        ("matcha-light", "Matcha Light", false, ["#F5F8F0", "#FFFFFF", "#ECF1E4", "#DDEBC8", "#D5DFC8", "#1E2818", "#5A6850", "#85927B", "#5E9A12", "#4A7D0A", "#3A8A2E", "#A67C0E", "#C0503F", "#FFFFFF"]),
        ("peach", "Peach", true, ["#261C19", "#2E221F", "#372A26", "#40302B", "#42332E", "#F2E8E3", "#B3A099", "#88766F", "#FFAA85", "#F08A66", "#8FC49A", "#F2C66E", "#E8847A", "#201714"]),
        ("peach-light", "Peach Light", false, ["#FFF6F1", "#FFFFFF", "#FCEBE2", "#FADBCB", "#F0DCD1", "#2E1E18", "#735A50", "#9C857B", "#E0703F", "#C25A2C", "#3A8A50", "#B07F0E", "#C8453A", "#FFFFFF"]),
        ("synthwave", "Synthwave", true, ["#1A1030", "#21153A", "#2A1C47", "#352357", "#36255A", "#F3EAFF", "#B6A6D6", "#8475A8", "#FF4FD8", "#D63AB4", "#5CF2B0", "#FFD25C", "#FF5C7A", "#150C28"]),
        ("synthwave-light", "Synthwave Light", false, ["#FBF4FF", "#FFFFFF", "#F4E8FC", "#EED6FA", "#E6D3F2", "#24133A", "#634F80", "#8E7BAA", "#C21FA0", "#A0177F", "#138A5C", "#A87800", "#D02A50", "#FFFFFF"]),
        ("matrix", "Matrix", true, ["#000A03", "#021006", "rgba(0, 255, 102, 0.10)", "rgba(0, 255, 102, 0.18)", "rgba(0, 255, 102, 0.22)", "#D7FFE0", "#7FD99A", "#4F9A66", "#00FF66", "#00B848", "#00FF66", "#E6FF4D", "#FF4D6A", "#020A04"]),
        ("matrix-light", "Matrix Light", false, ["#F2FAF3", "#FFFFFF", "rgba(0, 140, 60, 0.08)", "rgba(0, 140, 60, 0.15)", "rgba(0, 120, 50, 0.22)", "#0E2416", "#3D6B4A", "#6E9479", "#0A9A45", "#077A36", "#0A9A45", "#9A7A00", "#C8364E", "#FFFFFF"]),
    ]
}

extension Color {
    /// "#RRGGBB" or "rgba(r, g, b, a)" as written in themes.ts.
    static func css(_ s: String) -> Color {
        if s.hasPrefix("#") { return Color(hex: UInt32(s.dropFirst(), radix: 16) ?? 0) }
        let n = s.drop { $0 != "(" }.dropFirst().prefix { $0 != ")" }
            .split(separator: ",").compactMap { Double($0.trimmingCharacters(in: .whitespaces)) }
        guard n.count >= 3 else { return .gray }
        return Color(red: n[0] / 255, green: n[1] / 255, blue: n[2] / 255).opacity(n.count > 3 ? n[3] : 1)
    }

    init(hex: UInt32) {
        self.init(red: Double(hex >> 16 & 0xFF) / 255, green: Double(hex >> 8 & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
    }
}

extension Font {
    private static var base: CGFloat {
        let v = UserDefaults.standard.double(forKey: "fontSize")
        return v > 0 ? v : 15
    }

    /// Prose (transcript, composer), scaled by ⌘+ / ⌘−.
    static func ui(_ delta: CGFloat = 0, _ design: Font.Design = .default, weight: Font.Weight = .regular) -> Font {
        design == .monospaced ? brand(delta, weight: weight) : .system(size: base + delta, weight: weight, design: design)
    }

    /// Burrow's --font-ui (JetBrains Mono → 0xProto): chrome, titles, code. Falls back to system mono.
    static func brand(_ delta: CGFloat = 0, weight: Font.Weight = .regular) -> Font {
        let size = base + delta
        let name = weight == .bold || weight == .semibold ? "0xProtoNerdFont-Bold" : "0xProtoNerdFont-Regular"
        return NSFont(name: name, size: size) != nil ? .custom(name, size: size) : .system(size: size, weight: weight, design: .monospaced)
    }
}

// MARK: - Pet

/// The status pet from src/assets/pets, pixel art → nearest-neighbour scaling.
struct Pet: View {
    let state: String
    var size: CGFloat = 40

    var body: some View {
        TimelineView(.periodic(from: .now, by: 0.45)) { ctx in
            let frame = Int(ctx.date.timeIntervalSinceReferenceDate / 0.45) % 2
            if let img = Pet.image(name(frame)) {
                Image(nsImage: img).interpolation(.none).resizable().frame(width: size, height: size)
            }
        }
    }

    func name(_ frame: Int) -> String {
        switch state {
        case "running": "cat-work-\(frame)"
        case "waiting_input", "waiting_approval", "failed": "cat-attn"
        case "done": "cat-happy"
        default: "cat-idle"
        }
    }

    static var cache: [String: NSImage] = [:]
    static func image(_ name: String) -> NSImage? {
        if let i = cache[name] { return i }
        let i = Bundle.module.url(forResource: name, withExtension: "png", subdirectory: "Resources").flatMap(NSImage.init(contentsOf:))
        cache[name] = i
        return i
    }
}

/// Soft amber light from the logo's burrow entrance.
struct BurrowGlow: View {
    var body: some View {
        RadialGradient(colors: [Theme.yellow.opacity(0.10), .clear], center: .top, startRadius: 0, endRadius: 520)
            .ignoresSafeArea()
            .allowsHitTesting(false)
    }
}
