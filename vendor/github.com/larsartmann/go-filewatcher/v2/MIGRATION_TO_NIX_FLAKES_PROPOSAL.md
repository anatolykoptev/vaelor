# Migration to Nix Flakes — Comprehensive Proposal

**Project:** `github.com/larsartmann/go-filewatcher`
**Date:** 2026-04-21
**Status:** ✅ COMPLETED (2026-05-23)
**Scope:** Full Nix Flakes adoption across dev environment, CI, and release tooling

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Current State Assessment](#2-current-state-assessment)
3. [Gap Analysis](#3-gap-analysis)
4. [Target Architecture](#4-target-architecture)
5. [Migration Plan](#5-migration-plan)
6. [CI/CD Migration](#6-cicd-migration)
7. [Developer Experience](#7-developer-experience)
8. [Release & Packaging](#8-release--packaging)
9. [Risk Assessment](#9-risk-assessment)
10. [Actionable Steps](#10-actionable-steps)

---

## 1. Executive Summary

The project has an **initial Nix Flake setup** (`flake.nix`, `.envrc`, `flake.lock`) created on 2026-04-12, but it covers only the development shell — approximately **20% of full Nix Flakes potential**. The previous `justfile` was removed, and commands were documented in `shellHook` but not made executable through Nix.

This proposal outlines a **complete migration to Nix Flakes** that would provide:

- **Reproducible builds** via `nix build` (currently impossible)
- **Runnable commands** via `nix run` (currently impossible)
- **Automated quality gates** via `nix flake check` (currently impossible)
- **Nix-based CI** replacing `setup-go` (currently manual)
- **Proper Go version pinning** (currently mismatched: `go_1_24` vs Go 1.26.1)
- **Binary caching** via Cachix for faster CI and onboarding
- **Nix formatter** for `flake.nix` itself

**Recommendation:** Execute in 3 phases over ~4-6 hours of focused work.

---

## 2. Current State Assessment

### 2.1 What Exists

| File         | Status  | Quality                                   |
| ------------ | ------- | ----------------------------------------- |
| `flake.nix`  | Exists  | Basic — devShell only, wrong Go version   |
| `flake.lock` | Exists  | Locked to nixpkgs `13043924` (April 2025) |
| `.envrc`     | Exists  | Minimal — `use flake`                     |
| `.gitignore` | Updated | Includes `.direnv/`, `.envrc.local`       |

### 2.2 Current `flake.nix` Analysis

```
Strengths:
  ✅ Multi-platform support (4 systems)
  ✅ forEachSystem helper (correct pattern)
  ✅ GOWORK=off environment variable
  ✅ Helpful shellHook with command listing
  ✅ direnv integration via .envrc

Weaknesses:
  ❌ Uses go_1_24 — project requires Go 1.26.1
  ❌ No packages output (cannot nix build)
  ❌ No apps output (cannot nix run)
  ❌ No checks output (cannot nix flake check)
  ❌ No formatter output
  ❌ No overlay for other flakes to consume
  ❌ Commands are documentation-only (not executable)
  ❌ Missing development tools: gopls, delve, gotools, goreleaser
  ❌ Uses deprecated `nixpkgs` registry URL (should pin explicit ref)
  ❌ No nixpkgs input follows pattern
```

### 2.3 Current CI (`ci.yml`)

The CI uses `actions/setup-go@v5` and `golangci/golangci-lint-action@v7` — **completely independent of Nix**. This means:

- CI environment ≠ local dev environment (different Go versions possible)
- No benefit from Nix reproducibility in CI
- Two sources of truth for tool versions

### 2.4 What Was Removed

The `justfile` (removed 2026-04-12) provided these commands:

| Command         | Equivalent Today                                       | Nix-native Target                      |
| --------------- | ------------------------------------------------------ | -------------------------------------- |
| `just check`    | Manual: `go vet && golangci-lint run && go test -race` | `nix flake check` or `nix run .#check` |
| `just ci`       | Manual: `go mod tidy && go fmt && go vet && ...`       | `nix run .#ci`                         |
| `just lint-fix` | Manual: `golangci-lint run --fix`                      | `nix run .#lint-fix`                   |

The removal of `justfile` without Nix-native replacements **regressed developer experience**. Commands must be typed manually or memorized from shellHook output.

---

## 3. Gap Analysis

### 3.1 Critical Gaps

| #   | Gap                                           | Impact                                          | Severity     |
| --- | --------------------------------------------- | ----------------------------------------------- | ------------ |
| G1  | **Go version mismatch** (`go_1_24` vs 1.26.1) | Build failures, incorrect behavior              | **CRITICAL** |
| G2  | **No `packages` output**                      | Cannot `nix build`, no reproducible packaging   | **HIGH**     |
| G3  | **No `apps` output**                          | Cannot `nix run .#test`, `nix run .#lint`, etc. | **HIGH**     |
| G4  | **No `checks` output**                        | Cannot `nix flake check` for automated QA       | **HIGH**     |
| G5  | **CI not using Nix**                          | CI ≠ dev environment                            | **MEDIUM**   |

### 3.2 Quality Gaps

| #   | Gap                      | Impact                                   | Severity   |
| --- | ------------------------ | ---------------------------------------- | ---------- |
| G6  | **No formatter output**  | `flake.nix` not auto-formatted           | **LOW**    |
| G7  | **Missing dev tools**    | gopls, delve, gotools not in shell       | **MEDIUM** |
| G8  | **No Cachix**            | Slow CI, slow onboarding                 | **MEDIUM** |
| G9  | **Unpinned nixpkgs URL** | `nixpkgs` registry may shift             | **LOW**    |
| G10 | **No overlay**           | Other flakes cannot consume this project | **LOW**    |

### 3.3 Go Version Deep-Dive

The Go version mismatch is the single most critical issue:

```
go.mod:          go 1.26.0
.golangci.yml:   go: 1.26.1
AGENTS.md:       Go 1.26.1
README.md:       Go 1.26.1+

flake.nix:       go_1_24  ← WRONG
```

As of the `flake.lock` revision (`nixpkgs@13043924`), `go_1_26` may not yet exist in nixpkgs. Options:

1. **Update nixpkgs** to a revision containing `go_1_26` (preferred)
2. **Use `pkgs.go`** (latest available Go in nixpkgs)
3. **Overlay a custom Go build** (complex, not recommended for a library)

---

## 4. Target Architecture

### 4.1 Proposed `flake.nix` Structure

The target flake should expose all standard Nix outputs:

```nix
{
  description = "go-filewatcher — High-performance, composable file system watcher for Go";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forEachSystem = f: nixpkgs.lib.genAttrs supportedSystems (system: f {
        inherit system;
        pkgs = nixpkgs.legacyPackages.${system};
      });
    in
    {
      # ─── Packages (nix build) ───
      # packages.${system}.default = the library
      # (For a Go library, this is less critical than for a binary,
      #  but it validates the build is reproducible)

      # ─── Dev Shells (nix develop) ───
      # devShells.${system}.default = full dev environment

      # ─── Apps (nix run) ───
      # apps.${system}.test     = run tests
      # apps.${system}.lint     = run linter
      # apps.${system}.lint-fix = auto-fix lint issues
      # apps.${system}.ci       = full CI pipeline
      # apps.${system}.check    = vet + lint + test
      # apps.${system}.bench    = run benchmarks
      # apps.${system}.coverage = generate coverage report

      # ─── Checks (nix flake check) ───
      # checks.${system}.build  = nix build succeeds
      # checks.${system}.test   = go test passes
      # checks.${system}.lint   = golangci-lint passes
      # checks.${system}.fmt    = go fmt is clean
      # checks.${system}.vet    = go vet is clean

      # ─── Formatter (nix fmt) ───
      # formatter.${system} = nixpkgs-fmt
    };
}
```

### 4.2 Output Categories

| Output      | Command           | Purpose                       |
| ----------- | ----------------- | ----------------------------- |
| `packages`  | `nix build .`     | Reproducible build validation |
| `devShells` | `nix develop`     | Development environment       |
| `apps`      | `nix run .#test`  | Executable commands           |
| `checks`    | `nix flake check` | Automated quality gates       |
| `formatter` | `nix fmt`         | Nix file formatting           |

---

## 5. Migration Plan

### Phase 1: Fix Critical Issues (~1 hour)

**Goal:** Make the existing flake correct and usable.

#### Step 1.1: Fix Go Version

Update `flake.nix` to use the correct Go version:

```nix
packages = with pkgs; [
  go_1_26          # Match go.mod requirement
  golangci-lint
  gofumpt
  git
];
```

If `go_1_26` is not available in the current nixpkgs, update the flake lock:

```bash
nix flake update  # Update to latest nixpkgs with go_1_26
```

**Verification:** `nix develop --command go version` should print `go1.26.x`.

#### Step 1.2: Add Missing Development Tools

```nix
packages = with pkgs; [
  go_1_26
  golangci-lint
  gofumpt
  git
  gopls           # Go language server (IDE support)
  delve           # Go debugger
  gotools         # guru, staticcheck analysis tools
  golines         # Line length formatter (used in .golangci.yml)
];
```

**Rationale:** The `.golangci.yml` enables `golines` formatter — it must be available in the dev shell.

#### Step 1.3: Pin nixpkgs URL

Change from registry reference to explicit GitHub URL:

```nix
# Before:
inputs.nixpkgs.url = "nixpkgs";

# After:
inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
```

**Rationale:** Registry references are mutable and may change between runs. Explicit URLs are reproducible.

#### Step 1.4: Verify Build

```bash
nix develop --command go build ./...
nix develop --command go test -race ./...
nix develop --command golangci-lint run ./...
```

---

### Phase 2: Add Nix Outputs (~2 hours)

**Goal:** Full Nix integration with apps, checks, and formatter.

#### Step 2.1: Add `packages` Output

For a Go library, the package output validates reproducibility:

```nix
packages = forEachSystem ({ pkgs, system }: {
  default = pkgs.buildGoModule {
    pname = "go-filewatcher";
    version = "0.1.0";

    src = self;

    # vendorHash must be updated after dependency changes:
    #   nix build .#default 2>&1 | tail -1
    #   (copy the got: sha256-... value)
    vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

    # Library has no main binary — build validates compilation only
    subPackages = [ "." ];

    # No tests run during build (use nix flake check instead)
    doCheck = false;

    meta = with pkgs.lib; {
      description = "High-performance, composable file system watcher for Go";
      homepage = "https://github.com/larsartmann/go-filewatcher";
      license = licenses.mit;
    };
  };
});
```

**Note:** Since this is a library (not a binary), `packages` primarily validates that the Go module compiles reproducibly. The `vendorHash` needs to be computed once:

```bash
# First build will fail with a hash mismatch — copy the "got:" hash
nix build .#default
# Then update vendorHash and rebuild
```

#### Step 2.2: Add `apps` Output

Replace the removed `justfile` commands with Nix apps:

```nix
apps = forEachSystem ({ pkgs, system }: {
  # Core commands
  test = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "test" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.go_1_26}/bin/go test -race -count=1 ./...
    ''}/bin/test";
  };

  test-v = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "test-v" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.go_1_26}/bin/go test -v -race -count=1 ./...
    ''}/bin/test-v";
  };

  lint = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "lint" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.golangci-lint}/bin/golangci-lint run ./...
    ''}/bin/lint";
  };

  lint-fix = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "lint-fix" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.golangci-lint}/bin/golangci-lint run --fix ./...
    ''}/bin/lint-fix";
  };

  vet = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "vet" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.go_1_26}/bin/go vet ./...
    ''}/bin/vet";
  };

  fmt = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "fmt" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.go_1_26}/bin/go fmt ./...
      ${pkgs.gofumpt}/bin/gofumpt -w .
    ''}/bin/fmt";
  };

  bench = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "bench" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.go_1_26}/bin/go test -bench=. -benchmem ./...
    ''}/bin/bench";
  };

  coverage = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "coverage" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.go_1_26}/bin/go test -coverprofile=coverage.out ./...
      ${pkgs.go_1_26}/bin/go tool cover -func=coverage.out
    ''}/bin/coverage";
  };

  # Composite commands
  check = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "check" ''
      cd "${self}"
      export GOWORK=off
      echo "Running vet..."
      ${pkgs.go_1_26}/bin/go vet ./...
      echo "Running lint..."
      ${pkgs.golangci-lint}/bin/golangci-lint run ./...
      echo "Running tests..."
      ${pkgs.go_1_26}/bin/go test -race -count=1 ./...
      echo "All checks passed."
    ''}/bin/check";
  };

  ci = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "ci" ''
      cd "${self}"
      export GOWORK=off
      echo "Running tidy..."
      ${pkgs.go_1_26}/bin/go mod tidy
      echo "Running fmt..."
      ${pkgs.go_1_26}/bin/go fmt ./...
      echo "Running vet..."
      ${pkgs.go_1_26}/bin/go vet ./...
      echo "Running lint..."
      ${pkgs.golangci-lint}/bin/golangci-lint run ./...
      echo "Running tests..."
      ${pkgs.go_1_26}/bin/go test -race -count=1 ./...
      echo "CI complete."
    ''}/bin/ci";
  };

  tidy = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "tidy" ''
      cd "${self}"
      export GOWORK=off
      ${pkgs.go_1_26}/bin/go mod tidy
    ''}/bin/tidy";
  };

  default = self.apps.${system}.check;
});
```

**Usage:**

```bash
nix run .#test       # Run tests with race detection
nix run .#lint       # Run linter
nix run .#lint-fix   # Auto-fix lint issues
nix run .#check      # Full quality gate
nix run .#ci         # Full CI pipeline
nix run .            # Default = check
```

#### Step 2.3: Add `checks` Output

Automated checks for `nix flake check`:

```nix
checks = forEachSystem ({ pkgs, system }: {
  build = self.packages.${system}.default;

  test = pkgs.runCommand "test" {
    nativeBuildInputs = with pkgs; [ go_1_26 ];
    src = self;
  } ''
    cd "$src"
    export GOWORK=off
    export HOME="$TMPDIR"
    go test -race -count=1 ./...
    touch "$out"
  '';

  lint = pkgs.runCommand "lint" {
    nativeBuildInputs = with pkgs; [ go_1_26 golangci-lint ];
    src = self;
  } ''
    cd "$src"
    export GOWORK=off
    export HOME="$TMPDIR"
    golangci-lint run ./...
    touch "$out"
  '';

  go-fmt = pkgs.runCommand "go-fmt" {
    nativeBuildInputs = with pkgs; [ go_1_26 ];
    src = self;
  } ''
    cd "$src"
    export GOWORK=off
    # Check if any files need formatting
    unformatted=$(gofmt -l .)
    if [ -n "$unformatted" ]; then
      echo "Files need formatting:"
      echo "$unformatted"
      exit 1
    fi
    touch "$out"
  '';

  vet = pkgs.runCommand "vet" {
    nativeBuildInputs = with pkgs; [ go_1_26 ];
    src = self;
  } ''
    cd "$src"
    export GOWORK=off
    go vet ./...
    touch "$out"
  '';
});
```

**Usage:**

```bash
nix flake check              # Run all checks
nix flake check .#test       # Run specific check
```

#### Step 2.4: Add `formatter` Output

```nix
formatter = forEachSystem ({ pkgs, system }: pkgs.nixpkgs-fmt);
```

**Usage:**

```bash
nix fmt   # Format all .nix files
```

---

### Phase 3: CI & Polish (~1-2 hours)

**Goal:** Nix-based CI and final polish.

#### Step 3.1: Migrate CI to Nix

Replace `setup-go` + `golangci-lint-action` with Nix:

```yaml
name: CI

on:
  push:
    branches: [master, main]
  pull_request:
    branches: [master, main]

permissions:
  contents: read

jobs:
  check:
    name: Nix Flake Check
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: cachix/install-nix-action@v30
        with:
          nix_path: nixpkgs=channel:nixpkgs-unstable

      # Optional: Add Cachix for binary caching
      # - uses: cachix/cachix-action@v14
      #   with:
      #     name: go-filewatcher
      #     authToken: '${{ secrets.CACHIX_AUTH_TOKEN }}'

      - name: Check Nix Flake
        run: nix flake check --all-systems

      - name: Run Tests
        run: nix run .#test

      - name: Run Linter
        run: nix run .#lint

      - name: Build
        run: nix build .
```

**Benefits:**

- CI uses **exact same** toolchain as local dev
- No version drift between environments
- Single source of truth: `flake.nix`
- Cachix accelerates subsequent runs

#### Step 3.2: Update Documentation

Files to update after migration:

| File        | Changes                                                |
| ----------- | ------------------------------------------------------ |
| `AGENTS.md` | Update commands section with `nix run .#<cmd>` syntax  |
| `README.md` | Add Nix installation section, update dev commands      |
| `.envrc`    | Consider adding `watch_file flake.nix` for auto-reload |

#### Step 3.3: Add Shell Aliases (Optional Enhancement)

Add convenience shell aliases in devShell for shorter commands:

```nix
shellHook = ''
  # Convenience aliases (Nix apps also available via nix run .#<name>)
  alias check='nix run .#check'
  alias ci='nix run .#ci'
  alias lint='nix run .#lint'
  alias lint-fix='nix run .#lint-fix'
  alias test='nix run .#test'

  echo "go-filewatcher development shell"
  echo "Go version: $(go version)"
  echo ""
  echo "Quick commands (also available as 'nix run .#<name>'):"
  echo "  check       - vet + lint + test"
  echo "  ci          - tidy + fmt + vet + lint + test"
  echo "  lint-fix    - Auto-fix linter issues"
  echo "  test        - Run tests with -race"
  echo "  bench       - Run benchmarks"
  echo "  coverage    - Generate coverage report"
'';
```

---

## 6. CI/CD Migration

### 6.1 Current vs Target

| Aspect          | Current                            | Target                                |
| --------------- | ---------------------------------- | ------------------------------------- |
| Go install      | `actions/setup-go@v5`              | Nix (via `install-nix-action`)        |
| Lint install    | `golangci/golangci-lint-action@v7` | Nix (via `install-nix-action`)        |
| Go version      | Matrix: `["1.26"]`                 | Single: whatever `flake.nix` provides |
| Lint version    | `latest` (pinned by action)        | Whatever `flake.nix` provides         |
| Reproducibility | Partial (action versions drift)    | Full (flake.lock pins everything)     |
| Build cache     | Go module cache only               | Cachix binary cache                   |

### 6.2 CI Strategy: Nix-First

**Recommended approach:** Replace all Go tooling actions with a single Nix install step.

```yaml
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: cachix/install-nix-action@v30
        with:
          extra_nix_config: |
            experimental-features = nix-command flakes
      - run: nix flake check --all-systems
      - run: nix run .#coverage
```

**Alternative (hybrid):** Keep `setup-go` for speed but add `nix flake check` as an additional validation:

```yaml
jobs:
  test:
    # ... existing test job unchanged ...

  nix-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: cachix/install-nix-action@v30
      - run: nix flake check
```

### 6.3 Cachix Integration (Optional)

For projects with frequent CI runs, Cachix dramatically reduces build times:

```yaml
- uses: cachix/cachix-action@v14
  with:
    name: go-filewatcher
    authToken: "${{ secrets.CACHIX_AUTH_TOKEN }}"
```

**Cost:** Free for open-source projects. Requires setup at `cachix.org`.

**Decision needed:** Whether to set up Cachix now or defer.

---

## 7. Developer Experience

### 7.1 Command Comparison

| Before (justfile) | Current (manual)          | Target (Nix)                   |
| ----------------- | ------------------------- | ------------------------------ |
| `just check`      | Type 3 commands manually  | `nix run .#check`              |
| `just ci`         | Type 6 commands manually  | `nix run .#ci`                 |
| `just lint-fix`   | Type full command         | `nix run .#lint-fix`           |
| —                 | `go test -race ./...`     | `nix run .#test`               |
| —                 | `golangci-lint run ./...` | `nix run .#lint`               |
| —                 | `go test -bench=. ./...`  | `nix run .#bench`              |
| —                 | —                         | `nix flake check` (all QA)     |
| —                 | —                         | `nix build .` (validate build) |
| —                 | —                         | `nix fmt` (format Nix files)   |

### 7.2 Onboarding Flow

**Before (current):**

```bash
# Developer must have Go 1.26.1, golangci-lint, gofumpt installed
# No way to verify correct versions
# Different developers may have different tool versions
go build ./...
```

**After (proposed):**

```bash
# One command — everything is reproducible
nix develop
# OR with direnv (automatic on cd):
direnv allow

# Then any command works with guaranteed versions:
nix run .#test
nix run .#lint
nix run .#check
```

### 7.3 `direnv` Integration

Current `.envrc` is minimal. Proposed enhancement:

```bash
# .envrc
use flake
watch_file flake.nix
watch_file flake.lock
```

The `watch_file` directives cause direnv to automatically reload when the flake changes — no manual `direnv allow` after every edit.

---

## 8. Release & Packaging

### 8.1 Library vs Binary Considerations

`go-filewatcher` is a **library**, not a binary. This affects Nix packaging:

| Aspect            | Binary                      | Library                          |
| ----------------- | --------------------------- | -------------------------------- |
| `packages` output | Essential (produces binary) | Nice-to-have (validates build)   |
| `nix run`         | Runs the binary             | N/A (no main package)            |
| Docker image      | Common use case             | Rarely needed                    |
| nixpkgs inclusion | High value                  | Low value (Go modules preferred) |

### 8.2 Recommended Approach

For a Go library, the `packages` output should:

1. **Validate compilation** — `buildGoModule` ensures all code compiles
2. **Pin dependencies** — `vendorHash` locks transitive deps
3. **Not attempt to "install"** — Go libraries are consumed via `go get`, not Nix

The primary value of `packages` for this project is **CI validation** — ensuring the build is reproducible in a pure Nix environment.

### 8.3 Goreleaser (Alternative/Future)

If the project later adds a CLI binary (mentioned in TODO_LIST.md), Goreleaser integrates with Nix:

```nix
# Future: when CLI binary exists
packages.default = pkgs.buildGoModule {
  pname = "go-filewatcher-cli";
  version = "0.1.0";
  src = self;
  vendorHash = "...";
  subPackages = [ "cmd/filewatcher" ];
  ldflags = [ "-s" "-w" "-X main.version=0.1.0" ];
};
```

---

## 9. Risk Assessment

### 9.1 Risks

| Risk                             | Likelihood | Impact | Mitigation                                     |
| -------------------------------- | ---------- | ------ | ---------------------------------------------- |
| `go_1_26` not in nixpkgs         | Medium     | High   | Update flake.lock; use `pkgs.go` as fallback   |
| Nix evaluation slow on CI        | Medium     | Medium | Cachix binary cache                            |
| Team unfamiliar with Nix         | Low        | Low    | Document commands; shell aliases               |
| `vendorHash` maintenance burden  | Medium     | Low    | Automate via CI; document update procedure     |
| Breaking change for contributors | Low        | Medium | Keep `direnv allow` as primary onboarding path |
| `golangci-lint` version mismatch | Low        | Medium | Nix pins exact version; more reproducible      |

### 9.2 Rollback Plan

If Nix proves problematic:

1. Keep `.github/workflows/ci.yml` working independently (don't delete Go commands)
2. The `flake.nix` is additive — removing it doesn't break anything
3. No data loss risk — Nix only affects dev environment and CI

---

## 10. Actionable Steps

### Summary of All Steps (Ordered by Priority)

| #   | Step                                              | Phase | Est. Time | Dependency |
| --- | ------------------------------------------------- | ----- | --------- | ---------- |
| 1   | Update `flake.lock` to get `go_1_26`              | 1     | 5 min     | —          |
| 2   | Fix `go_1_24` → `go_1_26` in `flake.nix`          | 1     | 5 min     | Step 1     |
| 3   | Pin `nixpkgs.url` to explicit GitHub ref          | 1     | 2 min     | —          |
| 4   | Add missing dev tools (gopls, delve, golines)     | 1     | 5 min     | —          |
| 5   | Verify `nix develop` works with correct Go        | 1     | 5 min     | Steps 1-4  |
| 6   | Add `packages` output with `buildGoModule`        | 2     | 30 min    | Step 5     |
| 7   | Compute and set `vendorHash`                      | 2     | 10 min    | Step 6     |
| 8   | Add `apps` output (test, lint, check, ci, etc.)   | 2     | 45 min    | Step 5     |
| 9   | Add `checks` output (build, test, lint, fmt, vet) | 2     | 30 min    | Steps 6, 8 |
| 10  | Add `formatter` output                            | 2     | 5 min     | —          |
| 11  | Verify `nix flake check` passes                   | 2     | 10 min    | Steps 6-10 |
| 12  | Update `.envrc` with `watch_file` directives      | 3     | 2 min     | —          |
| 13  | Add shell aliases in `shellHook`                  | 3     | 5 min     | Step 8     |
| 14  | Update `AGENTS.md` with new commands              | 3     | 10 min    | Step 8     |
| 15  | Update `README.md` with Nix instructions          | 3     | 15 min    | Step 8     |
| 16  | Migrate `ci.yml` to Nix                           | 3     | 30 min    | Step 11    |
| 17  | Set up Cachix (optional)                          | 3     | 30 min    | Step 16    |
| 18  | Final verification: all commands work             | 3     | 15 min    | Steps 1-17 |

**Total estimated time:** ~4-5 hours

### Execution Order

```
Phase 1 (Fix Critical) ──── 20 min ──── Steps 1-5
Phase 2 (Add Outputs) ───── 2 hours ─── Steps 6-11
Phase 3 (CI & Polish) ───── 1.5 hours ─ Steps 12-18
```

### Verification Checklist

After completing all phases, verify:

- [x] `nix develop` enters shell with Go 1.26.x
- [x] `nix build .` succeeds (validates buildGoModule)
- [x] `nix run .#test` passes all tests with `-race`
- [x] `nix run .#lint` passes with 0 issues
- [x] `nix run .#check` runs vet + lint + test
- [x] `nix run .#ci` runs full CI pipeline
- [x] `nix run .#lint-fix` auto-fixes lint issues
- [x] `nix run .#bench` runs benchmarks
- [x] `nix run .#coverage` generates coverage report
- [x] `nix flake check` runs all checks
- [x] `nix fmt` formats `.nix` files
- [x] `direnv allow` works with `.envrc`
- [ ] CI pipeline passes with Nix (DEFERRED - using GitHub Actions Go setup instead per decision D4)
- [x] `AGENTS.md` reflects new commands
- [x] `README.md` has Nix installation instructions

**Note:** CI migration to Nix was deferred per proposal decision D4 ("Later — add when CI is stable"). The existing GitHub Actions CI using `setup-go` continues to work and provides equivalent quality gates.

---

## Appendix A: Complete Target `flake.nix`

```nix
{
  description = "go-filewatcher — High-performance, composable file system watcher for Go";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];

      forEachSystem = f: nixpkgs.lib.genAttrs supportedSystems (system: f {
        inherit system;
        pkgs = nixpkgs.legacyPackages.${system};
      });
    in
    {
      packages = forEachSystem ({ pkgs, system }: {
        default = pkgs.buildGoModule {
          pname = "go-filewatcher";
          version = "0.1.0";
          src = self;
          vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
          doCheck = false;
          meta = with pkgs.lib; {
            description = "High-performance, composable file system watcher for Go";
            homepage = "https://github.com/larsartmann/go-filewatcher";
            license = licenses.mit;
          };
        };
      });

      devShells = forEachSystem ({ pkgs, system }: {
        default = pkgs.mkShell {
          name = "go-filewatcher";

          packages = with pkgs; [
            go_1_26
            golangci-lint
            gofumpt
            golines
            gopls
            delve
            gotools
            git
          ];

          GOWORK = "off";

          shellHook = ''
            alias check='nix run .#check'
            alias ci='nix run .#ci'
            alias lint='nix run .#lint'
            alias lint-fix='nix run .#lint-fix'
            alias test='nix run .#test'

            echo "go-filewatcher development shell"
            echo "Go version: $(go version)"
            echo "golangci-lint version: $(golangci-lint --version)"
            echo ""
            echo "Commands (nix run .#<name> or alias):"
            echo "  check       - vet + lint + test"
            echo "  ci          - tidy + fmt + vet + lint + test"
            echo "  lint-fix    - Auto-fix linter issues"
            echo "  test        - Run tests with -race"
            echo "  bench       - Run benchmarks"
            echo "  coverage    - Generate coverage report"
          '';
        };
      });

      apps = forEachSystem ({ pkgs, system }: {
        test = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "test" ''
            cd "${self}"
            export GOWORK=off
            ${pkgs.go_1_26}/bin/go test -race -count=1 ./...
          ''}/bin/test";
        };

        lint = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "lint" ''
            cd "${self}"
            export GOWORK=off
            ${pkgs.golangci-lint}/bin/golangci-lint run ./...
          ''}/bin/lint";
        };

        lint-fix = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "lint-fix" ''
            cd "${self}"
            export GOWORK=off
            ${pkgs.golangci-lint}/bin/golangci-lint run --fix ./...
          ''}/bin/lint-fix";
        };

        check = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "check" ''
            cd "${self}"
            export GOWORK=off
            echo "Running vet..."
            ${pkgs.go_1_26}/bin/go vet ./...
            echo "Running lint..."
            ${pkgs.golangci-lint}/bin/golangci-lint run ./...
            echo "Running tests..."
            ${pkgs.go_1_26}/bin/go test -race -count=1 ./...
            echo "All checks passed."
          ''}/bin/check";
        };

        ci = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "ci" ''
            cd "${self}"
            export GOWORK=off
            echo "Running tidy..."
            ${pkgs.go_1_26}/bin/go mod tidy
            echo "Running fmt..."
            ${pkgs.go_1_26}/bin/go fmt ./...
            echo "Running vet..."
            ${pkgs.go_1_26}/bin/go vet ./...
            echo "Running lint..."
            ${pkgs.golangci-lint}/bin/golangci-lint run ./...
            echo "Running tests..."
            ${pkgs.go_1_26}/bin/go test -race -count=1 ./...
            echo "CI complete."
          ''}/bin/ci";
        };

        bench = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "bench" ''
            cd "${self}"
            export GOWORK=off
            ${pkgs.go_1_26}/bin/go test -bench=. -benchmem ./...
          ''}/bin/bench";
        };

        coverage = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "coverage" ''
            cd "${self}"
            export GOWORK=off
            ${pkgs.go_1_26}/bin/go test -coverprofile=coverage.out ./...
            ${pkgs.go_1_26}/bin/go tool cover -func=coverage.out
          ''}/bin/coverage";
        };

        fmt = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "fmt" ''
            cd "${self}"
            export GOWORK=off
            ${pkgs.go_1_26}/bin/go fmt ./...
            ${pkgs.gofumpt}/bin/gofumpt -w .
          ''}/bin/fmt";
        };

        tidy = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "tidy" ''
            cd "${self}"
            export GOWORK=off
            ${pkgs.go_1_26}/bin/go mod tidy
          ''}/bin/tidy";
        };

        default = self.apps.${system}.check;
      });

      checks = forEachSystem ({ pkgs, system }: {
        build = self.packages.${system}.default;

        test = pkgs.runCommand "test" {
          nativeBuildInputs = with pkgs; [ go_1_26 ];
        } ''
          cd "${self}"
          export GOWORK=off
          export HOME="$TMPDIR"
          go test -race -count=1 ./...
          touch "$out"
        '';

        lint = pkgs.runCommand "lint" {
          nativeBuildInputs = with pkgs; [ go_1_26 golangci-lint ];
        } ''
          cd "${self}"
          export GOWORK=off
          export HOME="$TMPDIR"
          golangci-lint run ./...
          touch "$out"
        '';

        go-fmt = pkgs.runCommand "go-fmt" {
          nativeBuildInputs = with pkgs; [ go_1_26 gofumpt ];
        } ''
          cd "${self}"
          export GOWORK=off
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "Files need formatting:"
            echo "$unformatted"
            exit 1
          fi
          touch "$out"
        '';

        vet = pkgs.runCommand "vet" {
          nativeBuildInputs = with pkgs; [ go_1_26 ];
        } ''
          cd "${self}"
          export GOWORK=off
          go vet ./...
          touch "$out"
        '';
      });

      formatter = forEachSystem ({ pkgs, system }: pkgs.nixpkgs-fmt);
    };
}
```

## Appendix B: Target `.envrc`

```bash
use flake
watch_file flake.nix
watch_file flake.lock
```

## Appendix C: Target `ci.yml`

```yaml
name: CI

on:
  push:
    branches: [master, main]
  pull_request:
    branches: [master, main]

permissions:
  contents: read

jobs:
  nix-check:
    name: Nix Flake Check
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: cachix/install-nix-action@v30
        with:
          extra_nix_config: |
            experimental-features = nix-command flakes

      - name: Validate flake
        run: nix flake check --all-systems

      - name: Run tests
        run: nix run .#test

      - name: Generate coverage
        run: nix run .#coverage

  nix-build:
    name: Nix Build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: cachix/install-nix-action@v30
        with:
          extra_nix_config: |
            experimental-features = nix-command flakes

      - name: Build
        run: nix build .
```

## Appendix D: Target `AGENTS.md` Commands Section

````markdown
## Critical Commands

```bash
# Using Nix flake (recommended)
nix develop              # Enter development shell with Go and tools
direnv allow             # Auto-load environment on cd (requires direnv)

# Nix apps (run from anywhere, no need to be in dev shell)
nix run .#check          # Full quality: vet + lint + test
nix run .#ci             # Full CI: tidy + fmt + vet + lint + test
nix run .#lint-fix       # Auto-fix linter issues
nix run .#test           # Run tests with -race
nix run .#lint           # Run linter
nix run .#bench          # Run benchmarks
nix run .#coverage       # Generate coverage report
nix run .#fmt            # Format Go code
nix run .#tidy           # Run go mod tidy

# Nix quality gates
nix flake check          # Run all checks (build, test, lint, fmt, vet)
nix fmt                  # Format .nix files

# Direct Go commands (inside dev shell, GOWORK=off is automatic)
go test -race ./...
golangci-lint run ./...
```
````

---

## Decision Points

The following decisions are needed before execution:

| #   | Decision                              | Options                           | Recommendation                                     |
| --- | ------------------------------------- | --------------------------------- | -------------------------------------------------- |
| D1  | Go version if `go_1_26` unavailable   | `pkgs.go` / custom overlay / wait | Update flake.lock first; use `pkgs.go` as fallback |
| D2  | Include `packages` output for library | Yes / No                          | Yes — validates build, minimal maintenance         |
| D3  | CI: full Nix or hybrid                | Full Nix / Hybrid                 | Full Nix — eliminates version drift                |
| D4  | Set up Cachix now or later            | Now / Later                       | Later — add when CI is stable                      |
| D5  | Include `buildGoModule` or skip       | Include / Skip                    | Include — catches dependency issues early          |

---

_Proposal prepared for `go-filewatcher` by Crush — Arete in Engineering_
_Date: 2026-04-21_
