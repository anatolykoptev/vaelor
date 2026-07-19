{
  description = "go-filewatcher - A Go file watching library with debouncing and middleware support";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      forEachSystem =
        f:
        nixpkgs.lib.genAttrs supportedSystems (
          system:
          f {
            inherit system;
            pkgs = nixpkgs.legacyPackages.${system};
          }
        );

      version = self.rev or self.dirtyRev or "dev";
      vendorHash = "sha256-nwcNVqwU1gWXaKWwzQdz0LutX9eDhSJgCNFdTlhccWs=";

      src = nixpkgs.lib.fileset.toSource {
        root = ./.;
        fileset = nixpkgs.lib.fileset.unions [
          ./go.mod
          ./go.sum
          ./doc.go
          ./debouncer.go
          ./debouncer_test.go
          ./errors.go
          ./errors_test.go
          ./event.go
          ./event_test.go
          ./example_test.go
          ./filter.go
          ./filter_gogen.go
          ./filter_gogen_test.go
          ./filter_test.go
          ./fuzz_test.go
          ./metrics.go
          ./metrics_test.go
          ./middleware.go
          ./middleware_test.go
          ./options.go
          ./options_test.go
          ./otel.go
          ./otel_test.go
          ./phantom_types.go
          ./phantom_types_test.go
          ./testing_helpers_test.go
          ./watcher.go
          ./watcher_coverage_test.go
          ./watcher_gitignore.go
          ./watcher_gitignore_test.go
          ./watcher_internal.go
          ./watcher_internal_test.go
          ./watcher_poll.go
          ./watcher_reset_test.go
          ./watcher_selfheal.go
          ./watcher_selfheal_test.go
          ./watcher_test.go
          ./watcher_walk.go
          ./watcher_walk_test.go
          ./benchmark_test.go
        ];
      };
    in
    {
      overlays.default = final: _prev: {
        go-filewatcher = final.callPackage ./package.nix { };
      };

      packages = forEachSystem (
        { pkgs, ... }:
        {
          default = pkgs.buildGoModule {
            pname = "go-filewatcher";
            inherit src version vendorHash;
            doCheck = false;
            meta = {
              description = "High-performance, composable file system watcher for Go";
              homepage = "https://github.com/larsartmann/go-filewatcher";
              license = pkgs.lib.licenses.mit;
              maintainers = [ ];
            };
          };
        }
      );

      devShells = forEachSystem (
        { pkgs, ... }:
        {
          default = pkgs.mkShell {
            name = "go-filewatcher";

            packages = [
              pkgs.go_1_26
              pkgs.golangci-lint
              pkgs.gofumpt
              pkgs.golines
              pkgs.gopls
              pkgs.delve
              pkgs.gotools
              pkgs.git
            ];

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
              echo "  test-v      - Run tests with -race -v"
              echo "  lint        - Run linter"
              echo "  bench       - Run benchmarks"
              echo "  coverage    - Generate coverage report"
              echo "  fmt         - Format Go code"
              echo "  tidy        - Run go mod tidy"
            '';

            GOWORK = "off";
          };
        }
      );

      apps = forEachSystem (
        { pkgs, system }:
        let
          mkApp = name: text: {
            type = "app";
            program = "${
              pkgs.writeShellApplication {
                inherit name text;
                runtimeInputs = with pkgs; [
                  go_1_26
                  golangci-lint
                  gofumpt
                ];
              }
            }/bin/${name}";
          };
        in
        {
          test = mkApp "test" ''
            cd "${self}"
            go test -race -count=1 ./...
          '';

          test-v = mkApp "test-v" ''
            cd "${self}"
            go test -v -race -count=1 ./...
          '';

          lint = mkApp "lint" ''
            cd "${self}"
            golangci-lint run ./...
          '';

          lint-fix = mkApp "lint-fix" ''
            cd "${self}"
            golangci-lint run --fix ./...
          '';

          vet = mkApp "vet" ''
            cd "${self}"
            go vet ./...
          '';

          fmt = mkApp "fmt" ''
            cd "${self}"
            go fmt ./...
            gofumpt -w .
          '';

          bench = mkApp "bench" ''
            cd "${self}"
            go test -bench=. -benchmem -race ./...
          '';

          coverage = mkApp "coverage" ''
            cd "${self}"
            COVERAGE_OUT="''${TMPDIR:-/tmp}/coverage.out"
            go test -coverprofile="$COVERAGE_OUT" ./...
            go tool cover -func="$COVERAGE_OUT"
          '';

          tidy = mkApp "tidy" ''
            cd "${self}"
            go mod tidy
          '';

          check = mkApp "check" ''
            cd "${self}"
            echo "Running vet..."
            go vet ./...
            echo "Running lint..."
            golangci-lint run ./...
            echo "Running tests..."
            go test -race -count=1 ./...
            echo "All checks passed."
          '';

          ci = mkApp "ci" ''
            cd "${self}"
            echo "Running tidy..."
            go mod tidy
            echo "Running fmt..."
            go fmt ./...
            echo "Running vet..."
            go vet ./...
            echo "Running lint..."
            golangci-lint run ./...
            echo "Running tests..."
            go test -race -count=1 ./...
            echo "CI complete."
          '';

          default = self.apps.${system}.check;
        }
      );

      checks = forEachSystem (
        { pkgs, system }:
        let
          goModules = self.packages.${system}.default.goModules;
        in
        {
          build = self.packages.${system}.default;

          test =
            pkgs.runCommand "test"
              {
                nativeBuildInputs = [
                  pkgs.go_1_26
                  pkgs.gcc
                ];
              }
              ''
                export GOWORK=off
                export HOME="$TMPDIR"
                cp -r "${self}" src && chmod -R u+w src && cd src
                ln -s "${goModules}" vendor
                go test -race -count=1 ./...
                touch "$out"
              '';

          lint =
            pkgs.runCommand "lint"
              {
                nativeBuildInputs = [
                  pkgs.go_1_26
                  pkgs.golangci-lint
                ];
              }
              ''
                export GOWORK=off
                export HOME="$TMPDIR"
                cp -r "${self}" src && chmod -R u+w src && cd src
                ln -s "${goModules}" vendor
                golangci-lint run ./...
                touch "$out"
              '';

          vet =
            pkgs.runCommand "vet"
              {
                nativeBuildInputs = [
                  pkgs.go_1_26
                  pkgs.gcc
                ];
              }
              ''
                export GOWORK=off
                export HOME="$TMPDIR"
                cp -r "${self}" src && chmod -R u+w src && cd src
                ln -s "${goModules}" vendor
                go vet ./...
                touch "$out"
              '';

          go-fmt =
            pkgs.runCommand "go-fmt"
              {
                nativeBuildInputs = [
                  pkgs.go_1_26
                  pkgs.gofumpt
                ];
              }
              ''
                export GOWORK=off
                export HOME="$TMPDIR"
                cp -r "${self}" src && chmod -R u+w src && cd src
                unformatted=$(gofmt -l .)
                if [ -n "$unformatted" ]; then
                  echo "Files need formatting:"
                  echo "$unformatted"
                  exit 1
                fi
                touch "$out"
              '';
        }
      );

      formatter = forEachSystem ({ pkgs, ... }: pkgs.nixfmt);
    };
}
