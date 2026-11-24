{ pkgs, version ? "0.1.0" }:

pkgs.buildGoModule {
  pname = "nixshell";
  inherit version;
  src = ./.;
  # No third-party Go modules are used.
  vendorHash = null;

  meta = {
    description = "Enter a Nix development shell using your own shell";
    mainProgram = "nixshell";
    license = pkgs.lib.licenses.mit;
    platforms = pkgs.lib.platforms.linux;
  };
}
