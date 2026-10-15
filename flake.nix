{
  description = "QuickNotes - reproducible build with Nix (Lab 11)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        quicknotes = pkgs.buildGoModule {
          pname = "quicknotes";
          version = "0.1.0";
          src = ./app;
          # QuickNotes has zero third-party deps (no go.sum) -> nothing to vendor
          vendorHash = null;
          subPackages = [ "." ];
          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" ];
          meta.mainProgram = "quicknotes";
        };

        docker = pkgs.dockerTools.buildImage {
          name = "quicknotes";
          tag = "nix";
          copyToRoot = pkgs.buildEnv {
            name = "quicknotes-root";
            paths = [ quicknotes ];
            pathsToLink = [ "/bin" ];
          };
          extraCommands = ''
            mkdir -p data tmp
            chmod 1777 tmp
            cp ${./app/seed.json} data/seed.json
          '';
          config = {
            Entrypoint = [ "/bin/quicknotes" ];
            ExposedPorts = { "8080/tcp" = { }; };
            User = "65532:65532";
            Env = [
              "ADDR=:8080"
              "DATA_PATH=/tmp/notes.json"
              "SEED_PATH=/data/seed.json"
            ];
          };
        };

        default = quicknotes;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.gopls pkgs.golangci-lint ];
        };
      });
    };
}
