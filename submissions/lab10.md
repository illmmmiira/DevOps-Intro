# Lab 10 — Ship QuickNotes to a Real Cloud

**Branch:** `feature/lab10` (from `upstream/main`). Lab 6 container files and the Lab 9 security-headers middleware were copied in so the image is the hardened one.

**Result:** pushing a signed `v*` tag → GitHub Actions builds the image → pushes it to public ghcr.io → calls Render's deploy hook → Render runs that exact tag at https://quicknotes-lab10-qodj.onrender.com.

**Platform used:** **Option A — Render** (free web service). Sign-up with GitHub asked for no card, so the Codespaces fallback wasn't needed.

---

## Task 1 — Tag → CI → ghcr.io

### Release workflow — `.github/workflows/release.yml`

```yaml
name: Release

on:
  push:
    tags: ["v*"]

permissions:
  contents: read

env:
  IMAGE: ghcr.io/illmmmiira/devops-intro/quicknotes

jobs:
  release:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
      packages: write
    env:
      RENDER_DEPLOY_HOOK: ${{ secrets.RENDER_DEPLOY_HOOK }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1  # v7.0.1

      - uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069  # v4.4.1

      - uses: docker/login-action@dbcb813823bdd20940b903addbd779551569679f  # v4.6.0
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - id: meta
        uses: docker/metadata-action@dc802804100637a589fabce1cb79ff13a1411302  # v6.2.0
        with:
          images: ${{ env.IMAGE }}
          tags: |
            type=ref,event=tag
            type=raw,value=latest

      - uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc  # v7.4.0
        with:
          context: app
          platforms: linux/amd64   # Render runs amd64
          provenance: false        # plain single-platform manifest
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}

      - name: Trigger Render deploy
        if: env.RENDER_DEPLOY_HOOK != ''
        run: |
          IMG=$(printf '%s' "${IMAGE}:${GITHUB_REF_NAME}" | jq -sRr @uri)
          curl -fsS -X POST "${RENDER_DEPLOY_HOOK}&imgURL=${IMG}" -o /dev/null -w 'render deploy hook: HTTP %{http_code}\n'
```

How it meets the requirements:

| Requirement | How |
|---|---|
| Trigger on `v*` tag | `on.push.tags: ["v*"]` |
| Build from `app/` | `context: app` |
| Push to `ghcr.io/<user>/<repo>/quicknotes` | `ghcr.io/illmmmiira/devops-intro/quicknotes` (repo name lower-cased — OCI names must be lowercase) |
| Version **and** `latest` tags | `type=ref,event=tag` → `v0.1.0`; `type=raw,value=latest` |
| Minimum permissions | workflow default `contents: read`; the job adds only `packages: write`; auth via the short-lived `GITHUB_TOKEN` |
| Every third-party action pinned to a 40-char SHA | all 5 actions, with the version in a comment |
| Pullable without auth | package visibility switched to **Public** once after the first push |

`platforms: linux/amd64` because Render runs amd64 and the laptop is Apple Silicon (arm64).

### Tagged release

```bash
git tag -a -s v0.1.0 -m "Lab 10 release"
git push origin v0.1.0
```

- Green release run (v0.1.0, 52 s): https://github.com/illmmmiira/DevOps-Intro/actions/runs/37835984842
- Second release (v0.1.1, also calls the Render hook): https://github.com/illmmmiira/DevOps-Intro/actions/runs/37837945943/job/113519718662

### Registry + clean pull

Registry: **`ghcr.io/illmmmiira/devops-intro/quicknotes`** — tags `v0.1.0`, `v0.1.1`, `latest`.

Pulled after `docker logout ghcr.io` and removing the local copy, i.e. with **no credentials**:

```
$ docker logout ghcr.io
Removing login credentials for ghcr.io
$ docker pull --platform linux/amd64 ghcr.io/illmmmiira/devops-intro/quicknotes:v0.1.0
v0.1.0: Pulling from illmmmiira/devops-intro/quicknotes
...
Digest: sha256:4fb81b24697c549ff967addff49e296fa36082722fceb1a81ce631ac4a786ad4
Status: Downloaded newer image for ghcr.io/illmmmiira/devops-intro/quicknotes:v0.1.0
$ docker pull --platform linux/amd64 ghcr.io/illmmmiira/devops-intro/quicknotes:latest
latest: Pulling from illmmmiira/devops-intro/quicknotes
Digest: sha256:4fb81b24697c549ff967addff49e296fa36082722fceb1a81ce631ac4a786ad4
Status: Downloaded newer image for ghcr.io/illmmmiira/devops-intro/quicknotes:latest
```

Same digest for both tags → at that moment `latest` and `v0.1.0` were the same image.

### Design questions a–c

**a) OIDC vs `GITHUB_TOKEN`.** For ghcr.io in the same repo, `GITHUB_TOKEN` is the right tool: GitHub mints it per job, it expires when the job ends, and its scope is set by `permissions:`. OIDC matters when the target is **outside GitHub** — AWS ECR, Google Artifact Registry, Azure ACR, a cloud deploy API. The job gets a signed OIDC token that states *which repo, ref, workflow and environment* it is, and the cloud exchanges it for short-lived credentials only if its trust policy matches (e.g. "only tags `v*` of `illmmmiira/DevOps-Intro`"). So you store **no long-lived cloud secret** at all, and access can be pinned to a branch/tag/environment. The same identity is what keyless **Cosign** signing (Sigstore) uses to sign the image.

**b) Why ship `:latest` next to the version tag.** The version tag is the contract: immutable by convention, used for deploys and rollbacks (Render runs `:v0.1.1`, not `:latest`). `:latest` is a convenience pointer for humans — `docker run ghcr.io/.../quicknotes` in a README, quick local tries, "what's newest?" — without knowing the version. The rule is: humans may use `latest`, automation pins the version (or the digest).

**c) `packages: write`.** Least privilege. The token can push packages and read code — nothing else. With `write-all`, any code that runs in the job (a compromised third-party action, a malicious build dependency — like the 2025 `tj-actions/changed-files` compromise that dumped tokens) could use the token to **push commits to `main`, move or create tags and releases, delete branches, cancel or re-run workflows, comment on and close issues and PRs**. With `contents: read` + `packages: write` the worst case is pushing a bad image — still bad, but the source code, tags and history stay untouched and the bad image is visible and replaceable. SHA-pinning the actions closes the other half of that attack (a moved tag can't swap the action's code).

---

## Task 2 — Render (Option A)

Config is in [`cloud/render.md`](../cloud/render.md); teardown in [`cloud/teardown.md`](../cloud/teardown.md).

- **URL:** https://quicknotes-lab10-qodj.onrender.com (Free, Frankfurt, existing image, `PORT=8080`, `ADDR=:8080`, health check `/health`)
- Render added the `-qodj` suffix because `quicknotes-lab10` was taken. The un-suffixed host is **someone else's service** — it answers too, but without the Lab 9 headers.

### `curl -v` against `/health`

```
$ curl -v https://quicknotes-lab10-qodj.onrender.com/health
* Connected to quicknotes-lab10-qodj.onrender.com (216.24.57.16) port 443
* SSL connection using TLSv1.3 / AEAD-CHACHA20-POLY1305-SHA256
*  subjectAltName: host "quicknotes-lab10-qodj.onrender.com" matched cert's "*.onrender.com"
* using HTTP/2
> GET /health HTTP/2
< HTTP/2 200
< content-type: application/json
< cache-control: no-store
< content-security-policy: default-src 'none'; frame-ancestors 'none'
< cross-origin-embedder-policy: require-corp
< cross-origin-opener-policy: same-origin
< cross-origin-resource-policy: same-origin
< referrer-policy: no-referrer
< x-content-type-options: nosniff
< x-frame-options: DENY
< x-render-origin-server: Render
< server: cloudflare
< cf-ray: a477cb38c9e471d9-FRA
{"notes":4,"status":"ok"}
```

The Lab 9 security headers prove it's this image. `/notes` returns the 4 seed notes as JSON.

### Port — deploy log

```
==> Starting service...
==> Setting WEB_CONCURRENCY=1 by default, based on available CPUs in the instance
[9dtvn] quicknotes listening on :8080 (notes loaded: 4)
==> Your service is live 🎉
==> Available at your primary URL https://quicknotes-lab10-qodj.onrender.com
```

No `New primary port detected … Restarting deploy` — `PORT` and `ADDR` agreed on first boot.

### Deploy from CI

The last workflow step (above) calls the deploy hook with `imgURL=<image>:<tag>`; the hook URL is the GitHub secret `RENDER_DEPLOY_HOOK`. Tag `v0.1.1` → release run step log:

```
render deploy hook: HTTP 200
```

→ Render deploy **v0.1.1**, source `55f8c37`, trigger **Deploy Hook**, 12.0 s, `Deploy succeeded | Live`. On v0.1.0 the secret didn't exist yet, so the step was skipped and the run stayed green.

### Warm latency (5 consecutive requests)

```
$ for i in 1 2 3 4 5; do curl -s -o /dev/null -w '%{time_total}\n' $URL/health; done
0.518349
0.493895
0.493367
0.410314
0.462745
```

**Warm p50 = 493 ms** (sorted: 410, 463, **493**, 494, 518). Measured from Russia; each `curl` opens a new TLS connection to Cloudflare's Frankfurt edge (`cf-ray …-FRA`).

TBD_RENDER_HYPERFINE

### Cold latency (after ≥ 15 min idle → spin-down)

| # | Time (MSK) | Result |
|---|---|---|
| 1 | 23:48 | first request: `HTTP 000` after **29.98 s** (connection dropped mid-wake); next request immediately `200` → wake took **≥ 30 s** |
| 2 | TBD_COLD2_TIME | TBD_COLD2 |
| 3 | TBD_COLD3_TIME | TBD_COLD3 |

Cold #1 shows a real-world effect: something on the path (client side, ~30 s idle timeout) dropped the connection before Render finished waking the container. For #2 and #3 the measurement retries until the first `200` and reports the total wake time.

### Note persistence across spin-down

```
$ curl -s -X POST $URL/notes -H 'Content-Type: application/json' \
    -d '{"title":"lab10 persistence test","body":"will I survive spin-down?"}'
{"id":5,"title":"lab10 persistence test","body":"will I survive spin-down?","created_at":"2026-10-08T20:20:53.878575682Z"}
$ curl -s $URL/notes      # → 5 notes, including id 5

# … 27 minutes idle, service spun down …

$ curl -s $URL/notes      # after wake → only the 4 seed notes; id 5 is gone
```

**The note was lost.** See f).

### Design questions d–f

**d) Why Render's wake is so much slower than Cloud Run's scale-to-zero.** On Render free, spin-down really releases the instance. A wake has to schedule the service on a node, pull/unpack the image (if not cached), start the container, wait for it to bind the port and pass the health check, then route the held request — tens of seconds (we saw ≥ 30 s; Render's banner says "50 seconds or more"). Cloud Run is built around scale-to-zero as the *normal* state: lightweight sandboxes that boot in milliseconds, images pre-staged / lazily streamed, and a request-driven autoscaler, so a small Go binary cold-starts in well under a second to a couple of seconds. Render free optimizes **cost for hobby projects** — idle capacity is reclaimed and the slow wake is part of the "upgrade for always-on" deal. Cloud Run optimizes **per-request elasticity**, billing only for request time while still serving fast.

**e) Why `PORT` and not `EXPOSE`.** `EXPOSE` is only metadata in the image — not enforced, often missing or wrong, and it can list several ports. The platform owns the router/load balancer, so it *tells* the app where to listen, the same way for every language (the 12-factor / Heroku convention). I set **`PORT=8080`** (so Render routes to 8080) and **`ADDR=:8080`** (QuickNotes' own setting), without touching code. With a mismatch, Render waits, scans for the port the process actually opened, logs `New primary port detected` and **restarts the deploy** — roughly 45 s extra on every bad deploy, a short failed window on first deploy, and confusing logs.

**f) Existing image vs Render building from the repo; where the note went.**

| | Existing image (used) | Render builds from repo |
|---|---|---|
| What runs | the exact artifact CI built and Lab 9 scanned (one digest) | a second build nobody scanned; floating base tags can drift |
| Reproducibility | pin by tag/digest; rollback = redeploy an old tag | depends on Render's build env and cache |
| Deploy speed | 12–21 s (pull only) | full Docker build each deploy (Render's own layer cache helps) |
| Setup | needs a registry + public package / credentials | simplest: connect repo, auto-deploy on push |

The **note** was written to `/data/notes.json` *inside the container's filesystem*. Free instances have no persistent disk, so when the service spun down the container and its writable layer were thrown away; on wake a fresh container started, found no data file and re-seeded from `seed.json` → 4 notes. To keep data you'd need a Render persistent disk (paid) or an external database.

---

## Bonus — Cloudflare Tunnel + comparison

Same hardened image running locally (`docker run -d --name qn-local -p 8080:8080 quicknotes:lab9`), exposed with a quick tunnel (`cloudflared 2026.10.0`):

```
$ cloudflared tunnel --url http://localhost:8080
INF Your quick Tunnel has been created! Visit it at:
INF https://being-regulatory-argument-goal.trycloudflare.com
INF Registered tunnel connection ... location=fra07 protocol=quic
INF SUMMARY: Environment is healthy. cloudflared will use 'quic' as primary protocol.
```

**Different network:** opened `https://being-regulatory-argument-goal.trycloudflare.com/health` on a phone with Wi-Fi off (**LTE**) → `{"notes":4,"status":"ok"}` (screenshot, 23:27).

**Benchmark:** `hyperfine -N --warmup 5 --runs 50 "curl -s -o /dev/null <url>/health"`, percentiles from hyperfine's JSON. Both targets measured from the same laptop on the same network so they're comparable; for the tunnel every request leaves the laptop, goes to Cloudflare's edge and comes back down the tunnel — a real Internet round trip, not localhost.

```
Tunnel: n=50 p50=594ms p95=651ms min=516ms max=786ms   (mean 590 ± 51 ms)
TBD_RENDER_PCT
```

| | Render (free) | Cloudflare quick tunnel |
|---|---|---|
| Warm p50 | TBD_R50 | **594 ms** |
| Warm p95 | TBD_R95 | **651 ms** |
| Cold start | TBD_COLD_SUMMARY | N/A — container runs continuously on the laptop |
| Public URL stability | stable (`quicknotes-lab10-qodj.onrender.com`) | ephemeral — new random URL on every `cloudflared` restart |
| Cost | free (750 instance-h/month, sleeps when idle) | free (no account, no uptime guarantee) |

### Design questions g–i

**g) Which is "really cloud"?** Render: compute, scheduling and networking all belong to the provider — that's cloud hosting. Tunnel: the compute is my laptop; Cloudflare only provides the edge (DNS, TLS, anycast entry, DDoS shielding) and proxies requests down an outbound connection. A user sees the same thing — an HTTPS URL that returns JSON — so the label doesn't matter to them. What matters is what's behind it: with the tunnel, availability = my laptop being awake and online, capacity = my laptop, and nobody is on call. "Cloud" is really about who operates the compute and what they guarantee.

**h) What dominates warm latency.** Not QuickNotes — it answers `/health` in well under a millisecond. Each `curl` run pays DNS + TCP + a full TLS handshake + the request, i.e. several round trips from Russia to Cloudflare's Frankfurt edge. Render: client → Cloudflare FRA → Render origin in Frankfurt (one short hop) → back. Tunnel: client → Cloudflare edge → down the QUIC tunnel **back to the same laptop** → response all the way back — a hairpin, so the origin leg is a second Internet trip instead of a datacenter hop. That's why the tunnel can't really beat Render even though the server is on the client's own machine.

**i) When the tunnel is (and isn't) right.** Right: home labs and on-prem services behind NAT/firewalls — outbound-only, no open inbound ports or public IP; sharing a dev build with a stakeholder for an afternoon; receiving webhooks on a local machine; putting a private origin behind Cloudflare Access. For anything lasting, use a **named** tunnel on your own domain, run as a service on an always-on host. Never: a *quick* tunnel for production (Cloudflare states no uptime guarantee, and the URL changes on restart), an origin that's a laptop that sleeps or roams, or anything needing high availability or capacity beyond one box.

---

Teardown steps: [`cloud/teardown.md`](../cloud/teardown.md).
