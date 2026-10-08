# Lab 9 — DevSecOps: Trivy + ZAP + govulncheck

**Branch:** `feature/lab9` (from `upstream/main`). Lab 6 container files (`app/Dockerfile`, `app/.dockerignore`, `app/cmd/healthcheck/main.go`, `compose.yaml`) and Lab 3 `.github/workflows/ci.yml` were copied in (commit `f737801`).

**Tools (pinned):** Trivy `aquasec/trivy:0.59.1`, ZAP `ghcr.io/zaproxy/zaproxy:2.16.1`, govulncheck `v1.1.4`.

**Artifacts in the repo:**
- `security/trivy/image.txt`, `image-after.txt`, `fs.txt`, `config.txt`
- `security/sbom-quicknotes.cdx.json` (CycloneDX 1.6)
- `security/zap/before.{html,json,txt}`, `after.{html,json,txt}`

| Commit | What |
|---|---|
| `25d9808` | Trivy scans + SBOM, Go builder `1.24-alpine` → `1.26.6-alpine` (fixes 19 CVEs) |
| `997e0d7` | Security-headers middleware + unit test, ZAP before/after |
| `5964998` | govulncheck CI job |
| `20e1e6e` | CI matrix `1.23/1.24` → `1.24/1.26.6` |
| `0ffa794` / `0a09bd7` | Vulnerable dep added (red) / reverted (green) |

---

## Task 1 — Trivy

All scans ran via Docker: `trivy() { docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v trivy-cache:/root/.cache/ -v "$PWD":/work -w /work aquasec/trivy:0.59.1 "$@"; }`

### 1. Image scan — `trivy image --severity HIGH,CRITICAL quicknotes:lab6`

```
quicknotes:lab6 (debian 12.15)
Total: 0 (HIGH: 0, CRITICAL: 0)

app/healthcheck (gobinary)
Total: 19 (HIGH: 19, CRITICAL: 0)

app/quicknotes (gobinary)
Total: 19 (HIGH: 19, CRITICAL: 0)
│ stdlib │ CVE-2026-25679 │ HIGH │ fixed │ v1.24.13 │ 1.25.8, 1.26.1 │ net/url: Incorrect parsing of IPv6 host literals ...
```

The distroless base has **0** findings. All 19 are in the **Go standard library v1.24.13** compiled into both binaries — the Dockerfile used the floating tag `golang:1.24-alpine`, and Go 1.24 no longer gets security fixes.

### 2. Filesystem scan — `trivy fs --severity HIGH,CRITICAL .`
```
[gomod] Detecting vulnerabilities...
(no findings — go.mod has no dependencies)
```
(`.vagrant/`, `Claude outputs/` and `.env` were skipped — local-only files with keys/passwords, not part of the repo.)

### 3. Config scan — `trivy config .`
```
Detected config files  num=1
(no misconfigurations)
```
The Dockerfile already follows best practice (multi-stage, distroless `nonroot`, own healthcheck). Trivy printed `ERROR [rego]` lines for an unrelated AWS EC2 rule (`specify_ami_owners.rego`) that this Trivy version can't parse — no AWS files exist here, so this does not affect the result.

### 4. SBOM — `trivy image --format cyclonedx --output security/sbom-quicknotes.cdx.json quicknotes:lab6`

(The lab says `trivy sbom`; that subcommand *scans* an existing SBOM — generating one is `trivy image --format cyclonedx`.)

```json
{
  "$schema": "http://cyclonedx.org/schema/bom-1.6.schema.json",
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "serialNumber": "urn:uuid:dc50f578-4fa9-4033-8d7b-3305aaa4f85f",
  "version": 1,
  "metadata": {
    "timestamp": "2026-10-08T13:20:23+00:00",
    "tools": {
      "components": [
        {
          "type": "application",
          "group": "aquasecurity",
          "name": "trivy",
          "version": "0.59.1"
        }
      ]
    },
    "component": {
      "bom-ref": "pkg:oci/quicknotes@sha256%3A1ebb8f4e...?arch=arm64&repository_url=index.docker.io%2Flibrary%2Fquicknotes",
      "type": "container",
      "name": "quicknotes:lab6",
      "purl": "pkg:oci/quicknotes@sha256%3A1ebb8f4e...?arch=arm64&repository_url=index.docker.io%2Flibrary%2Fquicknotes",
      "properties": [
        {
          "name": "aquasecurity:trivy:DiffID",
          "value": "sha256:0e0be256482a617b479c87a055f9018c289afff6e683785a4e613bd6dfdd7eb4"
        },
        {
          "name": "aquasecurity:trivy:DiffID",
```

### Triage — every HIGH/CRITICAL

Each CVE appears twice (once per binary: `quicknotes` and `healthcheck`); the decision is the same for both.

| CVE | Package | Fixed in | Disposition |
|---|---|---|---|
| CVE-2026-25679 | net/url | 1.25.8 / 1.26.1 | **FIX** — `25d9808` |
| CVE-2026-27145 | crypto/x509 | 1.25.11 / 1.26.4 | **FIX** — `25d9808` |
| CVE-2026-32280 | crypto/x509, crypto/tls | 1.25.9 / 1.26.2 | **FIX** — `25d9808` |
| CVE-2026-32281 | crypto/x509 | 1.25.9 / 1.26.2 | **FIX** — `25d9808` |
| CVE-2026-32283 | crypto/tls | 1.25.9 / 1.26.2 | **FIX** — `25d9808` |
| CVE-2026-33811 | net | 1.25.10 / 1.26.3 | **FIX** — `25d9808` |
| CVE-2026-33814 | net/http (HTTP/2) | 1.25.10 / 1.26.3 | **FIX** — `25d9808` |
| CVE-2026-33818 | encoding/asn1 | 1.25.13 / 1.26.6 | **FIX** — `25d9808` |
| CVE-2026-39820 | net/mail | 1.25.10 / 1.26.3 | **FIX** — `25d9808` |
| CVE-2026-39821 | x/net/idna (net/http) | 1.25.13 / 1.26.6 | **FIX** — `25d9808` |
| CVE-2026-39822 | os.Root | 1.25.12 / 1.26.5 | **FIX** — `25d9808` |
| CVE-2026-39836 | net | 1.25.10 / 1.26.3 | **FIX** — `25d9808` |
| CVE-2026-42499 | net/mail | 1.25.10 / 1.26.3 | **FIX** — `25d9808` |
| CVE-2026-42504 | mime | 1.25.11 / 1.26.4 | **FIX** — `25d9808` |
| CVE-2026-56853 | net/http (h2c) | 1.25.13 / 1.26.6 | **FIX** — `25d9808` |
| CVE-2026-56858 | html/template | 1.25.13 / 1.26.6 | **FIX** — `25d9808` |
| CVE-2026-56859 | encoding/xml | 1.25.13 / 1.26.6 | **FIX** — `25d9808` |
| CVE-2026-56860 | net/url | 1.25.13 / 1.26.6 | **FIX** — `25d9808` |
| CVE-2026-56862 | crypto/tls | 1.25.13 / 1.26.6 | **FIX** — `25d9808` |

Why FIX and not ACCEPT: many of these (`net/mail`, `html/template`, `encoding/xml`) are probably not reachable from QuickNotes, but all of them are fixed by one line in the Dockerfile at zero risk — triaging each one separately would cost more than fixing all. The highest required version was 1.26.6, so the builder is now pinned to an exact patch: `FROM golang:1.26.6-alpine`. `go.mod` stays `go 1.24` (the minimum language version).

**After the fix** — `trivy image --severity HIGH,CRITICAL quicknotes:lab9`:
```
quicknotes:lab9 (debian 12.15)
Total: 0 (HIGH: 0, CRITICAL: 0)
```
Both Go binaries have no findings any more (Trivy doesn't print a section for a clean binary).

### Design questions a–d

**a)** Severity says how bad a bug *could* be, not how bad it is *for us*. Also matters: is the vulnerable code actually reachable from our app, is there a public exploit or is it being exploited (KEV / EPSS), and the deployment context — internet-facing or internal, does it handle untrusted input, what does it run as. An HTTP/2 DoS in an internet-facing API is urgent; an XML bug in a package we never import isn't.

**b)** Every package in the image is attack surface and a future CVE. Distroless static has no shell, no package manager and only ~5 OS packages — here it had **0** findings while all 19 came from our own Go binary. A small base removes whole classes of findings at once, and with no shell an attacker who gets in has almost nothing to work with.

**c)** Right: a finding you have actually triaged as a false positive or a dated acceptance — with a comment saying why, who decided, and a re-check date. Theater: suppressing findings just to make the pipeline green, with no reason and no expiry. Then the scanner still "runs" but nobody sees the risk any more.

**d)** When the next Log4Shell-style CVE is published, the question is "do we ship this component, and which version?". With an SBOM per image you search a file and answer in minutes instead of rebuilding and scanning every image or reading Dockerfiles. You can also re-scan old SBOMs against new CVE data without the images (`trivy sbom file.json`).

---

## Task 2 — ZAP baseline + fix

App: `docker run -d --name qn -p 8080:8080 quicknotes:lab9`.
ZAP: `zap-baseline.py -t http://host.docker.internal:8080/notes` (passive only; `host.docker.internal` because ZAP runs in its own container on a Mac).

The target is `/notes`, not `/`: QuickNotes has no root page, so a first run against `/` only saw 404 pages, and ZAP skips most header checks on error responses — that would have hidden the real findings.

### Before (`security/zap/before.txt`)
```
WARN-NEW: X-Content-Type-Options Header Missing [10021] x 1
    http://host.docker.internal:8080/notes (200 OK)
WARN-NEW: Storable and Cacheable Content [10049] x 2
    http://host.docker.internal:8080/ (404 Not Found)
    http://host.docker.internal:8080/notes (200 OK)
WARN-NEW: ZAP is Out of Date [10116] x 1
WARN-NEW: Insufficient Site Isolation Against Spectre Vulnerability [90004] x 1
    http://host.docker.internal:8080/notes (200 OK)
FAIL-NEW: 0  WARN-NEW: 4  PASS: 63
```

### Triage

| ID | Name | Risk | URL | Disposition |
|---|---|---|---|---|
| 10021 | X-Content-Type-Options Header Missing | Low | `/notes` | **FIX** — `nosniff` in middleware (`997e0d7`) |
| 10049 | Storable and Cacheable Content | Info | `/`, `/notes` | **FIX** — `Cache-Control: no-store`; notes are user data and shouldn't sit in shared caches |
| 90004 | Insufficient Site Isolation (Spectre) | Low | `/notes` | **FIX** — COOP / COEP / CORP headers |
| 10116 | ZAP is Out of Date | Info | — | **ACCEPT** — the ZAP version is pinned on purpose for reproducible scans. Re-evaluate the pin by **2027-04-08** |

### The fix — `app/security_headers.go`

```go
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func buildHandler(routes http.Handler) http.Handler { return securityHeaders(routes) }
```
`main.go`: `Handler: buildHandler(server.Routes())` — the whole router is wrapped, so every route (including 404s) gets the headers.

**Test** — `app/security_headers_test.go` requests `/health`, `/notes`, `/metrics` and `/does-not-exist` through `buildHandler` and checks 4 headers. All tests pass:
```
ok   quicknotes  1.770s
```
With the middleware temporarily removed (`return routes`), the test fails — so the fix is guarded:
```
--- FAIL: TestSecurityHeadersOnAllRoutes (0.00s)
    /health: header X-Content-Type-Options = "", want "nosniff"
    /health: header Cache-Control = "", want "no-store"
    /notes: header Cross-Origin-Embedder-Policy = "", want "require-corp"
    ...
```

### After (`security/zap/after.txt`)
```
$ curl -sI http://localhost:8080/notes
HTTP/1.1 200 OK
Cache-Control: no-store
Content-Security-Policy: default-src 'none'; frame-ancestors 'none'
Cross-Origin-Embedder-Policy: require-corp
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Resource-Policy: same-origin
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
X-Frame-Options: DENY

PASS: X-Content-Type-Options Header Missing [10021]
PASS: Insufficient Site Isolation Against Spectre Vulnerability [90004]
WARN-NEW: Non-Storable Content [10049] x 4
WARN-NEW: ZAP is Out of Date [10116] x 1
FAIL-NEW: 0  WARN-NEW: 2  PASS: 65
```
10021 and 90004 are gone. Rule 10049 now reports **"Non-Storable Content"** — an informational note confirming that `no-store` works, so it is a **FALSE POSITIVE** (this is the intended state). 10116 stays as ACCEPT.

### Design questions e–g

**e)** One place to change, impossible to forget: every route — including new ones and the router's own 404s — gets the headers automatically. Per-handler `Header().Set` calls get copied inconsistently and the next new endpoint misses them. It's also one thing to test.

**f)** `default-src 'none'` blocks the browser from loading *anything* — scripts, styles, images, fonts, fetch calls, frames. A website would render as a blank or unstyled page. QuickNotes only returns JSON that no browser renders as a page, so nothing is lost and any injected content can't execute. A website instead needs an allowlist of exactly the sources it uses.

**g)** If you mark everything "accepted" without reading it, the triage table looks complete but means nothing — and a real issue hidden among the informational ones (e.g. a leaking header, a cacheable response with private data) ships unnoticed. Reading each one is cheap here; the 10049 finding above actually led to a real fix (`no-store`).

---

## Bonus — govulncheck as a PR gate

Job added to `.github/workflows/ci.yml` (commit `5964998`), and `ci-ok` now also `needs` it:

```yaml
  govulncheck:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1  # v7.0.1
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e  # v7.0.0
        with:
          # same toolchain as the release image (app/Dockerfile builder)
          go-version: "1.26.6"
          cache-dependency-path: app/go.mod
      - name: Install govulncheck (pinned)
        run: go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
      - name: govulncheck
        working-directory: app
        run: govulncheck ./...
```

**Two deviations, on purpose:**
- **This job uses Go 1.26.6, not 1.24.** govulncheck also checks the *stdlib of the Go it runs with*. Go 1.24 is end-of-life and has the 19 CVEs from Task 1, so the job would be red forever for a toolchain we no longer ship. It must scan the toolchain the release image is built with.
- **Matrix changed from `["1.23", "1.24"]` to `["1.24", "1.26.6"]`** (commit `20e1e6e`). Upstream's `go.mod` now says `go 1.24`, so the 1.23 jobs failed with `go.mod requires go >= 1.24 (running go 1.23.12; GOTOOLCHAIN=local)`. The new matrix tests the minimum supported version and the release toolchain.

The runs were triggered by a draft PR inside my fork (`illmmmiira/DevOps-Intro#3`, closed without merging).

**Green baseline** — all jobs passed: `vet (1.24)`, `vet (1.26.6)`, `test (1.24)`, `test (1.26.6)`, `lint`, `govulncheck`, `ci-ok`.

**Red — with a known-vulnerable dependency** (commit `0ffa794`: `golang.org/x/text@v0.3.5` + `vulndemo.go` calling `language.Parse`):
```
Vulnerability #1: GO-2021-0113
    Out-of-bounds read in golang.org/x/text/language
  Module: golang.org/x/text
    Found in: golang.org/x/text@v0.3.5
    Fixed in: golang.org/x/text@v0.3.7
    Example traces found:
      #1: vulndemo.go:6:36: quicknotes.init#1 calls language.Parse

Your code is affected by 1 vulnerability from 1 module.
This scan also found 1 vulnerability in packages you import and 2
vulnerabilities in modules you require, but your code doesn't appear to call
these vulnerabilities.
Error: Process completed with exit code 3.
```
`govulncheck` and `ci-ok` went red, so the PR was blocked.

**Green again** after `git revert` (commit `0a09bd7`) — all 7 checks passed; `app/go.mod` is back to no dependencies.

### Design questions h–j

**h)** "The module has a CVE" only says the vulnerable code is somewhere in the dependency tree. "We call the affected function" means it can actually be triggered. In the red run govulncheck found 4 vulnerabilities but failed the build on only **1** — the one with a real call path (`init → language.Parse`). Trivy would report all 4 with the same priority. Reachability cuts triage work down to the findings that matter.

**i)** The scanner is part of the gate. With `@latest`, a new govulncheck release can change output, exit codes or flags overnight and turn every PR red (or green) with no change in our code — and you can't reproduce an old run. A pinned scanner is also a supply-chain control: you only run a version you chose. The vuln *database* still updates every run, which is what you want.

**j)** govulncheck only knows Go code and the Go vuln DB. It doesn't see OS packages in the base image (e.g. a vulnerable `libssl` or `ca-certificates`), non-Go files in the image, secrets baked into layers, or Dockerfile/config misconfigurations — Trivy's image, secret and config scanners cover those.
