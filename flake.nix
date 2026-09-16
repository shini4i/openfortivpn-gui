{
  description = "Development environment for openfortivpn-gui";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
            go_1_27
            go-task
            # cgo's C compiler. clang builds the gotk4/libadwaita shims roughly
            # 5x faster than gcc on a cold GOCACHE. The goreleaser artifacts use
            # it too, so this shell, CI and the released deb/rpm share one
            # compiler; the nixpkgs package builds with its own stdenv.
            clang
            goreleaser
            pkg-config
            gobject-introspection
            golangci-lint
            gosec
            govulncheck
          ];

          buildInputs = with pkgs; [
            gtk4
            libadwaita
            glib
            libsecret
            openfortivpn
          ];

          shellHook = ''
            # Must be exported here, not set as an mkShell attribute: the
            # stdenv cc-wrapper's setup hook runs later and would overwrite
            # the attribute with gcc.
            export CC=clang

            echo "openfortivpn-gui development shell"
            echo "Go version: $(go version)"
            echo ""
            echo "Available tasks (run 'task --list' for full list):"
            echo "  task build  - Build the application"
            echo "  task run    - Build and run the application"
            echo "  task test   - Run tests with race detector"
            echo "  task lint   - Run static analysis"
          '';
        };
      }
    );
}
