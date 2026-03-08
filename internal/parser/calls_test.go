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

func TestExtractCalls_GoFuncRef(t *testing.T) {
	source := []byte(`package main

import "sync"

func initStealth() {}
func renderHeading() {}

var once sync.Once

func setup() {
	Register("heading", renderHeading)
	once.Do(initStealth)
}
`)
	calls, err := ExtractCalls("main.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	for _, want := range []string{"renderHeading", "initStealth"} {
		if !found[want] {
			t.Errorf("missing function reference %q in extracted calls", want)
		}
	}
}

func TestExtractCalls_PHP(t *testing.T) {
	source := []byte(`<?php
function helper($x) { return $x + 1; }

class Controller {
    public function index() {
        helper($this);
        $this->validate();
        User::all();
    }
    private function validate() {}
}
`)
	calls, err := ExtractCalls("app.php", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	for _, want := range []string{"helper", "validate", "all"} {
		if !found[want] {
			t.Errorf("missing call to %q", want)
		}
	}

	// Also verify symbol extraction works for PHP
	result, err := ParseFile("app.php", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	symFound := map[string]bool{}
	for _, sym := range result.Symbols {
		symFound[sym.Name] = true
	}

	for _, want := range []string{"helper", "Controller", "index", "validate"} {
		if !symFound[want] {
			t.Errorf("missing symbol %q", want)
		}
	}
}

func TestExtractCalls_PHPNewExpression(t *testing.T) {
	source := []byte(`<?php
class Settings {
    public function __construct() {}
    public function register() {}
}

class Plugin {
    public function init() {
        $settings = new Settings();
        $settings->register();
        $license = new \GigienaTeksta\License();
    }
}
`)
	calls, err := ExtractCalls("plugin.php", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	if !found["register"] {
		t.Error("missing call to 'register'")
	}
	if !found["Settings"] {
		t.Error("missing call to 'Settings' from new expression")
	}
	if !found["License"] {
		t.Error("missing call to 'License' from qualified new expression")
	}
}

func TestExtractCalls_JSXAttributeRef(t *testing.T) {
	source := []byte(`
import { useState } from 'react';

const handleReplace = (word) => {
    console.log(word);
};

const handleCheck = () => {
    checkText();
};

const Component = () => {
    return (
        <div>
            <Button onClick={handleCheck} />
            <Button onClick={() => handleReplace('word')} />
        </div>
    );
};

export default Component;
`)
	calls, err := ExtractCalls("component.tsx", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	// handleCheck — JSX attribute reference (not a call expression)
	if !found["handleCheck"] {
		t.Error("missing JSX reference to 'handleCheck'")
	}

	// handleReplace — called inside arrow function in JSX attribute
	if !found["handleReplace"] {
		t.Error("missing call to 'handleReplace'")
	}

	// checkText — regular call inside handleCheck function
	if !found["checkText"] {
		t.Error("missing call to 'checkText'")
	}
}

func TestExtractCalls_GoStructLiteralFuncRef(t *testing.T) {
	source := []byte(`package main

type CrossDeps struct {
	FetchViewsFn      func()
	FetchPlacePostsFn func()
}

func exportFetchViews()      {}
func exportFetchPlacePosts() {}

func setup() {
	Register(CrossDeps{
		FetchViewsFn:      exportFetchViews,
		FetchPlacePostsFn: exportFetchPlacePosts,
	})
}
`)
	calls, err := ExtractCalls("main.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	for _, want := range []string{"exportFetchViews", "exportFetchPlacePosts"} {
		if !found[want] {
			t.Errorf("missing struct literal function reference %q in extracted calls", want)
		}
	}
}

func TestExtractCalls_GoStructLiteralQualifiedRef(t *testing.T) {
	source := []byte(`package main

type Deps struct {
	Handler func()
}

func setup() {
	d := Deps{Handler: pkg.MyHandler}
	_ = d
}
`)
	calls, err := ExtractCalls("main.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	found := map[string]bool{}
	for _, c := range calls {
		found[c.Name] = true
	}

	if !found["MyHandler"] {
		t.Error("missing qualified struct literal reference 'MyHandler'")
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
