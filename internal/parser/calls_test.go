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

// TestExtractCalls_GoArgRefTagging asserts that identifier arguments and
// member-access selectors inside a call (e.g. `opts.Slug`, `ctx`) are emitted
// as CallSite entries tagged IsArgRef=true, while the actual call target
// (`helper`) is tagged IsArgRef=false. The call graph uses this to drop
// unresolved argref entries (vars / member access) from callee lists.
func TestExtractCalls_GoArgRefTagging(t *testing.T) {
	source := []byte(`package x

type Opts struct{ Slug string }

func helper(int) int { return 0 }

func f(opts Opts, ctx int) int {
	_ = opts.Slug // selector_expression on its own — no call_expression context
	return helper(ctx) + helper(opts.Slug)
}
`)
	calls, err := ExtractCalls("x.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	var helperPrimary, ctxArgRef, slugArgRef int
	for _, c := range calls {
		switch c.Name {
		case "helper":
			if !c.IsArgRef {
				helperPrimary++
			}
		case "ctx":
			if c.IsArgRef {
				ctxArgRef++
			}
		case "Slug":
			if c.IsArgRef {
				slugArgRef++
			}
		}
		// `Slug` from the bare `_ = opts.Slug` line MUST NOT appear at all —
		// it is a selector_expression outside any call_expression.
		if c.Name == "Slug" && c.Line == 8 {
			t.Errorf("bare member access leaked as CallSite: %+v", c)
		}
	}
	if helperPrimary < 2 {
		t.Errorf("expected helper as primary (non-argref) call >=2, got %d", helperPrimary)
	}
	if ctxArgRef == 0 {
		t.Errorf("expected ctx captured as argref, got 0")
	}
	if slugArgRef == 0 {
		t.Errorf("expected opts.Slug captured as argref, got 0")
	}
}

// TestExtractCalls_PythonArgRefTagging mirrors the Go test for Python.
func TestExtractCalls_PythonArgRefTagging(t *testing.T) {
	source := []byte(`
def helper(x):
    return x

def f(opts, ctx):
    return helper(ctx)
`)
	calls, err := ExtractCalls("x.py", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, ctxArgRef int
	for _, c := range calls {
		if c.Name == "helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "ctx" && c.IsArgRef {
			ctxArgRef++
		}
	}
	if helperPrimary == 0 {
		t.Errorf("helper missing as primary call")
	}
	if ctxArgRef == 0 {
		t.Errorf("ctx missing as argref")
	}
}

// TestExtractCalls_TypeScriptArgRefTagging covers TS argument-position refs.
func TestExtractCalls_TypeScriptArgRefTagging(t *testing.T) {
	source := []byte(`
function helper(x: number): number { return x; }
function f(ctx: number) { return helper(ctx); }
`)
	calls, err := ExtractCalls("x.ts", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, ctxArgRef int
	for _, c := range calls {
		if c.Name == "helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "ctx" && c.IsArgRef {
			ctxArgRef++
		}
	}
	if helperPrimary == 0 {
		t.Errorf("helper missing as primary call")
	}
	if ctxArgRef == 0 {
		t.Errorf("ctx missing as argref")
	}
}

// TestExtractCalls_JavaArgRefTagging covers Java method_invocation argrefs.
func TestExtractCalls_JavaArgRefTagging(t *testing.T) {
	source := []byte(`
class X {
    int helper(int x) { return x; }
    int f(int ctx) { return helper(ctx); }
}
`)
	calls, err := ExtractCalls("X.java", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, ctxArgRef int
	for _, c := range calls {
		if c.Name == "helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "ctx" && c.IsArgRef {
			ctxArgRef++
		}
	}
	if helperPrimary == 0 {
		t.Errorf("helper missing as primary call")
	}
	if ctxArgRef == 0 {
		t.Errorf("ctx missing as argref")
	}
}

// TestExtractCalls_RustNoArgRefNoise — Rust's call query has no argument-list
// wildcard, so plain identifier args (`ctx`) MUST NOT appear as CallSites.
// Acts as a regression guard against importing the noisy heuristic to Rust.
func TestExtractCalls_RustNoArgRefNoise(t *testing.T) {
	source := []byte(`
fn helper(x: i32) -> i32 { x }
fn f(ctx: i32) -> i32 { helper(ctx) }
`)
	calls, err := ExtractCalls("x.rs", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	for _, c := range calls {
		if c.Name == "ctx" {
			t.Errorf("Rust extracted bare identifier arg as call: %+v", c)
		}
	}
}

// TestExtractCalls_CArgRefTagging verifies that the C call query correctly tags
// the callee (helper) as IsArgRef=false and the argument (arg) as IsArgRef=true.
func TestExtractCalls_CArgRefTagging(t *testing.T) {
	source := []byte(`
void helper(int x) {}

void f(int arg) {
	helper(arg);
}
`)
	calls, err := ExtractCalls("x.c", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, argArgRef int
	for _, c := range calls {
		if c.Name == "helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "arg" && c.IsArgRef {
			argArgRef++
		}
	}
	if helperPrimary != 1 {
		t.Errorf("C: expected helper as primary (non-argref) call exactly once, got %d", helperPrimary)
	}
	if argArgRef != 1 {
		t.Errorf("C: expected arg as argref exactly once, got %d", argArgRef)
	}
}

// TestExtractCalls_CppArgRefTagging verifies that the C++ call query correctly
// tags the callee (helper) as IsArgRef=false and the argument (arg) as IsArgRef=true.
func TestExtractCalls_CppArgRefTagging(t *testing.T) {
	source := []byte(`
int helper(int x) { return x; }

int f(int arg) {
	return helper(arg);
}
`)
	calls, err := ExtractCalls("x.cpp", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, argArgRef int
	for _, c := range calls {
		if c.Name == "helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "arg" && c.IsArgRef {
			argArgRef++
		}
	}
	if helperPrimary != 1 {
		t.Errorf("C++: expected helper as primary (non-argref) call exactly once, got %d", helperPrimary)
	}
	if argArgRef != 1 {
		t.Errorf("C++: expected arg as argref exactly once, got %d", argArgRef)
	}
}

// TestExtractCalls_RubyArgRefTagging verifies that the Ruby call query tags
// the callee (helper) as IsArgRef=false and the argument (arg) as IsArgRef=true.
// In Ruby, a bare method call like helper(arg) is a `call` node; tree-sitter
// maps it to call.method (not call.function), so helper appears with IsArgRef=false
// via the @call.method capture.
func TestExtractCalls_RubyArgRefTagging(t *testing.T) {
	source := []byte(`
def helper(x)
  x
end

def f(arg)
  helper(arg)
end
`)
	calls, err := ExtractCalls("x.rb", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, argArgRef int
	for _, c := range calls {
		if c.Name == "helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "arg" && c.IsArgRef {
			argArgRef++
		}
	}
	if helperPrimary != 1 {
		t.Errorf("Ruby: expected helper as primary (non-argref) call exactly once, got %d", helperPrimary)
	}
	if argArgRef != 1 {
		t.Errorf("Ruby: expected arg as argref exactly once, got %d", argArgRef)
	}
}

// TestExtractCalls_PHPArgRefTagging verifies that the PHP call query tags the
// callee (helper) as IsArgRef=false and a bare function-name argument (callback)
// as IsArgRef=true. PHP argref captures (name) nodes (bare identifiers, i.e.
// function references) in argument position — not $variables, which are
// variable_name nodes and thus not captured as argrefs.
func TestExtractCalls_PHPArgRefTagging(t *testing.T) {
	source := []byte(`<?php
function helper($fn) { $fn(); }
function callback() {}

function f() {
	helper(callback);
}
`)
	calls, err := ExtractCalls("x.php", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, callbackArgRef int
	for _, c := range calls {
		if c.Name == "helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "callback" && c.IsArgRef {
			callbackArgRef++
		}
	}
	if helperPrimary != 1 {
		t.Errorf("PHP: expected helper as primary (non-argref) call exactly once, got %d", helperPrimary)
	}
	if callbackArgRef != 1 {
		t.Errorf("PHP: expected callback as argref exactly once, got %d", callbackArgRef)
	}
}

// TestExtractCalls_CSharpArgRefTagging verifies that the C# call query tags
// the callee (helper) as IsArgRef=false and the argument (arg) as IsArgRef=true.
func TestExtractCalls_CSharpArgRefTagging(t *testing.T) {
	source := []byte(`
class X {
    int Helper(int x) { return x; }

    int F(int arg) {
        return Helper(arg);
    }
}
`)
	calls, err := ExtractCalls("x.cs", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	var helperPrimary, argArgRef int
	for _, c := range calls {
		if c.Name == "Helper" && !c.IsArgRef {
			helperPrimary++
		}
		if c.Name == "arg" && c.IsArgRef {
			argArgRef++
		}
	}
	if helperPrimary != 1 {
		t.Errorf("C#: expected Helper as primary (non-argref) call exactly once, got %d", helperPrimary)
	}
	if argArgRef != 1 {
		t.Errorf("C#: expected arg as argref exactly once, got %d", argArgRef)
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
