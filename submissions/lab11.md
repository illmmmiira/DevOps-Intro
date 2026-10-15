# Lab 11 — Reproducible Builds of QuickNotes with Nix

**Setup:** macOS (Apple Silicon). Nix runs inside `nixos/nix` containers (linux/arm64), not on the host, so the build targets Linux and both "environments" are clean and isolated.

| Environment | How | What it shares with the other |
|---|---|---|
| **A** | `docker run -it --name nixA -v "$PWD:/repo" nixos/nix` — local working tree mounted | nothing but the source |
| **B** | `docker run --rm -it nixos/nix` — empty Nix store, fresh `git clone` of commit `265e09c` from GitHub | nothing but the source |
| **CI** | two parallel GitHub runners (x86_64), see Bonus | nothing but the source |

Pinned nixpkgs (`flake.lock`): `nixos-25.11` @ `b6018f87da91d19d0ab4cf979885689b469cdd41` (`narHash sha256-twXPFqFsrrY5r28Zh7Homgcp2gUMBgQ6WDS98Q/3xFI=`).

---

## Task 1 — Reproducible Go build via Nix flake

### `flake.nix`

```nix
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
```

Choices:
- **`buildGoModule`** from nixpkgs — standard, no extra flake input (see d).
- **`vendorHash = null`** — QuickNotes uses only the Go standard library (no `go.sum`, no `require`), so there is nothing to vendor. The value is still pinned: `null` explicitly means "no module dependencies" (see b).
- `env.CGO_ENABLED = 0`, `ldflags = [ "-s" "-w" ]`, `subPackages = [ "." ]` (only the server, not `cmd/healthcheck`).
- `nixos-25.11` ships Go **1.25.10**, which satisfies `go 1.24` in `app/go.mod` — no override needed.

### Build log (Environment A)

```
building '/nix/store/jfigl9gk5i3c3bzknyb9a8ifmrqx973r-quicknotes-0.1.0.drv'...
quicknotes> Running phase: buildPhase
quicknotes> Building subPackage ./.
quicknotes> Running phase: checkPhase
quicknotes> ok          quicknotes      0.003s
quicknotes> Running phase: installPhase
quicknotes> Running phase: fixupPhase
quicknotes> shrinking /nix/store/xmgpgdf6fa0crxaizawsa6h1kxjx7iaq-quicknotes-0.1.0/bin/quicknotes
quicknotes> patchelf: cannot find section '.dynamic'. The input file is most likely statically linked
```

The `patchelf` message confirms the binary is **static** (`CGO_ENABLED=0` works). The unit tests run inside the sandbox (`checkPhase`) and pass.

### It runs

```
$ cd /tmp && /repo/result/bin/quicknotes &
2026/10/15 10:21:13 quicknotes listening on :8080 (notes loaded: 0)
$ curl -s localhost:8080/health
{"notes":0,"status":"ok"}
```

### Dev shell

```
$ nix develop -c sh -c 'go version; gopls version; golangci-lint --version'
go version go1.25.10 linux/arm64
golang.org/x/tools/gopls v0.20.0
golangci-lint has version 2.6.2 built with go1.25.10 from v2.6.2 on 1970-01-01T00:00:00Z
```

### Reproducibility proof — store hashes

| Environment | Store path | `nix-store --query --hash` |
|---|---|---|
| A (mounted repo) | `/nix/store/xmgpgdf6fa0crxaizawsa6h1kxjx7iaq-quicknotes-0.1.0` | `sha256:0qd4klg5q3hn2malql089wdq1cm39ndclxbj10689vzdqwg82f62` |
| B (fresh container + clone of `265e09c`) | same | `sha256:0qd4klg5q3hn2malql089wdq1cm39ndclxbj10689vzdqwg82f62` ✅ |

### Design questions

**a) Why `go build` is not bit-identical across machines.** The output depends on things outside the Git SHA:
- **Go toolchain version** — a different `go` on each machine produces different code; nothing in `go build` pins it.
- **Absolute paths** — without `-trimpath`, `/home/alice/...` vs `/Users/bob/...` is embedded in the binary (panic traces, DWARF).
- **Build ID and VCS stamping** — the build ID is derived from inputs incl. paths/toolchain; `-buildvcs` embeds commit, dirty flag and **commit time**.
- **Environment** — `CGO_ENABLED`, the C compiler and libc (with cgo), `GOFLAGS`, `GOAMD64`, `GOARM`.
- **Dependency resolution** — `GOPROXY`/`GOFLAGS=-mod=mod` can resolve different module versions if `go.sum` is missing or changed.
Nix removes all of these: pinned toolchain, fixed `/build` path in a sandbox, no network, fixed env, `SOURCE_DATE_EPOCH`.

**b) `vendorHash`** is the SRI SHA-256 of the **fixed-output derivation `goModules`**: the result of `go mod vendor` (the full `vendor/` tree with every dependency's source + `modules.txt`). That derivation is allowed network access *only because* its output hash is declared upfront; if the downloaded content differs by one byte, the build fails with `hash mismatch`. With **`vendorHash = null`** no vendor derivation is built: Nix expects the source to need no external modules (or to already contain `vendor/`). For QuickNotes that is exactly right (stdlib only). For a project with dependencies, `null` would make the sandboxed build fail, because `go` cannot download modules without network.

**c) `flake.lock`** pins every input to an exact commit + `narHash`. nixpkgs *is* the whole toolchain: Go, gcc, coreutils, `dockerTools`, the builder scripts. `nixos-25.11` in `flake.nix` is a **moving branch**; without the lock, each `nix build` would resolve it to today's tip, potentially with a different Go patch release → different store paths and different binary. If it is deleted before the second build, Nix re-resolves the branch, writes a new lock, and the hashes match only if the branch hasn't moved. The bonus shows this exact failure (red run).

**d) `buildGoModule` vs `buildGoApplication`.**
- `buildGoModule` (nixpkgs built-in): vendors all modules into **one** fixed-output derivation, verified by a single `vendorHash`. Any dependency change → recompute the hash. No extra inputs.
- `buildGoApplication` (from the **gomod2nix** flake): a `gomod2nix.toml` lists each module with its own hash; each module is a separate derivation → better caching and sharing between projects, no "one hash for everything", but an extra tool + generated file to keep in sync.
- **Pick for QuickNotes: `buildGoModule`.** Zero dependencies, so `vendorHash = null` and there is nothing to gain from per-module derivations; it avoids an extra flake input and code-generation step.

---

## Task 2 — Deterministic OCI image

### Flake snippet (`packages.docker`)

```nix
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
    Env = [ "ADDR=:8080" "DATA_PATH=/tmp/notes.json" "SEED_PATH=/data/seed.json" ];
  };
};
```

Built **without Docker** (pure Nix, inside the `nixos/nix` container that has no Docker). The image contains only the static binary + the seed file — no shell, no libc. Runs as the same non-root UID as Lab 6's distroless (`65532`).

### Build + load + run

```
docker-image-quicknotes.tar.gz> Cooking the image...
docker-image-quicknotes.tar.gz> Finished.
$ ls -lhL result
-r--r--r-- 1 root root 2.8M Jan  1  1970 result

$ docker load < qn-nix.tar.gz
Loaded image: quicknotes:nix
$ docker inspect quicknotes:nix --format 'User=... Entrypoint=... Ports=...'
User=65532:65532 Entrypoint=[/bin/quicknotes] Ports=map[8080/tcp:{}]
$ curl -s localhost:18081/health
{"notes":4,"status":"ok"}
$ curl -s localhost:18081/notes       # 4 seed notes returned
```

### Reproducibility proof — image digests

| Environment | `sha256sum result` |
|---|---|
| A | `03b7111426060f25aa289c167649e62b9bf078dd0237bb5c94e1b58e385fbdd7` |
| B (fresh container + clone) | `03b7111426060f25aa289c167649e62b9bf078dd0237bb5c94e1b58e385fbdd7` ✅ |

### Comparison with Lab 6 (`docker build --no-cache` ×2, same commit `b55a097`, same pinned base images)

```
REPOSITORY   TAG    IMAGE ID                                                                  SIZE
qn-lab6      run2   sha256:3a136e5bd4b7fcf76d4e947be87760a9d3dc3e8791ce4b88b6f4ddb048844cf6   21.5MB
qn-lab6      run1   sha256:1067e2e03d783c2537f918e3ebfcd87441c186e9a8143b69eebc134a8af3b3a4   21.5MB

sha256:1067e2e0…8af3b3a4  created=2026-10-15T11:46:32.797597626Z
sha256:3a136e5b…48844cf6  created=2026-10-15T11:46:37.757343753Z
```

| | Lab 6 (Dockerfile) | Lab 11 (Nix) |
|---|---|---|
| Two builds → same digest? | ❌ `1067e2e0…` ≠ `3a136e5b…` | ✅ `03b71114…` = `03b71114…` |
| `created` timestamp | wall-clock time of the build | `1970-01-01` (epoch) |
| Image size (Docker "content size") | 21.5 MB | **9.19 MB** (2.8 MB gzipped tarball) |
| Contents | distroless base (ca-certs, tzdata, passwd) + 2 binaries | only the static binary + seed |
| Needs Docker daemon to build | yes | no |

### Design questions

**e) Non-determinism in `docker build`** (even with the same Dockerfile + Git SHA + pinned base digests — exactly the Lab 6 case above):
- **`created` timestamp** in the image config, and in history entries per layer — changes every build; the config digest changes → the image ID changes.
- **File mtimes** inside layer tarballs: `COPY` and `RUN` outputs get build-time mtimes, so layer digests differ.
- **`RUN` steps execute with network** and unpinned state: `apk add`/`go mod download` fetch "latest", the base tag (if not pinned by digest) moves.
- **Build metadata**: BuildKit provenance/attestation manifests include timestamps and builder info (visible in the log: "exporting attestation manifest").
- Order of files in tar, uid/gid of the builder, and compression settings can differ between builders.
BuildKit can reduce this (`SOURCE_DATE_EPOCH`, `rewrite-timestamp=true`), but it is opt-in and `RUN` with network stays non-hermetic.

**f) Reproducible vs signed-but-not-reproducible, for an auditor.** A signature proves **who** published the image and that it wasn't changed afterwards — but not **what source it came from**. If the CI runner or a dependency was compromised, the malicious image is signed just as validly (SolarWinds-style). With a reproducible image, the auditor can take the published source + `flake.lock`, **rebuild independently, and get the same digest** — this proves the image corresponds exactly to the reviewed source, nothing was injected at build time, and no trust in the build machine is needed. Signature = provenance of the artifact; reproducibility = verifiability of its contents. Best is both.

**g) Trade-offs of Nix.**
- Steep learning curve: a separate functional language, unfamiliar concepts (derivations, store, FODs, flakes are still "experimental").
- Ecosystem friction: every language needs its builder + hashes (`vendorHash` updates on each dep change); poor fit for tools that expect a mutable FHS (`/usr/lib`) or download at build time.
- Tooling and support: Docker is everywhere (CI, IDEs, PaaS, docs, colleagues), a Dockerfile is readable by anyone in minutes; Nix needs a dedicated person.
- Disk and first-build cost: `/nix/store` grows quickly; macOS can't natively build Linux images (we had to use a container / remote builder).
- Most teams' threat model doesn't need bit-for-bit builds; "pinned base digest + lockfile + signed image" is "good enough" at a fraction of the cost.

---

## Bonus — CI-verified reproducibility

### Workflow: [`.github/workflows/nix-repro.yml`](../.github/workflows/nix-repro.yml)

```yaml
name: nix-repro

on:
  push:
  pull_request:

permissions:
  contents: read

jobs:
  build-a:
    runs-on: ubuntu-24.04
    outputs:
      digest: ${{ steps.digest.outputs.digest }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # same pin as Lab 3
      - uses: DeterminateSystems/nix-installer-action@3138316df39ed29be04236d7ffc686fa525866aa # v23
      - run: nix build .#docker -L
      - id: digest
        run: |
          D=$(sha256sum result | awk '{print $1}')
          echo "digest=$D" >> "$GITHUB_OUTPUT"
          echo "build-a digest: $D"

  build-b:
    runs-on: ubuntu-24.04
    outputs:
      digest: ${{ steps.digest.outputs.digest }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # same pin as Lab 3
      - uses: DeterminateSystems/nix-installer-action@3138316df39ed29be04236d7ffc686fa525866aa # v23
      - run: nix build .#docker -L
      - id: digest
        run: |
          D=$(sha256sum result | awk '{print $1}')
          echo "digest=$D" >> "$GITHUB_OUTPUT"
          echo "build-b digest: $D"

  compare:
    needs: [build-a, build-b]
    runs-on: ubuntu-24.04
    steps:
      - env:
          A: ${{ needs.build-a.outputs.digest }}
          B: ${{ needs.build-b.outputs.digest }}
        run: |
          echo "A = $A"
          echo "B = $B"
          if [ "$A" = "$B" ]; then echo "REPRODUCIBLE"; else echo "DIGEST MISMATCH"; exit 1; fi
```

- Triggers: every push to any branch + every PR.
- `build-a` and `build-b` run **in parallel on separate fresh runners**; each exposes the digest as a job output; `compare` needs both and fails on mismatch.
- Both third-party actions pinned by 40-char SHA: `actions/checkout@3d3c42e5…` (same as Lab 3), `DeterminateSystems/nix-installer-action@3138316d…` (v23). Permissions: `contents: read` only.

### Runs (green → red → green)

| Run | Commit | Result | `compare` output |
|---|---|---|---|
| [#1](https://github.com/illmmmiira/DevOps-Intro/actions/runs/38730092489/job/116405829989) | `d5bf43b` workflow added | ✅ | `A = B = 691befd340b6975a0e567582791b52d8a0144b63c78a78f530a4157e18362365` → `REPRODUCIBLE` |
| [#2](https://github.com/illmmmiira/DevOps-Intro/actions/runs/38730871732) | `1350b6c` broken on purpose | ❌ | `A = RED_A_DIGEST` / `B = RED_B_DIGEST` → `DIGEST MISMATCH` |
| [#3](https://github.com/illmmmiira/DevOps-Intro/actions/runs/38732587336) | `75d379e` revert | ✅ | digests equal again → `REPRODUCIBLE` |

**How it was broken (run #2):** only in `build-a`, the build step became
`nix build .#docker -L --override-input nixpkgs github:NixOS/nixpkgs/nixos-unstable`.
That simulates "the lockfile was ignored/deleted": build-a used a different nixpkgs (different Go + `dockerTools`) while build-b used the locked revision. Both builds **succeeded individually**, only the cross-check caught it. Reverted in `75d379e`.

The CI digest (`691befd3…`, x86_64-linux) differs from the local one (`03b71114…`, aarch64-linux) — expected: a different architecture is a different binary. Reproducibility is "same inputs incl. `system` → same output".

### Design questions

**h) "Reproducible on my laptop" vs "in CI".** A laptop result is a claim by one person on one machine whose state nobody else can see: cached store paths, local overrides, a dirty tree, an `~/.config/nix` with extra substituters, or just a copy-paste. CI is an **independent, ephemeral, documented environment**: the workflow file states exactly what ran, the runners start empty, logs are public and tied to a commit SHA, and the check **runs on every push** — so reproducibility is enforced continuously rather than claimed once. An auditor can verify the evidence without trusting the developer.

**i) Why two parallel jobs instead of one job building twice.** In one job the second `nix build` simply **reuses the first result from `/nix/store`** — Nix sees the same derivation hash and doesn't rebuild at all, so it compares a file with itself (always "equal"). Even with `--rebuild`, both builds share the same host: same kernel, CPU, hostname, filesystem, clock, environment, same substituted dependencies — so host-dependent leaks (timestamps within the same second, hostname, CPU features, file ordering of one filesystem) would be identical in both and **go unnoticed**. Two fresh runners differ in all of those, so they actually test independence.

**j) `SOURCE_DATE_EPOCH`.** Timestamps would normally leak in:
- **File mtimes** in the Nix store — Nix normalises every store path mtime to `1` (1970-01-01 00:00:01) after build; that's why `ls` showed `Jan 1 1970`.
- **The Go binary** — VCS stamping (`-buildvcs`) would embed commit time; `buildGoModule` builds from a store path without `.git`, so nothing is stamped.
- **The image tarball** — tar member mtimes, gzip header timestamp, and the OCI config `created` field / history.
`stdenv` exports `SOURCE_DATE_EPOCH` (by default `315532800` = 1980-01-01, or the source's mtime) so well-behaved tools (gzip, tar, compilers) use it instead of "now". **`dockerTools.buildImage`** additionally sets everything explicitly: its `created` argument defaults to **`"1970-01-01T00:00:01Z"`**, the layer tar is created with fixed `--mtime`, `--owner=0 --group=0`, `--sort=name` and `--numeric-owner`, and gzip runs with `-n` (no name/timestamp). Setting `created = "now"` would be the one way to break it on purpose. Note: just changing `SOURCE_DATE_EPOCH` in the CI env is **not** enough to break this image, since `buildImage` doesn't take it from the environment and the build runs in a sandbox — so the red run used a different nixpkgs input instead.
