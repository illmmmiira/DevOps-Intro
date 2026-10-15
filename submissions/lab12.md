# Lab 12 — WebAssembly: a QuickNotes `/time` Endpoint on Spin

## Test machine and pinned versions

| | |
|---|---|
| Hardware | MacBook Air, Apple M3 (8 cores), 16 GB RAM |
| OS | macOS 26.6.2 |
| Spin | 4.2.1 (`fa7dc37`, 2026-09-30) |
| TinyGo | **0.41.1** (Go 1.26.9, LLVM 20.1.1), pinned, see "Version notes" |
| Spin Go SDK | `github.com/spinframework/spin-go-sdk/v2` **v2.2.1** |
| wasmtime | 49.0.3 |
| Docker | 29.2.1 (Docker Desktop) |
| hyperfine | 2.0.0 |

### Version notes (what broke and why the versions are pinned)

1. **Spin 4 changed the Go template.** `spin new -t http-go` scaffolded a project for `spin-go-sdk/v3` built with `go tool componentize-go build` (standard Go, no TinyGo). Out of the box its build failed: `Error: failed to read path for WIT [wit]`. The lab targets the v2 SDK + TinyGo `wasip1` + `c-shared`, so I kept the scaffolded `spin.toml` (route, `allowed_outbound_hosts`), switched `go.mod` to the v2 SDK and set the build command the lab requires.
2. **TinyGo 0.43.0 builds, but the component panics on the first request**: `nil pointer dereference` in `runtime.setupDeferFrame` ← `sync.Once.Do` ← `net/textproto.CanonicalMIMEHeaderKey` ← `spin_http_handle_http_request`. TinyGo 0.42 reimplemented defer/recover; in a `c-shared` reactor the host calls the export outside TinyGo's scheduler, so the new defer code has no current task. **TinyGo 0.41.1** (the lab-validated line) works. It supports Go ≤ 1.26 while the host has Go 1.27.1, so Go 1.26.9 (`brew install go@1.26`) is put first in `PATH` only for the build.
3. Spin 4.2 still runs a classic TinyGo `wasip1` core module that exports the legacy `fermyon:spin/inbound-http` handler.

---

## Task 1 — WASM endpoint with the Spin SDK

### `wasm/spin.toml`

```toml
#:schema https://schemas.spinframework.dev/spin/manifest-v2/latest.json

spin_manifest_version = 2

[application]
name = "qn-time"
version = "0.1.0"
authors = ["illmmmiira <i.usmanova@innopolis.university>"]
description = "QuickNotes /time endpoint as a Wasm component"

[[trigger.http]]
route = "/time"
component = "qn-time"

[component.qn-time]
source = "main.wasm"
allowed_outbound_hosts = []
[component.qn-time.build]
command = "tinygo build -target=wasip1 -gc=leaking -buildmode=c-shared -no-debug -o main.wasm ."
watch = ["**/*.go", "go.mod"]
```

### `wasm/main.go`

```go
package main

import (
	"fmt"
	"net/http"
	"time"

	spinhttp "github.com/spinframework/spin-go-sdk/v2/http"
)

// Moscow is UTC+3 all year (no DST since 2014). A fixed zone avoids
// time.LoadLocation, which needs tzdata that a Wasm module doesn't ship.
var msk = time.FixedZone("MSK", 3*60*60)

func init() {
	spinhttp.Handle(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprintln(w, `{"error":"method not allowed"}`)
			return
		}
		now := time.Now().In(msk)
		w.Header().Set("Content-Type", "application/json")
		// Build JSON by hand: no reflection, works on every Go/TinyGo target.
		fmt.Fprintf(w, `{"unix":%d,"iso":%q,"hour_minute":%q,"timezone":"Europe/Moscow","utc_offset":"+03:00"}`+"\n",
			now.Unix(), now.Format(time.RFC3339), now.Format("15:04"))
	})
}

func main() {}
```

Choices (from the lab's pitfalls): `time.FixedZone` instead of `time.LoadLocation("Europe/Moscow")` (no tzdata inside the sandbox; Moscow has no DST since 2014), JSON built with `fmt` instead of `encoding/json` + `map[string]any` (TinyGo reflection limits). Non-GET → 405 with `Allow: GET`.

### Build

```
$ spin build
Building component qn-time with `tinygo build -target=wasip1 -gc=leaking -buildmode=c-shared -no-debug -o main.wasm .`
Finished building all Spin components
$ ls -l main.wasm
-rw-r--r--  1 ilmira  staff  313326 Oct 15 16:14 main.wasm      # 306 KB
```

### Run and verify

```
$ spin up --listen 127.0.0.1:3000
Serving http://127.0.0.1:3000
Available Routes:
  qn-time: http://127.0.0.1:3000/time

$ curl -s -i http://127.0.0.1:3000/time
HTTP/1.1 200 OK
content-type: application/json
content-length: 125

{"unix":1792070086,"iso":"2026-10-15T16:14:46+03:00","hour_minute":"16:14","timezone":"Europe/Moscow","utc_offset":"+03:00"}

$ curl -s http://127.0.0.1:3000/time | python3 -m json.tool
{
    "unix": 1792070086,
    "iso": "2026-10-15T16:14:46+03:00",
    "hour_minute": "16:14",
    "timezone": "Europe/Moscow",
    "utc_offset": "+03:00"
}
```

### Experiment for question b — same code without `-buildmode=c-shared`

```
$ tinygo build -target=wasip1 -gc=leaking -no-debug -o nocshared.wasm .
$ spin up -f spin-nocshared.toml --listen 127.0.0.1:3001
$ curl -s -i http://127.0.0.1:3001/time
HTTP/1.1 500 Internal Server Error
content-length: 0

ERROR spin_trigger_http::server: Error processing request: error while executing at wasm backtrace:
    0:  0x3cd2e - main!canonical_abi_realloc
    1:  0x5097b - <unknown>!allocate_stack
    2:  0x4de97 - <unknown>!cabi_export_realloc
Caused by:
    wasm trap: wasm `unreachable` instruction executed
```

### Design questions

**a) Browser WASM (`js/wasm`) vs server WASM (TinyGo `wasip1`).**
`js/wasm` assumes a JavaScript host: the module imports a set of functions from `wasm_exec.js` and talks to the outside world through `syscall/js` (DOM, `fetch`, timers, console). `wasip1` has **no JS at all**: no `syscall/js`, no DOM, no `fetch`. It also lacks things a normal OS process has: no sockets (wasip1 has no networking API), no threads (TinyGo goroutines are cooperative on one thread), no `fork`/`exec`, no signals, and only the directories the host pre-opens. What you gain: the module runs in **any WASI runtime** (wasmtime, Spin, WasmEdge) on any OS/CPU without a browser or JS glue; the system interface is standardised and capability-based (the host decides exactly which files, env vars and clocks are visible); the binary is small (306 KB) and starts in microseconds once compiled.

**b) Why `-buildmode=c-shared`.**
Spin does not run the module like a program; it **calls an exported function** (`handle-http-request` of `fermyon:spin/inbound-http`) once per request. `c-shared` makes TinyGo produce a **reactor**: it exports `_initialize` (runs the Go runtime setup and package `init()`s, where `spinhttp.Handle` registers our handler) plus the handler exports, and **no `_start`**. Without the flag TinyGo produces a **command** module: all runtime setup lives inside `_start` → `main()`. Spin never calls `_start`, so the heap and runtime are never initialised; the first thing Spin does is ask the guest to allocate memory for the request (`cabi_export_realloc` → `canonical_abi_realloc`), and that traps with `unreachable`. Result exactly as above: HTTP 500, empty body, nothing in the component's own logs.

**c) Capability model and `allowed_outbound_hosts = []`.**
A Wasm guest has **no ambient authority**: it cannot make syscalls, it can only call the functions the host chose to import into it. Outbound HTTP exists only as a Spin host function, and Spin checks every call against `allowed_outbound_hosts` before any socket is opened. An empty list means the guest has no way to reach the network: not "blocked by a firewall" but "the capability was never granted". The same model is per-resource and fine-grained: you can grant one exact host (`https://api.example.com:443`) and nothing else.
Docker `--network none` is enforced by the **kernel**: the container gets its own network namespace with only `lo`. The process still has the full syscall interface (it can create sockets, use Unix sockets on mounted paths, etc.), it just has no route out. It is all-or-nothing (no per-host allow-list without extra tooling), and its strength depends on the shared kernel having no bugs. Wasm: deny by default, enforced by the runtime, granular per destination. Docker: allow by default, restricted by kernel isolation.

**d) TinyGo gaps vs upstream Go.**
- **Time zones:** `time.LoadLocation` needs zoneinfo files; inside the sandbox there are none. Fix: a fixed offset (what I did) or `import _ "time/tzdata"` (adds ~450 KB).
- **Reflection:** `reflect` is partial, so `encoding/json` on `map[string]any`, interfaces or complex structs can fail or be very slow; `text/template` / `html/template` are similarly limited. I build the JSON with `fmt`.
- **Concurrency:** goroutines are cooperative on a single thread; no parallelism, `GOMAXPROCS` is meaningless.
- **Networking/OS:** no `net` sockets on wasip1, no `os/exec`, no `os/signal`, limited `os/user`; `net/http` exists only as types (the SDK bridges the request/response).
- **Memory:** `-gc=leaking` (used by the Spin template) never frees memory. That is fine only because Spin gives every request a fresh instance.
- **The runtime itself keeps changing:** in TinyGo 0.42+ the new defer/recover implementation broke this SDK in `c-shared` mode (see "Version notes"); 0.41.1 works. The toolchain must be pinned like any other dependency.

---

## Task 2 — Performance comparison vs Lab 6

Method:
- **Lab 6 image:** `qn-lab6:run1`, built from my `feature/lab6` Dockerfile (distroless, non-root), already present locally (no pull during measurements).
- **Warm latency:** both servers running, `hyperfine -N --warmup 5 --runs 50 'curl -s -o /dev/null URL'`; Spin `/time`, Docker `/health`; p50/p95 computed from the 50 raw wall-clock samples in hyperfine's JSON export.
- **Cold start:** script that records the time, starts the runtime (`spin up` / `docker run -d --rm -p 18080:8080`), polls with `curl` every ~2 ms until the first HTTP 200, then stops it; 5 samples each.

### Raw measurements

```
spin   cold 1: 48 ms        docker cold 1: 186 ms
spin   cold 2: 61 ms        docker cold 2: 139 ms
spin   cold 3: 54 ms        docker cold 3: 182 ms
spin   cold 4: 78 ms        docker cold 4: 179 ms
spin   cold 5: 74 ms        docker cold 5: 167 ms

Benchmark 1: curl -s -o /dev/null http://127.0.0.1:3000/time (50 runs)     Wall Time [ms]  6.3 ± 0.8   (5.0 … 7.8)
Benchmark 2: curl -s -o /dev/null http://127.0.0.1:18080/health (50 runs)  Wall Time [ms]  5.9 ± 0.3   (5.6 … 6.7)
```

### Results table

| Dimension | Lab 6 Docker | Lab 12 WASM/Spin |
|---|---:|---:|
| Artifact size | 21.5 MB on disk (5.22 MB compressed content) | **313,326 B = 306 KB** (`main.wasm`), ~70× smaller |
| Cold start (p50 of 5) | 179 ms (139–186) | **61 ms** (48–78), ~3× faster |
| Warm latency p50 | **5.92 ms** | 6.06 ms |
| Warm latency p95 | **6.41 ms** | 7.71 ms |

**Reading the warm numbers:** each sample includes starting a `curl` process (~5 ms on its own), so both columns are dominated by the client; the real server-side difference is a fraction of a millisecond. Docker is slightly faster and steadier: a long-lived Go process with its goroutines and heap already warm answers from memory, while Spin creates a **fresh Wasm instance for every request** (instantiate, run `_initialize`, copy the request in and the response out). That is the price of per-request isolation. Docker Desktop's port forwarding goes through its VM networking and still came out ahead here; the difference is inside the noise of the curl overhead.

### Design questions

**e) What dominates cold start.**
- **Docker (139–186 ms):** the image was already local, so no pull/extraction. The cost is the container lifecycle: Docker CLI → API on Docker Desktop's Linux VM → containerd → `runc` creating namespaces (pid, mnt, net, uts, ipc), cgroups, mounting the overlayfs rootfs, setting up the veth/bridge network and the port-forward from macOS into the VM, then starting the Go binary and binding `:8080`. Only the last few ms are QuickNotes itself. On a first run from an empty cache, pulling and unpacking layers would add seconds.
- **Spin (48–78 ms):** one native process. Parse `spin.toml`, load `main.wasm`, compile/load the module with wasmtime's Cranelift (or fetch the precompiled code from the cache), set up the instance pool and bind the HTTP listener. No kernel isolation objects are created. After that, each request only pays instantiation (microseconds), not a process start.

**f) Where WASM wins and where Docker is still better** (Reading 12 trade-offs).
WASM fits: request/response functions and serverless/edge handlers that scale to zero (ms cold start, KB artifacts); plugin systems and multi-tenant code from untrusted authors (strong sandbox, capability grants); very dense hosting (thousands of instances per host); portable artefacts (same `.wasm` on arm64 and x86_64).
Docker stays better for: existing apps and full language runtimes (stock Go, Java, Python with C extensions) without porting; anything needing threads, raw sockets, long-lived connections, background workers, or a full filesystem; stateful services and databases; mature tooling (debuggers, profilers, observability, Kubernetes ecosystem). Example from this lab: all of QuickNotes (file storage, signal handling, graceful shutdown) would not fit TinyGo + WASI comfortably; one endpoint did.

**g) Which attack the WASM sandbox makes harder.**
**Container escape through the shared kernel.** A container is a normal Linux process with ~350 syscalls available; one kernel bug reachable from inside (e.g. Dirty Pipe CVE-2022-0847, or runtime bugs like runc CVE-2019-5736 / CVE-2024-21626 "Leaky Vessels") lets a tenant break out and read other tenants' data or take the host. A Wasm guest **cannot issue syscalls at all**: its only way out is the small list of host functions it was given, every memory access is bounds-checked inside its own linear memory, and code cannot jump to arbitrary addresses (structured control flow, separate call stack). So the attack surface shrinks from "the whole kernel" to "a few audited host functions". Bonus effect: with `allowed_outbound_hosts = []` a compromised guest also cannot do SSRF or exfiltrate data over the network.

---

## Bonus — Two WASM execution models

### `wasm-cli/main.go` (no Spin SDK, CGI-style)

```go
// CGI-style WASI CLI: request comes from env vars, response goes to stdout.
package main

import (
	"fmt"
	"os"
	"time"
)

// Same logic as the Spin component: fixed UTC+3, hand-built JSON.
var msk = time.FixedZone("MSK", 3*60*60)

func main() {
	method, path := os.Getenv("REQUEST_METHOD"), os.Getenv("PATH_INFO")
	if method != "GET" || path != "/time" {
		fmt.Print("Status: 404 Not Found\r\nContent-Type: application/json\r\n\r\n")
		fmt.Printf(`{"error":"no route","method":%q,"path":%q}`+"\n", method, path)
		return
	}
	now := time.Now().In(msk)
	fmt.Print("Content-Type: application/json\r\n\r\n")
	fmt.Printf(`{"unix":%d,"iso":%q,"hour_minute":%q,"timezone":"Europe/Moscow","utc_offset":"+03:00"}`+"\n",
		now.Unix(), now.Format(time.RFC3339), now.Format("15:04"))
}
```

### Build and run commands

| | Spin component (Task 1) | WASI CLI (Bonus) |
|---|---|---|
| Build | `spin build` → `tinygo build -target=wasip1 -gc=leaking -buildmode=c-shared -no-debug -o main.wasm .` | `tinygo build -target=wasi -no-debug -o main.wasm .` |
| Run | `spin up --listen 127.0.0.1:3000`, then `curl /time` | `wasmtime run --env REQUEST_METHOD=GET --env PATH_INFO=/time main.wasm` |

```
$ wasmtime run --env REQUEST_METHOD=GET --env PATH_INFO=/time main.wasm
Content-Type: application/json

{"unix":1792070468,"iso":"2026-10-15T16:21:08+03:00","hour_minute":"16:21","timezone":"Europe/Moscow","utc_offset":"+03:00"}

$ wasmtime run --env REQUEST_METHOD=POST --env PATH_INFO=/time main.wasm
Status: 404 Not Found
Content-Type: application/json

{"error":"no route","method":"POST","path":"/time"}

$ wasmtime run ../wasm/main.wasm; echo "exit=$?"      # the Spin component under bare wasmtime
exit=0                                                 # nothing printed, handler never called

$ hyperfine -N --warmup 3 --runs 30 'wasmtime run --env … main.wasm' 'wasmtime run -C cache=n --env … main.wasm'
Benchmark 1 (compile cache on):   Wall Time [ms]   4.2 ± 0.2   (4.1 … 4.7)    Memory 10.2 MiB
Benchmark 2 (cache=n):            Wall Time [ms]  29.8 ± 1.9  (27.5 … 33.7)   Memory 42.7 MiB   (7.0x slower)
```

### Comparison

| | Spin component (persistent server) | WASI CLI under `wasmtime run` (per invocation) |
|---|---:|---:|
| Module size | 313,326 B | **196,007 B** (no SDK, no `net/http`/`textproto`) |
| One-time startup | 61 ms p50 (`spin up` until first 200) | none |
| Cost per request/invocation | ~6 ms incl. curl process (fresh instance per request, sub-ms server side) | **4.2 ms** (new wasmtime process, cached machine code) |
| Per invocation without compile cache | n/a (compiled once at start) | 29.8 ms (Cranelift compiles every time) |
| Network listener | yes, Spin's HTTP server | no, input via env vars, output via stdout |

Takeaway: per-invocation cost is ~4 ms only because wasmtime caches the compiled code on disk; without the cache, every request would pay ~30 ms of compilation. Spin pays startup and compilation once and then amortises it across all requests.

### Design questions

**h) Why the Task 1 component cannot run under bare `wasmtime run`.**
`wasmtime run` runs a **command**: it instantiates the module and calls `_start` (or `wasi:cli/run` for a component). The Spin module is a **reactor**: it exports `_initialize` and the handler `handle-http-request`, but no `_start`. What I observed: wasmtime ran `_initialize`, found nothing else to start, and exited with **code 0 and no output**. The handler expects a request record passed in by a host that speaks Spin's HTTP interface; nobody ever calls it. It would also not work with `wasmtime serve`, because that expects a component exporting `wasi:http/incoming-handler`, while this TinyGo build is a core module with Spin's legacy export that Spin itself adapts.

**i) What Spin adds on top of wasmtime.**
- **HTTP server loop** (hyper) that accepts connections and turns each request into a guest call, and maps the response back.
- **Manifest and routing:** `spin.toml` defines components, routes (`/time` → `qn-time`), files, env and variables; one app can have many components on many routes.
- **Instance pooling and precompiled code:** the module is compiled once at startup; each request gets a fresh, isolated instance from a pre-allocated pool in microseconds, so no state leaks between requests and `-gc=leaking` is safe.
- **Interface adaptation:** it runs both modern wasi-http components and legacy core modules like ours (`fermyon:spin/inbound-http`).
- **Capability policy:** `allowed_outbound_hosts`, plus host APIs for key-value, SQLite, variables and secrets, each granted per component.
- **Developer workflow:** `spin new` templates, `spin build`, `spin up`, stdio logs in `.spin/logs/`, `spin deploy`/registry push.

**j) Workloads for each model.**
- **Per-invocation (CGI-like):** short, infrequent, self-contained jobs where an idle process would be wasted, e.g. a nightly cron report, a CI step or git hook, or a user-supplied plugin that transforms one file in a data pipeline (one `wasmtime run` per file, nothing left running).
- **Spin persistent server:** an HTTP API with steady traffic, like this `/time` endpoint or a webhook receiver: startup and compilation are paid once, then every request costs only an instantiation while still being isolated from the others.
