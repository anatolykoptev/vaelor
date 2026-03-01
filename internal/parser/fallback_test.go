package parser

import "testing"

func TestFallbackParseKotlin(t *testing.T) {
	t.Parallel()
	source := `package com.example

class UserService {
    fun getUser(id: Int): User {
        return repo.find(id)
    }

    fun deleteUser(id: Int) {
        repo.delete(id)
    }
}

fun main() {
    println("hello")
}
`
	result := fallbackParse("main.kt", []byte(source), "kotlin")
	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols from fallback parse")
	}

	hasClass := false
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Kind == KindClass {
			hasClass = true
		}
	}
	if !hasClass {
		t.Error("expected UserService class")
	}

	funcNames := make(map[string]bool)
	for _, sym := range result.Symbols {
		if sym.Kind == KindFunction || sym.Kind == KindMethod {
			funcNames[sym.Name] = true
		}
	}
	if !funcNames["main"] {
		t.Error("expected main function")
	}
}

func TestFallbackParsePHP(t *testing.T) {
	t.Parallel()
	source := `<?php
class Controller {
    function index() {
        return view('index');
    }

    function store($request) {
        return redirect('/');
    }
}

function helper() {
    return true;
}
`
	result := fallbackParse("app.php", []byte(source), "php")
	if len(result.Symbols) < 3 {
		t.Errorf("expected at least 3 symbols, got %d", len(result.Symbols))
	}
}

func TestFallbackParseEmpty(t *testing.T) {
	t.Parallel()
	result := fallbackParse("empty.txt", []byte(""), "unknown")
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols for empty file, got %d", len(result.Symbols))
	}
}
