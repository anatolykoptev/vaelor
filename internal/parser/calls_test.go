package parser

import (
	"testing"
)

func TestExtractCalls_Go(t *testing.T) {
	source := []byte(`package main

import "fmt"

func helper() int {
	return 42
}

func main() {
	x := helper()
	fmt.Println(x)
	s := &Server{}
	s.Start()
}
`)
	calls, err := ExtractCalls("main.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	if len(calls) < 3 {
		t.Fatalf("got %d calls, want >= 3", len(calls))
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	for _, want := range []string{"helper", "Println", "Start"} {
		if !found[want] {
			t.Errorf("missing call to %q in extracted calls", want)
		}
	}

	// Verify receiver extraction
	for _, c := range calls {
		switch c.Name {
		case "Println":
			if c.Receiver != "fmt" {
				t.Errorf("Println receiver = %q, want %q", c.Receiver, "fmt")
			}
		case "Start":
			if c.Receiver != "s" {
				t.Errorf("Start receiver = %q, want %q", c.Receiver, "s")
			}
		case "helper":
			if c.Receiver != "" {
				t.Errorf("helper receiver = %q, want empty", c.Receiver)
			}
		}
		if c.File != "main.go" {
			t.Errorf("call %s file = %q, want %q", c.Name, c.File, "main.go")
		}
		if c.Line == 0 {
			t.Errorf("call %s has line 0", c.Name)
		}
	}
}

func TestExtractCalls_Python(t *testing.T) {
	source := []byte(`
def helper():
    return 42

def main():
    x = helper()
    print(x)
    obj.process()
`)
	calls, err := ExtractCalls("main.py", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	for _, want := range []string{"helper", "print", "process"} {
		if !found[want] {
			t.Errorf("missing call to %q", want)
		}
	}

	for _, c := range calls {
		switch c.Name {
		case "process":
			if c.Receiver != "obj" {
				t.Errorf("process receiver = %q, want %q", c.Receiver, "obj")
			}
		case "helper", "print":
			if c.Receiver != "" {
				t.Errorf("%s receiver = %q, want empty", c.Name, c.Receiver)
			}
		}
	}
}

func TestExtractCalls_Unsupported(t *testing.T) {
	calls, err := ExtractCalls("readme.txt", []byte("hello"), ParseOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("got %d calls for unsupported file, want 0", len(calls))
	}
}
