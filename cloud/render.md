# Render deployment — QuickNotes (Lab 10, Option A)

| Setting | Value |
|---|---|
| Service | `quicknotes-lab10` (Web Service, ID `srv-db3vglijnfac73b00si0`) |
| Public URL | https://quicknotes-lab10-qodj.onrender.com |
| Instance type | **Free** (no card was required) |
| Region | Frankfurt (EU Central) |
| Source | **Existing image** `ghcr.io/illmmmiira/devops-intro/quicknotes:<tag>` (public package, no registry credentials) |
| First deployed tag | `v0.1.0` (digest `sha256:4fb81b24697c549ff967addff49e296fa36082722fceb1a81ce631ac4a786ad4`) |
| Environment | `PORT=8080`, `ADDR=:8080` |
| Health check path | `/health` |
| Auto-deploy | Off (image-backed service); deploys come from the CI deploy hook |
| Deploy hook | Stored only as GitHub Actions secret `RENDER_DEPLOY_HOOK` — never in the repo |

## Why an existing image instead of building from the repo

The image Render runs is the exact artifact the release workflow built, pushed and
(in Lab 9) scanned with Trivy — one build, one digest. Render building from the repo
would produce a second, different image that nobody scanned, and every deploy would
pay the build time again.

## Why both `PORT` and `ADDR`

Render routes traffic to `$PORT` (default `10000`). QuickNotes listens on `$ADDR`
(default `:8080`) and doesn't read `PORT`. Setting both to 8080 makes them agree on
first boot, so there is no `New primary port detected … Restarting deploy` cycle —
and no QuickNotes code change.

Log evidence (first deploy, `v0.1.0`):

```
==> Starting service...
quicknotes listening on :8080 (notes loaded: 4)
==> Your service is live 🎉
==> Available at your primary URL https://quicknotes-lab10-qodj.onrender.com
```

## Deploy from CI

`.github/workflows/release.yml`, last step — runs only when the secret exists:

```yaml
      - name: Trigger Render deploy
        if: env.RENDER_DEPLOY_HOOK != ''
        run: |
          IMG=$(printf '%s' "${IMAGE}:${GITHUB_REF_NAME}" | jq -sRr @uri)
          curl -fsS -X POST "${RENDER_DEPLOY_HOOK}&imgURL=${IMG}" -o /dev/null -w 'render deploy hook: HTTP %{http_code}\n'
```

`imgURL` selects the tag that was just pushed (URL-encoded, only the tag differs from
the service's image). Pushing tag `v0.1.1` → `render deploy hook: HTTP 200` → Render
deploy `v0.1.1`, trigger **Deploy Hook**, 12.0 s, Live.

## Deploy history

| Deploy | Image | Trigger | Duration |
|---|---|---|---|
| 1 | `v0.1.0` (`4fb81b2`) | First Deploy | 21.5 s |
| 2 | `v0.1.0` (`4fb81b2`) | Deploy Hook (manual hook test) | 12.1 s |
| 3 | `v0.1.1` (`55f8c37`) | Deploy Hook from CI (release run #2) | 12.0 s |
