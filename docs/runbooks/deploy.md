# Runbook — Deploy

Single binary + systemd, no containers. One host runs three units: the HTTP API, the scheduler and
the queue worker.

## Layout

```
/opt/ims/ims-go-api/
  releases/<timestamp>-<version>/ims     # immutable release binaries
  current -> releases/<...>              # atomic symlink, what units run
/etc/ims/ims-go-api/.env                 # environment (root:ims, 0640)
/var/lib/ims/ims-go-api/                 # writable state (uploads/exports)
/var/log/ims/ims-go-api/                 # optional file logs (else journald)
```

Nothing writable lives inside a release directory. Logs go to journald unless `APP_LOG_DIR` is set.

## First install (fresh host)

```bash
sudo deploy/scripts/install.sh          # user, dirs, systemd units, seeds .env
sudoedit /etc/ims/ims-go-api/.env       # DATABASE_DSN, APP_URL, APP_TIMEZONE, ...
sudo deploy/scripts/release.sh --source /srv/src/ims-go-api-starter
```

`install.sh` creates the `ims` user, the directory layout, installs and enables the three units,
and copies `.env.example` to `/etc/ims/ims-go-api/.env` for review.

## Upgrade

```bash
# from a local checkout (build-on-server):
sudo deploy/scripts/release.sh --source /srv/src/ims-go-api-starter

# from git (clones the ref on the host):
sudo deploy/scripts/release.sh --repo git@github.com:your-org/ims-go-api-starter.git --ref v1.4.0
```

The script: builds a static binary → runs `ims migrate` (gated) → atomically switches `current` →
restarts units → health-checks `/up` → prunes old releases (keep `--keep`, default 5).

Useful flags: `--dry-run` (print the plan, validate prerequisites), `--skip-migrate`,
`--no-restart`, `--health-url`, `--keep`.

## Units

| Unit | Command |
|---|---|
| `ims-go-api` | `ims serve` |
| `ims-go-api-scheduler` | `ims schedule` |
| `ims-go-api-worker` | `ims queue:work --queues=critical,default,heavy` |

Exactly **one** scheduler must run per deployment. The queue worker may be scaled (add more
`ims-go-api-worker@.service` instances or run `ims queue:work` elsewhere).

```bash
systemctl status ims-go-api ims-go-api-scheduler ims-go-api-worker
journalctl -u ims-go-api -f
```

## Reverse proxy & client IPs

If you terminate TLS at nginx/Caddy, set the proxy's address so forwarded client IPs are honoured:

```dotenv
HTTP_TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8
```

Only requests whose immediate peer is in this list have `X-Forwarded-For` / `X-Real-IP` trusted;
everything else uses the socket address. This drives login throttling, rate limiting and logs.

## Troubleshooting

- Health: `curl -fsS http://127.0.0.1:8080/api/health?fresh=1 | jq`.
- Config echo: `sudo -u ims /opt/ims/ims-go-api/current/ims config:show` (DSN password masked).
- Contract check: `ims openapi:check` (needs the DB up).
- A release that fails its health check is not marked good — roll back (see `rollback.md`).
