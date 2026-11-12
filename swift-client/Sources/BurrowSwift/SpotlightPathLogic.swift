import Foundation

/// Pure path-completion logic for the in-app project picker (PathPicker.vue's twin). Paths are the CORE's,
/// never this Mac's: `home` is the core's `home_dir`, listings come from `read_dir_shallow`.
enum PathLogic {
    /// "~/code/bur" → ("~/code/", "bur"). Everything after the last "/" filters the listing of the dir before it.
    static func split(_ path: String) -> (dir: String, segment: String) {
        guard let slash = path.lastIndex(of: "/") else { return ("", path) }
        return (String(path[...slash]), String(path[path.index(after: slash)...]))
    }

    /// "~" / "~/x" → home + rest; anything else unchanged ("~foo" is not a home path).
    static func expand(_ path: String, home: String) -> String {
        guard path == "~" || path.hasPrefix("~/"), !home.isEmpty else { return path }
        let h = home.hasSuffix("/") ? String(home.dropLast()) : home
        return h + path.dropFirst()
    }

    /// Absolute target for adding: expanded, trailing "/" dropped (root stays "/").
    static func target(_ path: String, home: String) -> String {
        var p = expand(path, home: home)
        while p.count > 1, p.hasSuffix("/") { p.removeLast() }
        return p
    }

    /// "~/code/" → "~/"; "/Users/" → "/"; "~/" → "/Users/" can't be known without home, so expand first.
    static func parent(_ dir: String, home: String) -> String {
        let trimmed = target(dir, home: home)
        guard let cut = trimmed.lastIndex(of: "/"), cut != trimmed.startIndex else { return "/" }
        return String(trimmed[...cut])
    }

    /// Fuzzy rank of `name` for `q` (lowercased): 0 prefix, 1 substring, 2 subsequence, nil = no match.
    static func rank(_ name: String, _ q: String) -> Int? {
        if q.isEmpty { return 0 }
        let n = name.lowercased()
        if n.hasPrefix(q) { return 0 }
        if n.contains(q) { return 1 }
        var it = n.makeIterator()
        for c in q { while true { guard let x = it.next() else { return nil }; if x == c { break } } }
        return 2
    }

    /// Directory names under the typed segment: hidden ones only when the segment starts with ".", best rank first.
    static func filter(_ dirs: [String], segment: String) -> [String] {
        let q = segment.lowercased()
        return dirs.filter { !$0.hasPrefix(".") || q.hasPrefix(".") }
            .compactMap { n in rank(n, q).map { (n, $0) } }
            .sorted { ($0.1, $0.0.lowercased()) < ($1.1, $1.0.lowercased()) }
            .map(\.0)
    }

    /// A new project's folder name: one path component, not "." / "..".
    static func validName(_ name: String) -> Bool {
        let n = name.trimmingCharacters(in: .whitespaces)
        return !n.isEmpty && n != "." && n != ".." && !n.contains("/")
    }

    static func join(_ dir: String, _ name: String) -> String {
        (dir.hasSuffix("/") ? dir : dir + "/") + name
    }

    static func selfCheck() {
        assert(split("~/code/bur") == ("~/code/", "bur"))
        assert(split("~/") == ("~/", ""))
        assert(split("/") == ("/", ""))
        assert(split("abc") == ("", "abc"))
        assert(expand("~/code", home: "/Users/x") == "/Users/x/code")
        assert(expand("~", home: "/Users/x/") == "/Users/x")
        assert(expand("~foo", home: "/Users/x") == "~foo")
        assert(expand("/tmp", home: "/Users/x") == "/tmp")
        assert(target("~/code/", home: "/Users/x") == "/Users/x/code")
        assert(target("/", home: "/h") == "/")
        assert(parent("~/code/", home: "/Users/x") == "/Users/x/")
        assert(parent("/Users/", home: "") == "/")
        assert(parent("/", home: "") == "/")
        let dirs = ["burrow", "Bar", ".git", "sub-burrow", "b_u_r", "zeta"]
        assert(filter(dirs, segment: "") == ["b_u_r", "Bar", "burrow", "sub-burrow", "zeta"])
        assert(filter(dirs, segment: "bur") == ["burrow", "sub-burrow", "b_u_r"])
        assert(filter(dirs, segment: ".") == [".git"])
        assert(filter(dirs, segment: "xyz").isEmpty)
        assert(validName("app") && !validName("a/b") && !validName("..") && !validName("  "))
        assert(join("~/code", "x") == "~/code/x" && join("~/", "x") == "~/x")
    }
}
