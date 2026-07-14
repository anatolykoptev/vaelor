package sample

class Greeter(val name: String) {
    fun greet(): String {
        return build(name)
    }
}

fun build(who: String): String {
    return "Hello, $who"
}
