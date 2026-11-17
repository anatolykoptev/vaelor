{
  description = "gogenfilter — Go source filtering framework";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    systems.url = "github:nix-systems/default";
    md-go-validator = {
      url = "github:LarsArtmann/md-go-validator";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{
      self,
      flake-parts,
      treefmt-nix,
      systems,
      md-go-validator,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = import systems;

      imports = [
        treefmt-nix.flakeModule
      ];

      perSystem =
        {
          config,
          pkgs,
          system,
          ...
        }:
        let
          inherit (pkgs) lib;
          goPkg = pkgs.go_1_26;

          goFiles = lib.fileset.fileFilter (file: file.hasExt "go") ./.;
          src = lib.fileset.toSource {
            root = ./.;
            fileset = lib.fileset.unions [
              ./go.mod
              ./go.sum
              ./README.md
              ./testhelpers
              ./testdata
              goFiles
            ];
          };

          mkApp =
            name: description: runtimeInputs: text:
            let
              script = pkgs.writeShellApplication {
                inherit name runtimeInputs text;
                meta.description = description;
              };
            in
            {
              type = "app";
              program = lib.getExe script;
              meta.description = description;
            };

          pkg = pkgs.buildGoModule {
            pname = "gogenfilter";
            version = self.rev or self.dirtyRev or "dev";
            inherit src;
            go = goPkg;
            vendorHash = "sha256-x4cy+Lyr68u7CrLuUq/fHP/qOmj8j43BD/drOkY8g50=";
            proxyVendor = true;
            meta = with pkgs.lib; {
              description = "Go library for detecting and filtering auto-generated code files";
              license = licenses.mit;
              homepage = "https://gogenfilter.lars.software";
              platforms = platforms.unix ++ platforms.windows;
              maintainers = [
                {
                  name = "Lars Artmann";
                  github = "LarsArtmann";
                }
              ];
            };
          };

          mdgo = md-go-validator.packages.${system}.default.overrideAttrs (_: {
            vendorHash = "sha256-oNZTI5SywT9C4guLdULUwvSlJ9KhNHurg7fqhyxDB7k=";
          });
        in
        {
          treefmt = {
            projectRootFile = "go.mod";
            programs = {
              gofumpt.enable = true;
              goimports.enable = true;
              golines.enable = true;
              nixfmt.enable = true;
            };
          };

          checks.format = config.treefmt.build.check self;
          devShells.default = pkgs.mkShell {
            packages = [
              goPkg
              pkgs.golangci-lint
              pkgs.gofumpt
              pkgs.golines
              pkgs.gopls
              pkgs.gotools
              pkgs.govulncheck
              pkgs.trash-cli
              mdgo
            ];

            GOWORK = "off";

            shellHook = ''
              echo "gogenfilter dev shell — $(go version)"
            '';
          };

          devShells.ci = pkgs.mkShellNoCC {
            packages = [
              goPkg
              pkgs.golangci-lint
            ];

            GOWORK = "off";
          };

          checks = {
            build = pkg;
            test = pkg.overrideAttrs (_: {
              doCheck = true;
            });
          };

          apps = {
            test = mkApp "test" "Run the Go test suite" [ goPkg ] ''
              go test ./... -count=1 "$@"
            '';

            test-race = mkApp "test-race" "Run tests with the race detector" [ goPkg ] ''
              go test ./... -race -count=1 "$@"
            '';

            build = mkApp "build" "Compile all Go packages" [ goPkg ] ''
              go build ./...
            '';

            vet = mkApp "vet" "Run go vet on all packages" [ goPkg ] ''
              go vet ./...
            '';

            lint = mkApp "lint" "Run golangci-lint" [ pkgs.golangci-lint ] ''
              golangci-lint run ./...
            '';

            gendocs = mkApp "gendocs" "Generate documentation from the detectors table" [ goPkg ] ''
              go run ./cmd/gendocs "$@"
            '';

            coverage = mkApp "coverage" "Generate test coverage report" [ goPkg ] ''
              go test ./... -coverprofile=coverage.out -covermode=atomic "$@"
              go tool cover -func=coverage.out
            '';

            vulncheck = mkApp "vulncheck" "Scan for Go vulnerabilities with govulncheck" [ pkgs.govulncheck ] ''
              govulncheck ./...
            '';

            clean =
              mkApp "clean" "Remove coverage artifacts and clear test cache"
                [
                  goPkg
                  pkgs.trash-cli
                ]
                ''
                  trash-put coverage.out 2>/dev/null || true
                  go clean -testcache
                '';

            validate-docs =
              mkApp "validate-docs" "Validate website docs structure with md-go-validator" [ mdgo ]
                ''
                  md-go-validator -f table website/src/content/docs/
                '';
          };
        };

      flake.overlays.default = final: _prev: {
        gogenfilter = self.packages.${final.stdenv.system}.default;
      };

    };
}
