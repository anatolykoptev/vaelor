import java.util.List;
import java.util.Map;

// Enum WITH methods — edge case
enum Status {
    ACTIVE, INACTIVE;

    public String display() {
        return name().toLowerCase();
    }
}

// Nested class
public class Outer {
    private int value;

    public Outer(int value) {
        this.value = value;
    }

    public int getValue() {
        return value;
    }

    // Static nested class
    public static class Inner {
        public void doWork() {
            System.out.println("working");
        }
    }

    // Interface inside class
    interface Callback {
        void onComplete(String result);
    }
}
