package routes

import "testing"

func TestPHPHookStringCallback(t *testing.T) {
	t.Parallel()

	source := []byte(`
add_action('init', 'my_init_function');
add_action('wp_enqueue_scripts', 'enqueue_my_styles');
`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}

	r := routes[0]
	if r.Method != "ACTION" {
		t.Errorf("Method = %q, want ACTION", r.Method)
	}
	if r.Path != "init" {
		t.Errorf("Path = %q, want init", r.Path)
	}
	if r.Handler != "my_init_function" {
		t.Errorf("Handler = %q, want my_init_function", r.Handler)
	}
	if r.Framework != "wordpress" {
		t.Errorf("Framework = %q, want wordpress", r.Framework)
	}
	if r.Side != "server" {
		t.Errorf("Side = %q, want server", r.Side)
	}
}

func TestPHPHookFilterCallback(t *testing.T) {
	t.Parallel()

	source := []byte(`add_filter('the_content', 'modify_content');`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	r := routes[0]
	if r.Method != "FILTER" {
		t.Errorf("Method = %q, want FILTER", r.Method)
	}
	if r.Path != "the_content" {
		t.Errorf("Path = %q, want the_content", r.Path)
	}
	if r.Handler != "modify_content" {
		t.Errorf("Handler = %q, want modify_content", r.Handler)
	}
	if r.Side != "server" {
		t.Errorf("Side = %q, want server", r.Side)
	}
}

func TestPHPHookInstanceCallback(t *testing.T) {
	t.Parallel()

	source := []byte(`add_action('admin_menu', [$this, 'register_menu']);`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	r := routes[0]
	if r.Method != "ACTION" {
		t.Errorf("Method = %q, want ACTION", r.Method)
	}
	if r.Path != "admin_menu" {
		t.Errorf("Path = %q, want admin_menu", r.Path)
	}
	if r.Handler != "register_menu" {
		t.Errorf("Handler = %q, want register_menu", r.Handler)
	}
}

func TestPHPHookStaticCallback(t *testing.T) {
	t.Parallel()

	source := []byte(`add_action('init', [MyPlugin::class, 'bootstrap']);`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	r := routes[0]
	if r.Handler != "bootstrap" {
		t.Errorf("Handler = %q, want bootstrap", r.Handler)
	}
}

func TestPHPHookDoAction(t *testing.T) {
	t.Parallel()

	source := []byte(`do_action('my_custom_hook', $arg1, $arg2);`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	r := routes[0]
	if r.Method != "ACTION" {
		t.Errorf("Method = %q, want ACTION", r.Method)
	}
	if r.Path != "my_custom_hook" {
		t.Errorf("Path = %q, want my_custom_hook", r.Path)
	}
	if r.Handler != "" {
		t.Errorf("Handler = %q, want empty (client-side)", r.Handler)
	}
	if r.Side != "client" {
		t.Errorf("Side = %q, want client", r.Side)
	}
}

func TestPHPHookApplyFilters(t *testing.T) {
	t.Parallel()

	source := []byte(`$result = apply_filters('my_filter', $value);`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	r := routes[0]
	if r.Method != "FILTER" {
		t.Errorf("Method = %q, want FILTER", r.Method)
	}
	if r.Side != "client" {
		t.Errorf("Side = %q, want client", r.Side)
	}
}

func TestPHPHookMixedFile(t *testing.T) {
	t.Parallel()

	source := []byte(`<?php
class My_Plugin {
    public function __construct() {
        add_action('init', [$this, 'on_init']);
        add_filter('the_title', 'custom_title');
    }

    public function on_init() {
        do_action('my_plugin_loaded');
        $val = apply_filters('my_plugin_value', 42);
        echo "Hello World";
    }
}

function custom_title($title) {
    return strtoupper($title);
}
`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	// 2 server (add_action + add_filter) + 2 client (do_action + apply_filters) = 4
	if len(routes) != 4 {
		t.Fatalf("got %d routes, want 4; routes: %+v", len(routes), routes)
	}

	serverCount := 0
	clientCount := 0
	for _, r := range routes {
		switch r.Side {
		case "server":
			serverCount++
		case "client":
			clientCount++
		}
	}
	if serverCount != 2 {
		t.Errorf("server routes = %d, want 2", serverCount)
	}
	if clientCount != 2 {
		t.Errorf("client routes = %d, want 2", clientCount)
	}
}

func TestPHPHookDeduplication(t *testing.T) {
	t.Parallel()

	// Same hook registered twice should be deduped.
	source := []byte(`
add_action('init', 'my_func');
add_action('init', 'my_func');
`)

	matcher := &PHPHookMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 1 {
		t.Errorf("got %d routes, want 1 (deduped)", len(routes))
	}
}

func TestPHPHookExtractAll(t *testing.T) {
	t.Parallel()

	source := []byte(`
add_action('wp_head', 'my_head_code');
do_action('init');
`)

	routes := ExtractAll("php", source)
	if len(routes) < 2 {
		t.Errorf("ExtractAll returned %d routes, want at least 2", len(routes))
	}
}
