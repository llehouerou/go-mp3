{
  description = "Pure Go MP3 decoder";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          # cgo in the example's audio backend (oto) discovers ALSA via pkg-config.
          nativeBuildInputs = with pkgs; [ pkg-config ];

          buildInputs = with pkgs; [
            # Go toolchain
            go
            gopls
            golines
            goimports-reviser
            golangci-lint
            delve

            # Nix tooling
            nil

            # Build tools
            gnumake

            # Reference decoder for compliance testing
            mpg123
            ]
            ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isLinux [
              # Audio output for ./example
              pkgs.alsa-lib
            ];

          shellHook = ''
            export GOPATH="$HOME/go"
            export PATH="$GOPATH/bin:$PATH"
          '';
        };
      }
    );
}
