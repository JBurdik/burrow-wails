// burrow-fm: Apple's on-device Foundation Model as a one-shot text generator,
// in the same contract as textgen.go's other providers: prompt on stdin, JSON
// schema as an argument, the generated JSON object on stdout.
//
//   burrow-fm --check              → exit 0 when the model can answer, else the reason on stderr
//   burrow-fm --schema '<json>'    → guided generation into that schema
//   burrow-fm                      → plain text answer
//
// Build: swiftc -O -parse-as-library main.swift -o burrow-fm (macOS 26+ SDK).

import Foundation
import FoundationModels

@main
struct BurrowFM {
    static func main() async {
        let args = CommandLine.arguments
        let model = SystemLanguageModel.default
        if case .unavailable(let reason) = model.availability {
            fail("Apple Intelligence model unavailable: \(reason)", code: 3)
        }
        if args.contains("--check") { exit(0) }

        let prompt = String(decoding: FileHandle.standardInput.readDataToEndOfFile(), as: UTF8.self)
        guard !prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { fail("empty prompt") }

        let session = LanguageModelSession(model: model)
        do {
            if let i = args.firstIndex(of: "--schema"), i + 1 < args.count {
                guard let obj = try JSONSerialization.jsonObject(with: Data(args[i + 1].utf8)) as? [String: Any] else {
                    fail("schema must be a JSON object")
                }
                let schema = try GenerationSchema(root: dynamicSchema(obj, name: "Output"), dependencies: [])
                let response = try await session.respond(to: prompt, schema: schema)
                print(response.content.jsonString)
            } else {
                print(try await session.respond(to: prompt).content)
            }
        } catch {
            fail("\(error)")
        }
    }

    // The JSON Schema subset textgen.go uses: object / string (+enum) / array /
    // number / integer / boolean. Nested objects get path names so they stay
    // unique (DynamicGenerationSchema names are global within one schema).
    static func dynamicSchema(_ s: [String: Any], name: String) throws -> DynamicGenerationSchema {
        let desc = s["description"] as? String
        switch s["type"] as? String {
        case "object":
            let props = s["properties"] as? [String: Any] ?? [:]
            let required = Set(s["required"] as? [String] ?? [])
            let properties = try props.keys.sorted().map { key in
                DynamicGenerationSchema.Property(
                    name: key,
                    description: (props[key] as? [String: Any])?["description"] as? String,
                    schema: try dynamicSchema(props[key] as? [String: Any] ?? [:], name: "\(name)_\(key)"),
                    isOptional: !required.contains(key)
                )
            }
            return DynamicGenerationSchema(name: name, description: desc, properties: properties)
        case "array":
            return DynamicGenerationSchema(arrayOf: try dynamicSchema(s["items"] as? [String: Any] ?? ["type": "string"], name: "\(name)_item"))
        case "string":
            if let choices = s["enum"] as? [String] {
                return DynamicGenerationSchema(name: name, description: desc, anyOf: choices)
            }
            return DynamicGenerationSchema(type: String.self)
        case "integer":
            return DynamicGenerationSchema(type: Int.self)
        case "number":
            return DynamicGenerationSchema(type: Double.self)
        case "boolean":
            return DynamicGenerationSchema(type: Bool.self)
        default:
            throw NSError(domain: "burrow-fm", code: 1, userInfo: [NSLocalizedDescriptionKey: "unsupported schema at \(name)"])
        }
    }

    static func fail(_ message: String, code: Int32 = 1) -> Never {
        FileHandle.standardError.write(Data((message + "\n").utf8))
        exit(code)
    }
}
