package investigate

import "testing"

func TestOperationToFuncName_GRPCStyle(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"/api.Service/Method", "Method"},
		{"/grpc.health.v1.Health/Check", "Check"},
		{"/oxpulse.chat.v1.ChatService/SendMessage", "SendMessage"},
	}
	for _, c := range cases {
		if got := OperationToFuncName(c.in); got != c.want {
			t.Errorf("OperationToFuncName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOperationToFuncName_HTTPStyle(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"GET /api/v1/users", "users"},
		{"POST /api/v1/messages", "messages"},
		{"PUT /api/v1/posts/:id", "posts"},
		{"GET /", ""},
	}
	for _, c := range cases {
		if got := OperationToFuncName(c.in); got != c.want {
			t.Errorf("OperationToFuncName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOperationToFuncName_PlainFunc(t *testing.T) {
	t.Parallel()
	if got := OperationToFuncName("ProcessMessage"); got != "ProcessMessage" {
		t.Errorf("got %q", got)
	}
	if got := OperationToFuncName("(*Server).Handle"); got != "Handle" {
		t.Errorf("got %q", got)
	}
}

func TestOperationToFuncName_Empty(t *testing.T) {
	t.Parallel()
	if got := OperationToFuncName(""); got != "" {
		t.Errorf("got %q for empty input", got)
	}
}
