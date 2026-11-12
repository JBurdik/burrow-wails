import Foundation

// MARK: - Sub-agent profiles (src/lib/subagentProfiles.ts) — config key `subagentProfiles`

struct SubagentProfile: Equatable {
    var id: String, name: String, instructions: String, agentId: String, model: String, permissionMode: String
    var json: JSON { ["id": id, "name": name, "instructions": instructions, "agentId": agentId, "model": model, "permissionMode": permissionMode] }
}

enum SubagentProfiles {
    static let defaults: [SubagentProfile] = [
        .init(id: "scout", name: "Scout", instructions: "Investigate the task and locate relevant code. Do not change files. Report concrete findings, file paths, and a suggested approach to the parent agent.", agentId: "", model: "", permissionMode: "auto"),
        .init(id: "worker", name: "Worker", instructions: "Implement the assigned task within its stated scope. Respect repository instructions and other agents' work. Run appropriate checks and report the changes, validation, and any remaining issues.", agentId: "", model: "", permissionMode: "auto"),
        .init(id: "reviewer", name: "Reviewer", instructions: "Review the assigned changes for bugs, regressions, and missing validation. Do not change files. Report actionable findings ordered by severity, with file paths, evidence, and suggested fixes. Say explicitly when no findings remain.", agentId: "", model: "", permissionMode: "auto"),
    ]
    static let modes = [("default", "Supervised"), ("auto", "Auto"), ("plan", "Plan (no workspace writes)")]

    /// normalizeSubagentProfiles: always the three built-ins, saved fields layered on.
    static func normalize(_ v: Any?) -> [SubagentProfile] {
        let rows = v as? [JSON] ?? []
        return defaults.map { d in
            guard let s = rows.first(where: { $0.s("id") == d.id }) else { return d }
            var p = d
            if let t = s["instructions"] as? String { p.instructions = t }
            p.agentId = s.s("agentId"); p.model = s.s("model")
            if ["default", "auto", "plan"].contains(s.s("permissionMode")) { p.permissionMode = s.s("permissionMode") }
            return p
        }
    }

    @MainActor static func load(_ b: Burrow?) async -> [SubagentProfile] {
        if ConfigStore.shared.values.isEmpty, let b { await ConfigStore.shared.attach(b) }
        return normalize(ConfigStore.shared.values["subagentProfiles"])
    }
}

// MARK: - Project scripts + settings (<repo>/.burrow/config.toml, src/stores/scripts.ts)

struct ProjectScript: Equatable, Identifiable {
    var id: String, name: String, steps: [String], continueOnError: Bool, runOnWorktreeCreate: Bool
    var icon: String = "", color: String = ""
    var commandLine: String {
        steps.map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }.joined(separator: continueOnError ? " ; " : " && ")
    }
}

struct ProjectScripts: Equatable {
    var settings: [String: String] = [:]   // claude_config_dir, env_file
    var scripts: [ProjectScript] = []

    @MainActor static func load(_ b: Burrow?, path: String) async -> ProjectScripts {
        guard let b, !path.isEmpty,
              let t = try? await b.call("read_text_file", ["path": path + "/.burrow/config.toml"]) as? String else { return .init() }
        return parse(t)
    }

    @MainActor func save(_ b: Burrow, path: String) async throws {
        try await b.call("write_text_file", ["path": path + "/.burrow/config.toml", "content": serialize()])
    }

    // ponytail: the same line-based TOML subset scripts.ts reads/writes; not a TOML parser.
    static func value(_ raw: String) -> Any {
        let v = raw.trimmingCharacters(in: .whitespaces)
        if v == "true" { return true }
        if v == "false" { return false }
        if v.hasPrefix("["), v.hasSuffix("]") {
            var items: [String] = [], cur = "", inQ = false, prev: Character = " "
            for ch in v.dropFirst().dropLast() {
                defer { prev = ch }
                if ch == "\"", prev != "\\" { inQ.toggle(); continue }
                if ch == ",", !inQ { items.append(cur); cur = ""; continue }
                cur.append(ch)
            }
            if !cur.trimmingCharacters(in: .whitespaces).isEmpty { items.append(cur.trimmingCharacters(in: .whitespaces)) }
            return items.map { unescape($0.trimmingCharacters(in: .whitespaces)) }
        }
        if v.count >= 2, (v.hasPrefix("\"") && v.hasSuffix("\"")) || (v.hasPrefix("'") && v.hasSuffix("'")) {
            return unescape(String(v.dropFirst().dropLast()))
        }
        return v
    }
    static func unescape(_ s: String) -> String { s.replacingOccurrences(of: "\\\"", with: "\"").replacingOccurrences(of: "\\\\", with: "\\") }
    static func escape(_ s: String) -> String { s.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"") }

    static func kv(_ line: Substring) -> (String, String)? {
        guard let eq = line.firstIndex(of: "=") else { return nil }
        let k = line[..<eq].trimmingCharacters(in: .whitespaces)
        guard !k.isEmpty, k.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" }) else { return nil }
        return (k, line[line.index(after: eq)...].trimmingCharacters(in: .whitespaces))
    }

    static func parse(_ text: String) -> ProjectScripts {
        var out = ProjectScripts()
        var section = ""
        var raw: JSON? = nil
        func flush() {
            guard let r = raw, !(r.s("id").isEmpty && r.s("name").isEmpty) else { return }
            out.scripts.append(ProjectScript(
                id: r.s("id").isEmpty ? "script-\(out.scripts.count + 1)" : r.s("id"),
                name: r.s("name").isEmpty ? "Script" : r.s("name"),
                steps: r["steps"] as? [String] ?? [], continueOnError: r["continueOnError"] as? Bool ?? false,
                runOnWorktreeCreate: r["runOnWorktreeCreate"] as? Bool ?? false, icon: r.s("icon"), color: r.s("color")))
        }
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            let t = line.trimmingCharacters(in: .whitespaces)
            if t == "[[scripts]]" { flush(); raw = [:]; section = "scripts"; continue }
            if t.hasPrefix("[") { flush(); raw = nil; section = t; continue }
            guard let (k, v) = kv(line) else { continue }
            if section == "[settings]", let s = value(v) as? String { out.settings[k] = s }
            if section == "scripts" { raw?[k] = value(v) }
        }
        flush()
        return out
    }

    func serialize() -> String {
        var s = ""
        let pairs = settings.filter { !$0.value.isEmpty }.sorted { $0.key < $1.key }
        if !pairs.isEmpty {
            s += "[settings]\n" + pairs.map { "\($0.key) = \"\(Self.escape($0.value))\"" }.joined(separator: "\n") + "\n\n"
        }
        s += scripts.map { sc in
            var b = "[[scripts]]\nid = \"\(Self.escape(sc.id))\"\nname = \"\(Self.escape(sc.name))\"\n"
            b += "steps = [" + sc.steps.map { "\"\(Self.escape($0))\"" }.joined(separator: ", ") + "]\n"
            b += "continueOnError = \(sc.continueOnError)\n"
            if sc.runOnWorktreeCreate { b += "runOnWorktreeCreate = true\n" }
            if !sc.color.isEmpty { b += "color = \"\(Self.escape(sc.color))\"\n" }
            if !sc.icon.isEmpty { b += "icon = \"\(Self.escape(sc.icon))\"\n" }
            return b
        }.joined(separator: "\n")
        return s
    }

    /// Round-trip self-check; run from a debug build or a scratch harness.
    static func check() {
        let src = """
        [settings]
        env_file = ".env.local"

        [[scripts]]
        id = "a"
        name = "Dev \\"x\\""
        steps = ["pnpm i", "echo \\"a, b\\""]
        continueOnError = false
        runOnWorktreeCreate = true
        """
        let p = parse(src)
        assert(p.settings["env_file"] == ".env.local")
        assert(p.scripts.count == 1 && p.scripts[0].name == "Dev \"x\"")
        assert(p.scripts[0].steps == ["pnpm i", "echo \"a, b\""], "\(p.scripts[0].steps)")
        assert(p.scripts[0].runOnWorktreeCreate && p.scripts[0].commandLine == "pnpm i && echo \"a, b\"")
        assert(parse(p.serialize()) == p)
        assert(ShellControl.quote("it's") == #"'it'\''s'"#)
    }
}
