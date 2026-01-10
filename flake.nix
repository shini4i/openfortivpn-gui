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
            go
            go-task
            pkg-config
            gobject-introspection
            golangci-lint
            gosec
          ];

          buildInputs = with pkgs; [
            gtk4
            libadwaita
            glib
            libsecret
            openfortivpn
          ];

          shellHook = ''
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
