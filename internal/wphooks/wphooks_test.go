package wphooks

import "testing"

func TestIsKnownHook_CoreAction(t *testing.T) {
	if !IsKnownHook("init") {
		t.Error("init should be a known WP core hook")
	}
}

func TestIsKnownHook_CoreFilter(t *testing.T) {
	if !IsKnownHook("the_content") {
		t.Error("the_content should be a known WP core hook")
	}
}

func TestIsKnownHook_Unknown(t *testing.T) {
	if IsKnownHook("my_totally_custom_hook_xyz") {
		t.Error("custom hook should not be known")
	}
}

func TestLookup_Init(t *testing.T) {
	h := Lookup("init")
	if h == nil {
		t.Fatal("Lookup(init) returned nil")
	}
	if h.Type != "action" {
		t.Errorf("Type = %q, want action", h.Type)
	}
	if h.Name != "init" {
		t.Errorf("Name = %q, want init", h.Name)
	}
}

func TestLookup_WPEnqueueScripts(t *testing.T) {
	h := Lookup("wp_enqueue_scripts")
	if h == nil {
		t.Fatal("Lookup(wp_enqueue_scripts) returned nil")
	}
	if h.Type != "action" {
		t.Errorf("Type = %q, want action", h.Type)
	}
}

func TestLookup_TheContent(t *testing.T) {
	h := Lookup("the_content")
	if h == nil {
		t.Fatal("Lookup(the_content) returned nil")
	}
	if h.Type != "filter" {
		t.Errorf("Type = %q, want filter", h.Type)
	}
}

func TestLookup_Unknown(t *testing.T) {
	h := Lookup("nonexistent_hook_abc")
	if h != nil {
		t.Errorf("expected nil for unknown hook, got %+v", h)
	}
}

func TestCount(t *testing.T) {
	c := Count()
	// wp-hooks has ~2500+ core hooks
	if c < 2000 {
		t.Errorf("Count() = %d, want at least 2000", c)
	}
}
