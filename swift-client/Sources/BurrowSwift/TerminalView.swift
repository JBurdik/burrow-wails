import AppKit
import CGhosttyVT
import SwiftUI

/// SwiftUI host for one session's terminal view.
struct TerminalHostView: NSViewRepresentable {
    let session: TerminalSession
    @AppStorage("fontSize") private var fontSize = 15.0
    @ObservedObject private var cfg = ConfigStore.shared

    /// uiPrefs.terminalFont (a CSS family list) + terminalFontSize, scaled with ⌘+/⌘− like the Vue UI scale.
    private var font: (family: String, size: CGFloat) {
        let css = cfg.ui.s("terminalFont")
        let family = css.split(separator: ",").first.map { $0.trimmingCharacters(in: CharacterSet(charactersIn: "\"' ")) } ?? ""
        let base = (cfg.ui["terminalFontSize"] as? NSNumber)?.doubleValue ?? 13
        return (family, CGFloat(base * fontSize / 15))
    }

    func makeNSView(context: Context) -> TerminalNSView {
        let v = TerminalNSView(session: session)
        v.setFont(family: font.family, size: font.size)
        return v
    }

    func updateNSView(_ v: TerminalNSView, context: Context) {
        if v.session !== session { v.session = session }
        session.view = v
        v.setFont(family: font.family, size: font.size)
    }

    static func dismantleNSView(_ v: TerminalNSView, coordinator: ()) {
        if v.session.view === v { v.session.view = nil }
    }
}

/// Draws a libghostty render state with CoreText and turns AppKit input into PTY bytes.
final class TerminalNSView: NSView, NSTextInputClient {
    var session: TerminalSession { didSet { session.view = self; recomputeGrid(); needsDisplay = true } }

    private var font = NSFont.monospacedSystemFont(ofSize: 13, weight: .regular)
    private var bold = NSFont.monospacedSystemFont(ofSize: 13, weight: .bold)
    private var italic = NSFont.monospacedSystemFont(ofSize: 13, weight: .regular)
    private var cellW: CGFloat = 8
    private var cellH: CGFloat = 16
    private var ascent: CGFloat = 12
    private let pad: CGFloat = 8

    private var cells: [BvtCell] = []
    private var frameInfo = BvtFrame()
    private var focused = false

    /// Selection in viewport cells (anchor, head), cleared on scroll.
    private var selection: (a: (x: Int, y: Int), h: (x: Int, y: Int))?
    private var scrollAccum: CGFloat = 0

    // Text input: what interpretKeyEvents produced for the key being handled.
    private var keyText: String?
    private var inKeyDown = false
    private var marked = NSMutableAttributedString()

    init(session: TerminalSession) {
        self.session = session
        super.init(frame: .zero)
        session.view = self
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }
    override var acceptsFirstResponder: Bool { true }
    override var isOpaque: Bool { true }

    // MARK: fonts / grid

    private var family = "\0"

    /// A font family by name (installed fonts only); generic CSS names / missing families fall back to 0xProto.
    func setFont(family fam: String, size: CGFloat) {
        guard abs(font.pointSize - size) > 0.01 || fam != family else { return }
        family = fam
        let fm = NSFontManager.shared
        if let f = fm.font(withFamily: fam, traits: [], weight: 5, size: size) {
            font = f
            bold = fm.convert(f, toHaveTrait: .boldFontMask)
        } else {
            font = NSFont(name: "0xProtoNerdFont-Regular", size: size) ?? .monospacedSystemFont(ofSize: size, weight: .regular)
            bold = NSFont(name: "0xProtoNerdFont-Bold", size: size) ?? .monospacedSystemFont(ofSize: size, weight: .bold)
        }
        italic = fm.convert(font, toHaveTrait: .italicFontMask)
        let adv = ("M" as NSString).size(withAttributes: [.font: font]).width
        cellW = ceil(adv * 100) / 100
        ascent = ceil(font.ascender)
        cellH = ceil(font.ascender - font.descender + font.leading) + 2
        recomputeGrid()
        needsDisplay = true
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        recomputeGrid()
    }

    private func recomputeGrid() {
        guard bounds.width > 0, bounds.height > 0 else { return }
        let c = max(2, Int((bounds.width - pad * 2) / cellW))
        let r = max(1, Int((bounds.height - pad * 2) / cellH))
        session.resize(cols: UInt16(clamping: c), rows: UInt16(clamping: r), cellW: UInt32(cellW), cellH: UInt32(cellH))
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        recomputeGrid()
        // In a split only the tab's focused pane takes the keyboard.
        if let id = Int64(session.id), !TerminalLayout.shared.wantsFocus(id) { return }
        window?.makeFirstResponder(self)
    }

    func dataArrived() {
        needsDisplay = true
    }

    // MARK: drawing

    private static func color(_ hex: UInt32, _ alpha: CGFloat = 1) -> CGColor {
        CGColor(red: CGFloat(hex >> 16 & 0xFF) / 255, green: CGFloat(hex >> 8 & 0xFF) / 255, blue: CGFloat(hex & 0xFF) / 255, alpha: alpha)
    }

    private func cellText(_ c: BvtCell) -> String {
        guard c.len > 0 else { return "" }
        return withUnsafeBytes(of: c.text) { raw in
            String(decoding: raw.prefix(Int(c.len)), as: UTF8.self)
        }
    }

    private func isSelected(_ x: Int, _ y: Int) -> Bool {
        guard let s = selection else { return false }
        var (a, h) = (s.a, s.h)
        if (h.y, h.x) < (a.y, a.x) { swap(&a, &h) }
        if y < a.y || y > h.y { return false }
        if y == a.y && x < a.x { return false }
        if y == h.y && x > h.x { return false }
        return true
    }

    override func draw(_ dirtyRect: NSRect) {
        guard let ctx = NSGraphicsContext.current?.cgContext else { return }
        let need = Int(session.cols) * Int(session.rows)
        if cells.count < need { cells = Array(repeating: BvtCell(), count: need) }
        let n = cells.withUnsafeMutableBufferPointer { bvt_snapshot(session.vt, $0.baseAddress, $0.count, &frameInfo) }
        let cols = Int(frameInfo.cols), rows = Int(frameInfo.rows)
        let defFg = frameInfo.fg, defBg = frameInfo.bg

        ctx.setFillColor(Self.color(defBg))
        ctx.fill(bounds)
        guard n > 0, cols > 0 else { return }

        ctx.textMatrix = CGAffineTransform(scaleX: 1, y: -1)
        let cursorHere = frameInfo.cursor_visible
        let cx = Int(frameInfo.cursor_x), cy = Int(frameInfo.cursor_y)

        for y in 0..<rows {
            let top = pad + CGFloat(y) * cellH
            // Backgrounds + selection.
            for x in 0..<cols {
                let c = cells[y * cols + x]
                if c.wide == UInt8(BVT_SPACER) { continue }
                let inv = c.flags & UInt8(BVT_INVERSE) != 0
                var bg: UInt32? = c.flags & UInt8(BVT_HAS_BG) != 0 ? c.bg : nil
                if inv { bg = c.flags & UInt8(BVT_HAS_FG) != 0 ? c.fg : defFg }
                let w = CGFloat(c.wide == UInt8(BVT_WIDE) ? 2 : 1) * cellW
                let rect = CGRect(x: pad + CGFloat(x) * cellW, y: top, width: w, height: cellH)
                if let bg { ctx.setFillColor(Self.color(bg)); ctx.fill(rect) }
                if isSelected(x, y) { ctx.setFillColor(Self.color(0x7C6FD8, 0.45)); ctx.fill(rect) }
            }
            // Cursor block sits under the glyph.
            if cursorHere, cy == y, focused, frameInfo.cursor_style == 1 {
                ctx.setFillColor(Self.color(0xA895F5))
                ctx.fill(CGRect(x: pad + CGFloat(cx) * cellW, y: top, width: cellW, height: cellH))
            }
            // Glyphs: runs of same-styled single-width ASCII draw as one line; anything else per cell.
            var x = 0
            while x < cols {
                let c = cells[y * cols + x]
                if c.len == 0 || c.wide == UInt8(BVT_SPACER) { x += 1; continue }
                var fg = c.flags & UInt8(BVT_HAS_FG) != 0 ? c.fg : defFg
                if c.flags & UInt8(BVT_INVERSE) != 0 { fg = c.flags & UInt8(BVT_HAS_BG) != 0 ? c.bg : defBg }
                if cursorHere, focused, frameInfo.cursor_style == 1, cy == y, cx == x { fg = defBg }
                let style = c.flags & UInt8(BVT_BOLD | BVT_ITALIC | BVT_FAINT | BVT_UNDERLINE | BVT_STRIKE)
                var text = cellText(c)
                var span = c.wide == UInt8(BVT_WIDE) ? 2 : 1
                let ascii = c.len == 1 && c.text.0 < 0x7F && span == 1
                if ascii {
                    while x + span < cols {
                        let d = cells[y * cols + x + span]
                        var dfg = d.flags & UInt8(BVT_HAS_FG) != 0 ? d.fg : defFg
                        if d.flags & UInt8(BVT_INVERSE) != 0 { dfg = d.flags & UInt8(BVT_HAS_BG) != 0 ? d.bg : defBg }
                        let atCursor = cursorHere && focused && frameInfo.cursor_style == 1 && cy == y && cx == x + span
                        guard d.wide == UInt8(BVT_NARROW), d.len <= 1, d.text.0 < 0x7F, !atCursor,
                              d.flags & UInt8(BVT_BOLD | BVT_ITALIC | BVT_FAINT | BVT_UNDERLINE | BVT_STRIKE) == style,
                              d.len == 0 || dfg == fg else { break }
                        text += d.len == 0 ? " " : cellText(d)
                        span += 1
                    }
                }
                drawText(ctx, text.replacingOccurrences(of: " +$", with: "", options: .regularExpression),
                         x: pad + CGFloat(x) * cellW, top: top, fg: fg, style: style, cells: span)
                x += span
            }
            // Bar / underline / unfocused cursor drawn over the glyph.
            if cursorHere, cy == y, !(focused && frameInfo.cursor_style == 1) {
                let r = CGRect(x: pad + CGFloat(cx) * cellW, y: top, width: cellW, height: cellH)
                ctx.setFillColor(Self.color(0xA895F5))
                switch frameInfo.cursor_style {
                case 0: ctx.fill(CGRect(x: r.minX, y: r.minY, width: 2, height: r.height))
                case 2: ctx.fill(CGRect(x: r.minX, y: r.maxY - 2, width: r.width, height: 2))
                default:
                    ctx.setStrokeColor(Self.color(0xA895F5))
                    ctx.stroke(r.insetBy(dx: 0.5, dy: 0.5), width: 1)
                }
            }
        }
        // IME composition (dead keys, Pinyin, …) drawn at the cursor until committed.
        if marked.length > 0, cursorHere {
            let text = marked.string
            let w = CGFloat(max(1, text.count)) * cellW * (text.unicodeScalars.contains { $0.value > 0x2E80 } ? 2 : 1)
            let r = CGRect(x: pad + CGFloat(cx) * cellW, y: pad + CGFloat(cy) * cellH, width: w, height: cellH)
            ctx.setFillColor(Self.color(defBg)); ctx.fill(r)
            drawText(ctx, text, x: r.minX, top: r.minY, fg: defFg, style: UInt8(BVT_UNDERLINE), cells: Int(ceil(w / cellW)))
        }
        // Scrolled back: a thin position marker on the right edge.
        if !frameInfo.at_bottom, frameInfo.scroll_total > 0 {
            let h = bounds.height * CGFloat(frameInfo.scroll_len) / CGFloat(frameInfo.scroll_total)
            let y = bounds.height * CGFloat(frameInfo.scroll_offset) / CGFloat(frameInfo.scroll_total)
            ctx.setFillColor(Self.color(0x7E7379, 0.6))
            ctx.fill(CGRect(x: bounds.width - 4, y: y, width: 3, height: max(h, 12)))
        }
    }

    private func drawText(_ ctx: CGContext, _ text: String, x: CGFloat, top: CGFloat, fg: UInt32, style: UInt8, cells: Int) {
        let f = style & UInt8(BVT_BOLD) != 0 ? bold : style & UInt8(BVT_ITALIC) != 0 ? italic : font
        let color = NSColor(cgColor: Self.color(fg, style & UInt8(BVT_FAINT) != 0 ? 0.6 : 1)) ?? .white
        if !text.isEmpty {
            let line = CTLineCreateWithAttributedString(NSAttributedString(string: text, attributes: [.font: f, .foregroundColor: color]))
            ctx.textPosition = CGPoint(x: x, y: top + ascent + 1)
            CTLineDraw(line, ctx)
        }
        let w = CGFloat(cells) * cellW
        ctx.setFillColor(color.cgColor)
        if style & UInt8(BVT_UNDERLINE) != 0 { ctx.fill(CGRect(x: x, y: top + cellH - 2, width: w, height: 1)) }
        if style & UInt8(BVT_STRIKE) != 0 { ctx.fill(CGRect(x: x, y: top + cellH / 2, width: w, height: 1)) }
    }

    // MARK: focus

    override func becomeFirstResponder() -> Bool {
        focused = true
        if let id = Int64(session.id) { TerminalLayout.shared.didFocus(id) }
        sendFocus(true)
        needsDisplay = true
        return true
    }

    override func resignFirstResponder() -> Bool {
        focused = false
        sendFocus(false)
        needsDisplay = true
        return true
    }

    private func sendFocus(_ on: Bool) {
        var buf = [CChar](repeating: 0, count: 8)
        let n = bvt_encode_focus(session.vt, on, &buf, buf.count)
        if n > 0 { session.send(buf.prefix(n).map { UInt8(bitPattern: $0) }) }
    }

    // MARK: keyboard

    private static func mods(_ f: NSEvent.ModifierFlags) -> UInt16 {
        var m: UInt16 = 0
        if f.contains(.shift) { m |= 1 << 0 }
        if f.contains(.control) { m |= 1 << 1 }
        if f.contains(.option) { m |= 1 << 2 }
        if f.contains(.command) { m |= 1 << 3 }
        if f.contains(.capsLock) { m |= 1 << 4 }
        return m
    }

    /// Text a key produced, minus control characters and AppKit's function-key private-use range.
    private static func printable(_ s: String?) -> String {
        guard let s, let u = s.unicodeScalars.first else { return "" }
        if u.value < 0x20 || u.value == 0x7F || (0xF700...0xF8FF).contains(u.value) { return "" }
        return s
    }

    private func encode(_ e: NSEvent, text: String) {
        let mods = Self.mods(e.modifierFlags)
        let consumed: UInt16 = text.isEmpty ? 0 : mods & (1 << 0 | 1 << 2)
        let unshifted = e.characters(byApplyingModifiers: [])?.unicodeScalars.first?.value ?? 0
        var out = [CChar](repeating: 0, count: 128)
        let utf8 = Array(text.utf8CString.dropLast())
        let n = utf8.withUnsafeBufferPointer { u in
            bvt_encode_key(session.vt, e.keyCode, mods, consumed, u.baseAddress, u.count, unshifted, false, &out, out.count)
        }
        if n > 0 {
            bvt_scroll_bottom(session.vt)
            selection = nil
            session.send(out.prefix(n).map { UInt8(bitPattern: $0) })
        }
    }

    override func keyDown(with e: NSEvent) {
        let f = e.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if f.contains(.command) { super.keyDown(with: e); return }
        // Burrow's own bindings (XTerm.vue): Shift+Enter → CSI u, Option+←/→ word jump, Option+⌫ word delete.
        if e.keyCode == 0x24, f == .shift { sendScrolled("\u{1b}[13;2u"); return }
        if f.contains(.option), !f.contains(.control) {
            if e.keyCode == 0x7B { sendScrolled("\u{1b}b"); return }
            if e.keyCode == 0x7C { sendScrolled("\u{1b}f"); return }
            if e.keyCode == 0x33 || e.keyCode == 0x75 { sendScrolled("\u{1b}\u{7f}"); return }
        }
        let fn = e.characters?.unicodeScalars.first.map { (0xF700...0xF8FF).contains($0.value) } ?? false
        if f.contains(.control) || fn || [0x24, 0x30, 0x33, 0x35, 0x4C].contains(e.keyCode), !hasMarkedText() {
            encode(e, text: Self.printable(e.characters))
            return
        }
        // Everything else goes through the input system so dead keys and IMEs compose (č, ů, …).
        let wasMarked = hasMarkedText()
        keyText = nil
        inKeyDown = true
        interpretKeyEvents([e])
        inKeyDown = false
        if hasMarkedText() { return }
        if let t = keyText {
            encode(e, text: t)
        } else if !wasMarked {
            encode(e, text: Self.printable(e.characters))
        }
    }

    private func sendScrolled(_ s: String) {
        bvt_scroll_bottom(session.vt)
        session.send(s)
    }

    override func performKeyEquivalent(with e: NSEvent) -> Bool {
        guard window?.firstResponder === self else { return false }
        let f = e.modifierFlags.intersection([.command, .shift, .option, .control])
        if f == [.command, .shift], e.charactersIgnoringModifiers?.lowercased() == "r" { session.repaint(); return true }
        guard f == .command else { return false }
        switch e.charactersIgnoringModifiers {
        case "c" where selection != nil: copy(nil); return true
        case "v": paste(nil); return true
        case "k": session.clear(); selection = nil; return true
        default: return false
        }
    }

    // MARK: NSTextInputClient

    func insertText(_ string: Any, replacementRange: NSRange) {
        let s = (string as? NSAttributedString)?.string ?? (string as? String) ?? ""
        marked = NSMutableAttributedString()
        needsDisplay = true
        if inKeyDown { keyText = (keyText ?? "") + s } else if !s.isEmpty { sendScrolled(s) }
    }

    override func doCommand(by selector: Selector) {} // keyDown encodes the raw key instead

    func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        marked = NSMutableAttributedString(string: (string as? NSAttributedString)?.string ?? (string as? String) ?? "")
        needsDisplay = true
    }

    func unmarkText() { marked = NSMutableAttributedString(); needsDisplay = true }
    func hasMarkedText() -> Bool { marked.length > 0 }
    func markedRange() -> NSRange { hasMarkedText() ? NSRange(location: 0, length: marked.length) : NSRange(location: NSNotFound, length: 0) }
    func selectedRange() -> NSRange { NSRange(location: NSNotFound, length: 0) }
    func validAttributesForMarkedText() -> [NSAttributedString.Key] { [] }
    func attributedSubstring(forProposedRange range: NSRange, actualRange: NSRangePointer?) -> NSAttributedString? { nil }
    func characterIndex(for point: NSPoint) -> Int { NSNotFound }

    /// Where the IME candidate window goes: at the cursor.
    func firstRect(forCharacterRange range: NSRange, actualRange: NSRangePointer?) -> NSRect {
        let r = NSRect(x: pad + CGFloat(frameInfo.cursor_x) * cellW, y: pad + CGFloat(frameInfo.cursor_y) * cellH, width: cellW, height: cellH)
        return window?.convertToScreen(convert(r, to: nil)) ?? r
    }

    // MARK: clipboard

    @objc func copy(_ sender: Any?) {
        guard let s = selection else { return }
        var (a, h) = (s.a, s.h)
        if (h.y, h.x) < (a.y, a.x) { swap(&a, &h) }
        let cols = Int(frameInfo.cols)
        var lines: [String] = []
        for y in a.y...h.y where y < Int(frameInfo.rows) {
            var line = ""
            for x in (y == a.y ? a.x : 0)...(y == h.y ? min(h.x, cols - 1) : cols - 1) {
                let c = cells[y * cols + x]
                if c.wide == UInt8(BVT_SPACER) { continue }
                line += c.len == 0 ? " " : cellText(c)
            }
            lines.append(line.replacingOccurrences(of: " +$", with: "", options: .regularExpression))
        }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(lines.joined(separator: "\n"), forType: .string)
    }

    @objc func paste(_ sender: Any?) {
        guard let s = NSPasteboard.general.string(forType: .string), !s.isEmpty else { return }
        var data = Array(s.utf8CString.dropLast())
        var out = [CChar](repeating: 0, count: data.count * 2 + 32)
        let n = bvt_encode_paste(session.vt, &data, data.count, &out, out.count)
        guard n <= out.count else { return }
        sendScrolled(String(decoding: out.prefix(n).map { UInt8(bitPattern: $0) }, as: UTF8.self))
    }

    // MARK: mouse

    private func cell(at e: NSEvent) -> (x: Int, y: Int) {
        let p = convert(e.locationInWindow, from: nil)
        let x = Int((p.x - pad) / cellW), y = Int((p.y - pad) / cellH)
        return (min(max(x, 0), max(Int(frameInfo.cols) - 1, 0)), min(max(y, 0), max(Int(frameInfo.rows) - 1, 0)))
    }

    /// The program asked for mouse reports and Shift isn't held (Shift = local selection, as in xterm).
    private func reporting(_ e: NSEvent) -> Bool {
        bvt_mouse_tracking(session.vt) && !e.modifierFlags.contains(.shift)
    }

    private var buttonsDown = 0

    /// Report a mouse event to the program. Positions go in as a virtual 10 px grid, so libghostty's integer
    /// cell size doesn't drift from our fractional one. ponytail: SGR-pixel mode sees that virtual grid; no
    /// motion reports without a button held (mode 1003 hover) — add a tracking area when a TUI needs it.
    private func report(_ e: NSEvent, action: Int32, button: Int32) {
        let p = convert(e.locationInWindow, from: nil)
        let cols = max(1, Int(frameInfo.cols)), rows = max(1, Int(frameInfo.rows))
        let x = Float(max(0, min(CGFloat(cols) - 0.01, (p.x - pad) / cellW)) * 10)
        let y = Float(max(0, min(CGFloat(rows) - 0.01, (p.y - pad) / cellH)) * 10)
        var out = [CChar](repeating: 0, count: 64)
        let n = bvt_encode_mouse(session.vt, action, button, Self.mods(e.modifierFlags), x, y,
                                 UInt32(cols * 10), UInt32(rows * 10), 10, 10, 0, buttonsDown > 0, &out, out.count)
        if n > 0 { session.send(out.prefix(n).map { UInt8(bitPattern: $0) }) }
    }

    private static let wordChars = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_./~:@%+=#?&*"))

    /// Viewport row as one string, plus where each cell starts in it (UTF-16 offsets) and back.
    private func rowText(_ y: Int) -> (text: String, start: [Int]) {
        let cols = Int(frameInfo.cols)
        var text = "", start: [Int] = []
        for x in 0..<cols {
            start.append(text.utf16.count)
            let c = cells[y * cols + x]
            if c.wide == UInt8(BVT_SPACER) { continue }
            text += c.len == 0 ? " " : cellText(c)
        }
        return (text, start)
    }

    private func col(of offset: Int, _ start: [Int]) -> Int { max(0, (start.lastIndex { $0 <= offset }) ?? 0) }

    /// ⌘-click: the http(s) URL under the pointer (WebLinksAddon). ponytail: one row only — a URL the
    /// program wrapped onto the next line opens cut short.
    private func url(at c: (x: Int, y: Int)) -> URL? {
        guard c.y < Int(frameInfo.rows) else { return nil }
        let (text, start) = rowText(c.y)
        let ns = text as NSString
        let re = try! NSRegularExpression(pattern: #"(https?|file)://[^\s"'<>`]+"#)
        for m in re.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            var s = ns.substring(with: m.range)
            while let last = s.last, ".,;:!?)]}".contains(last) { s.removeLast() }
            let lo = col(of: m.range.location, start), hi = col(of: m.range.location + (s as NSString).length - 1, start)
            if (lo...hi).contains(c.x) { return URL(string: s) }
        }
        return nil
    }

    /// Double-click = the word under the pointer, triple-click = the row.
    private func wordSelection(at c: (x: Int, y: Int), line: Bool) -> (a: (x: Int, y: Int), h: (x: Int, y: Int)) {
        let cols = Int(frameInfo.cols)
        if line { return ((0, c.y), (cols - 1, c.y)) }
        func isWord(_ x: Int) -> Bool {
            let cc = cells[c.y * cols + x]
            if cc.wide == UInt8(BVT_SPACER) { return true }
            return cc.len > 0 && cellText(cc).unicodeScalars.allSatisfy { Self.wordChars.contains($0) }
        }
        guard isWord(c.x) else { return (c, c) }
        var lo = c.x, hi = c.x
        while lo > 0, isWord(lo - 1) { lo -= 1 }
        while hi < cols - 1, isWord(hi + 1) { hi += 1 }
        return ((lo, c.y), (hi, c.y))
    }

    private var clickSelected = false

    override func mouseDown(with e: NSEvent) {
        window?.makeFirstResponder(self)
        let c = cell(at: e)
        if e.modifierFlags.contains(.command), let u = url(at: c) { NSWorkspace.shared.open(u); return }
        if reporting(e) { buttonsDown += 1; report(e, action: 0, button: 1); return }
        clickSelected = e.clickCount >= 2
        if clickSelected { selection = wordSelection(at: c, line: e.clickCount >= 3) } else { selection = (c, c) }
        needsDisplay = true
    }

    override func mouseDragged(with e: NSEvent) {
        if reporting(e) { report(e, action: 2, button: 1); return }
        guard let s = selection else { return }
        selection = (s.a, cell(at: e))
        clickSelected = false
        needsDisplay = true
    }

    override func mouseUp(with e: NSEvent) {
        if buttonsDown > 0, reporting(e) { buttonsDown = max(0, buttonsDown - 1); report(e, action: 1, button: 1); return }
        buttonsDown = 0
        if !clickSelected, let s = selection, s.a == s.h { selection = nil; needsDisplay = true } // a click, not a drag
    }

    override func rightMouseDown(with e: NSEvent) {
        guard reporting(e) else { return super.rightMouseDown(with: e) }
        buttonsDown += 1; report(e, action: 0, button: 2)
    }

    override func rightMouseUp(with e: NSEvent) {
        guard reporting(e) else { return super.rightMouseUp(with: e) }
        buttonsDown = max(0, buttonsDown - 1); report(e, action: 1, button: 2)
    }

    override func otherMouseDown(with e: NSEvent) {
        guard reporting(e) else { return super.otherMouseDown(with: e) }
        buttonsDown += 1; report(e, action: 0, button: 3)
    }

    override func otherMouseUp(with e: NSEvent) {
        guard reporting(e) else { return super.otherMouseUp(with: e) }
        buttonsDown = max(0, buttonsDown - 1); report(e, action: 1, button: 3)
    }

    override func resetCursorRects() { addCursorRect(bounds, cursor: .iBeam) }

    /// Wheel: reported to a mouse-tracking TUI, arrow keys on a full-screen app without tracking (xterm's
    /// alternate scroll), else the local scrollback.
    override func scrollWheel(with e: NSEvent) {
        let dy = e.hasPreciseScrollingDeltas ? e.scrollingDeltaY : e.scrollingDeltaY * cellH
        scrollAccum += dy
        let rows = Int(scrollAccum / cellH)
        guard rows != 0 else { return }
        scrollAccum -= CGFloat(rows) * cellH
        if reporting(e) {
            for _ in 0..<abs(rows) { report(e, action: 0, button: rows > 0 ? 4 : 5) }
            return
        }
        if frameInfo.alt_screen {
            var out = [CChar](repeating: 0, count: 16)
            for _ in 0..<abs(rows) {
                let n = bvt_encode_key(session.vt, rows > 0 ? 0x7E : 0x7D, 0, 0, nil, 0, 0, false, &out, out.count)
                if n > 0 { session.send(out.prefix(n).map { UInt8(bitPattern: $0) }) }
            }
            return
        }
        bvt_scroll(session.vt, -rows)
        selection = nil
        needsDisplay = true
    }
}
