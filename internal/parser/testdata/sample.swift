import Foundation

struct Greeter {
    let name: String

    func greet() -> String {
        return build(name)
    }
}

func build(_ who: String) -> String {
    return "Hello, \(who)"
}
