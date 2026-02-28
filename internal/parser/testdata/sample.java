import java.util.List;
import java.util.HashMap;

public class Config {
    private String host;
    private int port;

    public Config(String host, int port) {
        this.host = host;
        this.port = port;
    }

    public String address() {
        return host + ":" + port;
    }
}

interface Handler {
    void handle(String request);
}

enum Status {
    ACTIVE,
    INACTIVE
}
