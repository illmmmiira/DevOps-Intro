# Lab 4 — OS & Networking Diagnostics

**Environment:** macOS (MacBook Air), loopback interface `lo0`. Linux-specific tools from the original lab doc (`ss`, `journalctl`, `iptables`) substituted with macOS equivalents (`lsof`, direct process log, `pfctl`) — noted inline below.

## Task 1 — Trace a Request End-to-End (6 pts)

### 1.1–1.2 Capture

```
sudo tcpdump -i lo0 -nn -s 0 -A 'tcp port 8080' -w lab4-trace.pcap
```

One request sent while capturing:
```
curl -v -X POST http://localhost:8080/notes -H 'Content-Type: application/json' -d '{"title":"trace me","body":"in flight"}'
```

Result: **12 packets captured, 0 dropped by kernel**. Full text dump: `lab4-trace.txt` (included alongside this file).

Note: curl resolved `localhost` to the IPv6 loopback address `::1` first, so the whole exchange below is over IPv6 (`IP6 ::1.51341 > ::1.8080`) rather than IPv4 `127.0.0.1` — same protocol behavior, just a different address family.

### 1.3 Annotated capture

**1) TCP three-way handshake**
```
01:01:54.571176 IP6 ::1.51341 > ::1.8080: Flags [S]   seq 513484718 ...   # SYN       (client -> server)
01:01:54.571339 IP6 ::1.8080 > ::1.51341: Flags [S.]  seq 3908970976 ...  # SYN, ACK  (server -> client)
01:01:54.571405 IP6 ::1.51341 > ::1.8080: Flags [.]   ack 1 ...           # ACK       (client -> server) -- handshake complete
```

**2) HTTP request** (client -> server, `Flags [P.]`, seq 1:175)
```
POST /notes HTTP/1.1
Host: localhost:8080
User-Agent: curl/8.7.1
Accept: */*
Content-Type: application/json
Content-Length: 39

{"title":"trace me","body":"in flight"}
```

**3) HTTP response** (server -> client, `Flags [P.]`, seq 1:204)
```
HTTP/1.1 201 Created
Content-Type: application/json
Date: Thu, 17 Sep 2026 22:01:54 GMT
Content-Length: 90

{"id":6,"title":"trace me","body":"in flight","created_at":"2026-09-17T22:01:54.571918Z"}
```

**4) Connection close** — active close, both sides send `FIN` (not `RST`)
```
01:01:54.572767 IP6 ::1.51341 > ::1.8080: Flags [F.]  seq 175 ...   # client sends FIN
01:01:54.572848 IP6 ::1.8080 > ::1.51341: Flags [.]   ack 176 ...  # server ACKs client's FIN
01:01:54.572888 IP6 ::1.8080 > ::1.51341: Flags [F.]  seq 204 ...  # server sends its own FIN
01:01:54.573004 IP6 ::1.51341 > ::1.8080: Flags [.]   ack 205 ...  # client ACKs server's FIN -- fully closed
```

The remaining plain `ACK`-only packets (between handshake/request and response/close) are ordinary segment acknowledgements, not separate protocol events.

### 1.4 Five diagnostic commands

macOS substitutions used: `lsof -iTCP -sTCP:LISTEN` instead of `ss -tlnp` (no `ss` on macOS); `netstat -rn` instead of `ip route show` (no `ip` on macOS); the 5th command (`journalctl`) is skipped — QuickNotes isn't running as a systemd/launchd service here, so there's nothing for it to show; the direct `go run .` terminal output is the equivalent.

**1) What's listening?**
```
$ lsof -iTCP -sTCP:LISTEN -n -P | grep 8080
quicknote 3872 ilmira    5u  IPv6 0x8907560bbba1513a      0t0  TCP *:8080 (LISTEN)
```
QuickNotes (PID 3872) is listening on `*:8080` over IPv6 — consistent with the capture, which showed the connection going over `::1`.

**2) Routing table**
```
$ netstat -rn
```
Default IPv4 route via `192.168.1.1` on `en0`; loopback (`127.0.0.1`, `::1`) routed via `lo0`. Full table saved alongside this doc / available in terminal scrollback if needed — the key line for this lab is that `127.0.0.1`/`::1` traffic never leaves the host (`lo0`, no gateway).

**3) Reachability (mtr, loop on lo0)**
```
$ sudo mtr -rwc 5 localhost
Start: 2026-09-18T01:07:30+0300
HOST: MacBook-Air-Ilmira.local Loss%   Snt   Last   Avg  Best  Wrst StDev
  1.|-- localhost                 0.0%     5    0.3   0.2   0.2   0.3   0.1
```
Single hop (loopback), 0% loss, ~0.2ms average — as expected for `localhost`, since the packet never touches a physical network interface.
(Note: `mtr` needed `sudo` on macOS — it opens raw ICMP sockets, which require root privileges here, same reason `tcpdump` does.)

**4) DNS works**
```
$ dig +short example.com @1.1.1.1
8.6.112.0
8.47.69.0
```
Querying Cloudflare's `1.1.1.1` resolver directly (bypassing the local resolver) confirms DNS resolution works end-to-end from this host.

**5) Logs**
No systemd/`journalctl` on macOS. QuickNotes was run directly (`go run .`); its own stdout in that terminal (`quicknotes listening on :8080 (notes loaded: N)` etc.) is the log — the macOS equivalent here for a foreground-run process.

### 1.5 502 reflection

A 502 means the layer in front of the app (reverse proxy / load balancer) accepted the client's connection just fine, but couldn't get a valid response from the thing behind it — so the first thing to check is whether QuickNotes itself is actually alive and listening (`ps -ef | grep quicknotes`, `lsof -iTCP -sTCP:LISTEN | grep 8080`), not the network path to it. If the process is up, the next step is hitting it directly with `curl localhost:8080/health`, bypassing whatever proxy sits in front — if that also fails, the problem is inside the app (crash, deadlock, out of memory); if it succeeds, the problem is between the proxy and the app (wrong upstream port, proxy timeout, or the process restarting mid-request). Only after ruling out "is the process even there and responding" would I move on to logs and lower-level checks (DNS, firewall/pf rules) — most 502s in practice turn out to be a dead or restarting upstream, not a networking issue.

---

## Task 2 — Outside-In Debugging on a Broken Deploy (4 pts)

### 2.1 Reproduce the port conflict

```bash
ADDR=:8080 go run . &
PID1=$!
sleep 1
ADDR=:8080 go run . 2>&1 | tee /tmp/qn-broken.log &
PID2=$!
sleep 2
ps -ef | grep "go run" | grep -v grep
```

Output (trimmed):
```
2026/09/18 01:10:20 quicknotes listening on :8080 (notes loaded: 6)
2026/09/18 01:10:20 quicknotes listening on :8080 (notes loaded: 6)
2026/09/18 01:10:20 listen: listen tcp :8080: bind: address already in use
exit status 1
```

The second process fails fast with `bind: address already in use` and exits — it does not hang. Worth noting: QuickNotes logs `"listening on :8080"` from a goroutine *before* `ListenAndServe()` actually confirms the bind succeeded, so both processes print that line even though only one of them is really listening — a log-based health check watching for that string alone would be misled.

### 2.2 Outside-in chain

| Step | Command | Output | Decision |
|---|---|---|---|
| 1. Is it running? | `ps -ef \| grep quicknotes` | compiled binary (child of the surviving `go run`) present and running | process is alive → move to network layer |
| 2. Is it listening? | `lsof -iTCP -sTCP:LISTEN -n -P \| grep 8080` | `quicknote ... TCP *:8080 (LISTEN)` | socket is bound and listening → move to reachability |
| 3. Reachable from host? | `curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/health` | `200` | app answers correctly end-to-end → rule out app-level failure |
| 4. Firewall blocking? | `sudo pfctl -s rules` | only default Apple `com.apple/*` anchors, nothing custom | `pf` not the cause → rule out |
| 5. DNS? | `dig +short localhost` | `127.0.0.1` | name resolution fine → rule out |

All five checks came back clean for the *surviving* process — confirming the failure was isolated to the second process's startup (the bind call itself), not a downstream networking/DNS/firewall issue.

### 2.3 Repair + re-verify (two attempts — the second one is the real finding)

**Attempt 1 (didn't work):** killed the `go run` wrapper PID directly —
```bash
kill 4501
ADDR=:8080 go run . &     # PID 4541
curl -s http://localhost:8080/health   # -> {"notes":6,"status":"ok"}
```
The new `go run .` *still* failed with `bind: address already in use`, yet `curl` returned a healthy response. Root cause of *that*: `go run` forks a wrapper process that execs a separate compiled binary under a different PID; killing the wrapper (`4501`) does not kill the actual binary (`4506`), which kept holding the port and kept answering `curl` the whole time.

**Attempt 2 (correct):** found the real listener with `lsof`, killed *that* PID —
```bash
lsof -iTCP -sTCP:LISTEN -n -P | grep 8080   # -> PID 4506
kill 4506
ADDR=:8080 go run . &                        # PID 4558
curl -s http://localhost:8080/health         # -> {"notes":6,"status":"ok"}
```
This time the log showed a clean `shutting down` from the old process's signal handler, the new instance started, and health-checked green.

### 2.4 Root cause & postmortem

**Root cause:** `bind: address already in use` — two QuickNotes instances were started against the same port with no check that it was already taken.

**Blameless mini-postmortem (≤200 words):**

On this host, two independent QuickNotes instances were started against the same port (`:8080`) with no check for availability. The second process's `ListenAndServe` failed immediately with `bind: address already in use` and exited — a safe, fail-fast failure mode. But the log line announcing `"listening on :8080"` is printed *before* the bind is confirmed, so a log-based health check would have reported a false positive for the process that actually crashed.

A second, subtler issue surfaced during recovery: `go run` starts a wrapper process that execs a separate compiled binary under its own PID. Killing the wrapper's PID does not terminate the child binary, which kept holding the port. This isn't specific to this bug — any process supervision that assumes "the PID I started is the PID doing the work" can leak resources this way.

Neither failure is really an individual mistake — both are what happens without automated guardrails. Prevention belongs in tooling, not vigilance: a startup preflight check that fails loudly if the target port is already bound, a real process supervisor (systemd/launchd) that tracks actual PIDs instead of shell job numbers, and a log message that only claims "listening" once `Listen()` has actually succeeded.

---

## Bonus — TLS Handshake (2 pts)

### B.1 HTTPS via Caddy

```bash
brew install caddy
cat > Caddyfile <<'"'"'EOF'"'"'
localhost:8443 {
  reverse_proxy localhost:8080
}
EOF
caddy run --config Caddyfile
```
Caddy auto-provisioned a local CA (`Caddy Local Authority`) and a leaf cert for `localhost`, installed the root in the macOS keychain, and started serving `:8443` -> `localhost:8080`.

### B.2 Capture

```bash
sudo tcpdump -i lo0 -nn -s 0 -w lab4-tls.pcap 'tcp port 8443' &
curl -vk https://localhost:8443/health
sudo kill $TCPDUMP_PID; wait $TCPDUMP_PID 2>/dev/null
```
Result: **35 packets captured, 0 dropped**. Saved as `lab4-tls.pcap`.

### B.3 Decoding the handshake

_Note: captured with tcpdump/curl/openssl rather than the Wireshark GUI (no GUI access from this environment) — the same handshake fields Wireshark would show are below from `curl -v` and `openssl s_client`._

**ClientHello / ServerHello (from `curl -vk`):**
```
* ALPN: curl offers h2,http/1.1
* (304) (OUT), TLS handshake, Client hello (1):
* (304) (IN), TLS handshake, Server hello (2):
* (304) (IN), TLS handshake, Certificate (11):
* (304) (IN), TLS handshake, CERT verify (15):
* (304) (IN), TLS handshake, Finished (20):
* SSL connection using TLSv1.3 / AEAD-CHACHA20-POLY1305-SHA256
* ALPN: server accepted h2
```
- ClientHello offered ALPN `h2, http/1.1` (no HTTP/1.0-only fallback, no TLS 1.0/1.1 offered at all — see B.4)
- ServerHello: TLS 1.3, chose cipher `AEAD-CHACHA20-POLY1305-SHA256`, ALPN `h2`

**Certificate chain (`openssl s_client -connect localhost:8443 -servername localhost -alpn h2,http/1.1 -showcerts`):**
```
0 s: (no CN — SAN: DNS:localhost)   i: CN=Caddy Local Authority - ECC Intermediate
1 s: CN=Caddy Local Authority - ECC Intermediate   i: CN=Caddy Local Authority - 2026 ECC Root
Negotiated TLS1.3 group: X25519MLKEM768
Cipher: TLS_AES_128_GCM_SHA256
Verify return code: 20 (unable to get local issuer certificate)
```
`Verify return code: 20` is expected here, not a bug: Caddy's local root CA was installed into the macOS Keychain (which `curl` on this system trusts), but not into OpenSSL's own default trust store, so `openssl s_client` (no `-k` equivalent by default) reports it as unverified while `curl` accepted it.

Notable: the negotiated key-exchange group was `X25519MLKEM768` — a hybrid post-quantum key exchange (classical X25519 + ML-KEM768), now the default in current OpenSSL/TLS 1.3 stacks rather than an opt-in experiment.

### B.4 Which step kills TLS 1.0/1.1 in 2026?

It's not a rejection at the ServerHello — it's earlier than that. The **ClientHello's `supported_versions` extension** on any current client (curl/OpenSSL here) simply never lists TLS 1.0 or 1.1 as offers in the first place, so the server never gets the chance to negotiate down to them. TLS 1.0/1.1 are not being actively "blocked" per request in 2026 so much as no longer *offered* by any modern client stack by default — the deprecation happened at the client-library level, years before any given server enforces a minimum version.
