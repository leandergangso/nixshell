{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems =
        f:
        nixpkgs.lib.genAttrs systems (
          system:
          let
            pkgs = nixpkgs.legacyPackages.${system};
          in
          f pkgs
        );
    in
    {
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.go-task
          ];

          shellHook = ''
            echo nix shell
          '';
        };

        ci = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.fish
            pkgs.zsh
          ];
        };
      });
    };
}
