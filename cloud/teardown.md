# Teardown — Lab 10

Everything in this lab is free and needs no card, but nothing should be left running by accident.

## Cloudflare quick tunnel (bonus)

1. `Ctrl+C` in the terminal running `cloudflared tunnel --url http://localhost:8080`.
   The `*.trycloudflare.com` URL dies with the process — there is nothing to delete on Cloudflare's side.
2. Stop the local container: `docker rm -f qn-local`

## Render service

Free hours cost nothing and the service sleeps after 15 min idle, so leaving it is harmless. To remove it:

1. Dashboard → `quicknotes-lab10` → **Settings**.
2. Either **Suspend Web Service** (keeps config, stops serving) or **Delete Web Service** (type the name to confirm).
3. Delete the GitHub secret `RENDER_DEPLOY_HOOK` (repo → Settings → Secrets and variables → Actions),
   otherwise the next `v*` tag will call a hook for a service that no longer exists and fail the release run.
   (Alternatively regenerate the hook in Render — the old URL stops working.)

## ghcr.io package

Keep it — it's the release artifact. To remove: GitHub profile → Packages → `devops-intro/quicknotes`
→ Package settings → **Delete this package**. Old tags can also be deleted per version there.

## Git tags

`v0.1.0` and `v0.1.1` stay — they are the releases. (Deleting would be
`git push origin :refs/tags/v0.1.x` + `git tag -d v0.1.x`.)
