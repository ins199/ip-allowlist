# ip-allowlist — Universal IP Allowlist Manager

> Deploy to any Linux host and go. Manage IP allowlists (iptables) for **multiple ports** via a web UI.
> Prevents external IPs from reaching your servers/databases with leaked keys, and lets you self-serve IP changes when your address drifts — no cloud console needed.

**Languages**: [English](README.en.md) | [中文](README.md)

---

## Why

- SSH/database ports are usually open to the public internet, protected only by keys — **a leaked key means a naked server**.
- A fixed-IP allowlist locks you out when your home broadband IP drifts, and you have to edit security groups each time.
- Business systems running in Docker containers **cannot touch the host's iptables**, so this capability can't be embedded there.

This system runs as an **independent host process** (root), naturally able to manage the host's iptables. Deploy to any ECS and it works, with no dependency on any business project.

---

## Features

| Feature | Description |
|---------|-------------|
| Multi-port allowlist | Each rule = {port, IP/CIDR, remark}; manage SSH(22), Redis(6379), webhook(9000), etc. |
| Web UI | Login with username/password; view/add/remove allowlist entries; shows your current source IP |
| Auth | bcrypt-hashed passwords + stateless JWT; supports password change |
| Remember me | Keep session for 30 days (`remember_days`), no re-login on refresh |
| Server overview | Real-time CPU/memory/disk/load/uptime/listening ports/recent logins |
| Mobile friendly | Responsive layout, works in phone browsers |
| Strict mode | Per-port option "only allowlisted IPs can connect"; non-allowlisted DROP. Loose mode only shows, doesn't block |
| Anti-lockout | Auto-adds current IP, blocks deleting current IP, blocks emptying allowlist in strict mode, ACCEPT before DROP |
| Persistence | JSON on disk + systemd auto-start + auto-restore iptables rules on boot |
| Idempotent sync | Reconcile on startup/manual trigger aligns iptables with config |
| Dry-run | `IPAW_DRY_RUN=1` prints commands without executing — safe local testing |

---

## Architecture

```
┌─────────────────────────────────────────────┐
│  ip-allowlist (host, systemd, root)         │
│  ├─ Web UI (login + allowlist + overview)   │
│  ├─ API (login/rules/rule/ip/strict/sync/   │
│  │        me/change-password/server-info)    │
│  ├─ iptables manager (rebuild, anti-lockout)│
│  ├─ sysinfo collector (CPU/mem/disk/load)   │
│  └─ persistence (JSON file)                 │
└──────────────────────┬──────────────────────┘
                       │ operates iptables directly
                       ▼
             host firewall (iptables)
```

### Rule structure

One dedicated chain `IPAW-<port>` per managed port:

```
# Chain IPAW-22 (allowlisted IPs ACCEPT, others RETURN to INPUT → fail2ban)
-A IPAW-22 -s 1.2.3.4/32 -j ACCEPT
-A IPAW-22 -s 5.6.7.8/32 -j ACCEPT
-A IPAW-22 -j RETURN

# INPUT chain
-I INPUT -p tcp --dport 22 -j IPAW-22      # before fail2ban
-A INPUT -p tcp --dport 22 -j DROP         # strict mode only, only when allowlist non-empty
```

---

## Quick Deploy

> **Zero-dependency install**: no Go, no Docker, no git, no toolchain — only Linux's built-in `iptables`. The web frontend is embedded into the binary via `go:embed`, so deployment is a single file. The install script auto-detects CPU arch (amd64/arm64), compiles locally if Go is present, or downloads the prebuilt binary from GitHub Release.

### Option 1: One-line curl install (recommended, no clone)

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ins199/ip-allowlist/master/deploy.sh)" 10443 MyPass123
```

Pulls the installer → compiles or downloads the binary (arch auto-selected) → installs systemd → starts. No clone, no Go/Docker/git.

### Option 2: Clone then deploy

```bash
git clone https://github.com/ins199/ip-allowlist.git
cd ip-allowlist
sudo bash deploy.sh [port] [admin-password] [optional-domain]
```

**How the binary is sourced** (auto-selected):
1. Server has Go → compile locally, ready to use
2. No Go → download prebuilt binary (amd64/arm64) from GitHub Release, no git needed
3. Download fails (e.g., GitHub unreachable) → compile locally and upload: `GOOS=linux GOARCH=amd64 go build -o deploy/ip-allowlist .`, put it in `deploy/`, re-run `deploy.sh`

> ⚠️ **Change the default password immediately** after deploy (default `changeme`).
> ⚠️ Default port `10443` (uncommon, less scanned); change via args or config.yaml.
> Strongly recommend a domain + nginx reverse proxy + HTTPS (see "Domain & HTTPS").

### Auto-start & rule recovery

systemd auto-start is enabled during install. On startup `main.go` runs a `Reconcile` to restore the allowlist config as iptables rules. No extra script needed.

### Service management (systemd)

```bash
systemctl status ip-allowlist      # status
systemctl restart ip-allowlist     # restart
systemctl stop ip-allowlist        # stop (iptables rules are NOT removed on stop)
journalctl -u ip-allowlist -f      # live logs
journalctl -u ip-allowlist -n 50   # recent logs
systemctl is-enabled ip-allowlist  # should output enabled
vi /opt/ip-allowlist/config.yaml   # edit config, then restart
systemctl restart ip-allowlist
```

**Crash recovery**: `Restart=always` — systemd restarts the process 3s after a crash; rules auto-recover. Auto-start on boot too.

### Release (maintainers)

Tagging triggers GitHub Actions to cross-compile linux/amd64 + arm64, **with the version injected from the tag automatically** (no code change needed):

```bash
git tag v1.0.1
git push origin v1.0.1
```

Actions automatically:
1. Cross-compiles amd64 + arm64, injects version via `-ldflags`
2. Generates `SHA256SUMS` checksums (Release + OSS mirror)
3. Publishes GitHub Release + syncs to Aliyun OSS (when `OSS_*` secrets configured)

> CI also runs **Leak Check**: scans for internal info on push/PR (sensitive words live in GitHub Secret `LEAK_WORDS`, not in the public repo). Fails on hit, preventing leaks into the open-source repo.

Pin a version with `IPAW_VERSION`:

```bash
sudo IPAW_VERSION=v1.0.1 bash -c "$(curl -fsSL https://raw.githubusercontent.com/ins199/ip-allowlist/master/deploy.sh)" 10443 MyPass123
```

> `IPAW_VERSION` defaults to `latest` (always pulls newest Release); pin to a specific version in production.

### Upgrade

Two ways:
1. **Script upgrade**: re-run the one-line install script (config & allowlist preserved in `/opt/ip-allowlist/`)
2. **In-page self-upgrade**: web UI shows current version → check for updates → one-click upgrade. The download is **SHA256-verified** (anti-tampering), binary replaced and auto-restarted; auto-rolls back on failure

### Mirror source (optional)

Domestic servers (e.g., Aliyun) often have limited/throttled access to GitHub Release assets. Use `IPAW_MIRROR` to point at a reachable mirror prefix (e.g., an Aliyun OSS public-read bucket):

```ini
# Add to [Service] section of the systemd unit
Environment=IPAW_MIRROR=https://your-bucket.oss-cn-shenzhen.aliyuncs.com/
```

Self-upgrade **tries GitHub first by default**, then falls back to the mirror on failure (10s per source).

> This repo's CI already uploads binaries to Aliyun OSS on release: configure GitHub Secrets `OSS_BUCKET` / `OSS_ENDPOINT` / `OSS_AK_ID` / `OSS_AK_SECRET` (RAM sub-user least-privilege) and it syncs automatically.

---

## Usage

### Add a port rule

1. Enter port (e.g., 22) + purpose (SSH) + strict mode toggle at the top → create.
2. Add IP/CIDR + remark below the rule card.
3. On first add, the system **auto-adds your current source IP** (anti-lockout).
4. Strict mode toggle: non-allowlisted IPs can't connect once enabled.

### Strict-mode safety design

- **Auto-add current IP**: when enabling strict mode, if your source IP isn't allowlisted, it's added and persisted (survives restart).
- **Block deleting current IP**: deleting the current source IP in strict mode is rejected.
- **Block emptying**: deleting to an empty allowlist in strict mode rolls back, requiring at least one entry.
- **ACCEPT before DROP**: rule order guarantees allowlist passes before the catch-all reject.

### UI buttons

| Button | Action |
|--------|--------|
| Add | Add IP to this port's allowlist |
| ✕ | Remove an IP |
| Sync rules | Align iptables with config (manual Reconcile) |
| Delete rule | Remove the whole port rule |
| Strict mode | Toggle strict/loose |

---

## API Reference

All endpoints require a JWT after login: browsers send the HttpOnly Cookie automatically; API clients can use `X-Auth-Token` header or `?token=` query param.

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/login` | Login, body `{username, password, remember}`, returns `{token}` |
| GET  | `/api/rules` | All port rules + current source IP + DROP status |
| POST | `/api/rule` | Create/update port rule, body `{port, comment, strict}` |
| DELETE | `/api/rule/:port` | Delete port rule |
| POST | `/api/rule/:port/ip` | Add allowlist IP, body `{ip, remark}` |
| DELETE | `/api/rule/:port/ip/:ip` | Remove allowlist IP |
| POST | `/api/rule/:port/strict` | Toggle strict mode, body `{strict}` |
| GET  | `/api/me` | Current login info (username + source IP) |
| POST | `/api/change-password` | Change password, body `{old_password, new_password}` |
| POST | `/api/logout` | Logout (clear cookie) |
| GET  | `/api/sync` | Manual sync iptables with config |
| GET  | `/api/server-info` | Server metrics (CPU/mem/disk/load/ports/recent logins) |

---

## Configuration

`/opt/ip-allowlist/config.yaml`:

```yaml
server:
  addr: "0.0.0.0:10443"                  # listen address
  data_file: "/opt/ip-allowlist/allowlist.json"  # allowlist data
auth:
  username: "admin"                      # admin account
  password: "your-password"              # admin password
  secret: "random-long-string"           # JWT signing key (must be random)
  session_hours: 24                      # session duration (hours)
  remember_days: 30                      # "remember me" session (days)
```

Allowlist data file `allowlist.json` (example):

```json
{
  "rules": [
    {
      "port": 22,
      "comment": "SSH",
      "strict": true,
      "allow_list": [
        { "ip": "203.0.113.10", "remark": "home broadband" }
      ]
    }
  ]
}
```

---

## Domain & HTTPS

Bind a domain and reverse-proxy with nginx + HTTPS (don't expose plain HTTP publicly):

```nginx
# /etc/nginx/conf.d/ip-allow.conf
server {
    listen 443 ssl;
    server_name allow.example.com;          # your domain → server public IP
    ssl_certificate     /etc/ssl/fullchain.pem;
    ssl_certificate_key /etc/ssl/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:10443;  # local listen port
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Then access `https://allow.example.com`. The service listens on 10443 behind nginx.

---

## Local Development & Testing

```bash
go build ./...

# Run in dry-run mode (prints iptables commands, does NOT modify the firewall)
IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test-allowlist.json -bind 127.0.0.1:10443

# Login test
curl -X POST http://127.0.0.1:10443/api/login -d '{"username":"admin","password":"changeme"}' -H 'Content-Type: application/json'
```

---

## Directory Layout

```
ip-allowlist/
├── main.go                 # entry
├── embed.go                # go:embed web/ frontend into binary (single file deploy)
├── config.go               # config loading
├── config.example.yaml     # sample config
├── internal/
│   ├── api/                # HTTP API + Web
│   ├── auth/               # password + JWT auth
│   ├── iptables/           # rule generation/apply/anti-lockout
│   ├── sysinfo/            # server metrics collection
│   └── store/              # JSON persistence
├── web/                    # frontend (embedded at build; not needed at runtime)
├── deploy.sh               # one-line deploy (curl remote / local)
├── deploy/                 # systemd unit
├── .github/workflows/      # release CI + leak scan
└── README.md
```

---

## Security Notes

1. **Admin password**: change immediately after deploy, never use defaults.
2. **Web port**: use nginx reverse proxy + HTTPS; don't expose plain HTTP publicly.
3. **iptables privileges**: the service runs as root — make sure the environment is trusted.
4. **Anti-lockout**: add your current source IP first, then enable strict mode.
5. **Backup**: back up `allowlist.json` regularly (contains all allowlist config).

---

## Technical Architecture

### Layered design

- `api` layer only does HTTP input/output conversion, calling `store`, `iptables`, `auth`.
- `iptables` layer doesn't care about HTTP; it only turns "given rules → generate/apply iptables commands".
- `store` layer doesn't touch the firewall; it only persists config.
- This isolation lets the `iptables` core be reused independently (e.g., future central-agent embedding).

### Data flow

```
User clicks "Add IP"
  → api.handleAddIP
  → store.AddIP (updates allowlist.json)
  → iptables.ApplyPortRule (rebuilds IPAW chain)
  → takes effect immediately
```

### Tech choices

| Component | Choice | Reason |
|-----------|--------|--------|
| Language | Go | single binary, cross-compile, concurrency-safe, zero-dependency deploy; CI produces linux/amd64 + arm64 |
| Web framework | gin | lightweight, fast, mature |
| Frontend | vanilla HTML/JS | single embedded file, no build step, mobile-friendly |
| Password hashing | bcrypt | brute-force resistant, industry standard |
| Auth | JWT (golang-jwt) | stateless, survives restart, shared secret across instances |
| Persistence | JSON file | simple, reliable, no external deps, fits config-style data |
| Firewall | iptables | Linux built-in, fine-grained, fail2ban-compatible |

---

## How It Works

### How iptables rules work

Each managed port has a dedicated chain `IPAW-<port>`. The kernel matches rules in order — **first match wins**:

```
INPUT chain (matched in order)
  1. ACCEPT  tcp dpt:<other services>    # server's existing rules
  2. IPAW-22                             # inserted by this system, before fail2ban
  3. f2b-sshd                            # fail2ban fallback
  ...
```

Inside `IPAW-22`:
```
IPAW-22
  1. -s 203.0.113.10/32 -j ACCEPT   # allowlisted → pass
  2. -s 127.0.0.1/32 -j ACCEPT      # current source → pass (auto-added)
  3. -j RETURN                      # others → back to INPUT
```

- **Loose mode**: chain has only ACCEPT + RETURN; non-allowlisted traffic returns to INPUT and hits fail2ban (log only, no block).
- **Strict mode**: `-j DROP` appended at the end of the INPUT rules; non-allowlisted traffic is dropped before fail2ban.

### Anti-lockout mechanism (core safety design)

The most important design — prevents accidentally locking yourself out of SSH forever:

| Mechanism | Implementation | When |
|-----------|----------------|------|
| Auto-add current IP | `ApplyPortRule` adds the current source IP if not allowlisted | every rule apply |
| Block delete current IP | `handleDelIP` rejects in strict mode | on delete |
| Block empty allowlist | strict-mode delete-to-empty rolls back | on delete |
| ACCEPT before DROP | rule order guarantees allowlist passes first | every chain rebuild |
| Reject empty strict allowlist | `ApplyPortRule` refuses to apply, preventing naked DROP | on apply |

**Why order matters**: iptables matches in order; if DROP came before ACCEPT, allowlisted IPs would also be dropped. The system always keeps ACCEPT first.

### Idempotency & restart recovery

- Rules are **rebuilt** (not patched): delete old chain → create new → full rewrite. Repeatable, order guaranteed.
- On startup `Reconcile` rebuilds all rules from `allowlist.json`.
- systemd `Restart=always` auto-restarts crashed processes; rules recover.
- Even if iptables is flushed (e.g., kernel rules lost), the service rebuilds on startup.

### Dry-run mode

With `IPAW_DRY_RUN=1`, all iptables operations are printed but not executed:

```
[dry-run] iptables -N IPAW-22
[dry-run] iptables -A IPAW-22 -s 203.0.113.10/32 -j ACCEPT
```

For local dev, rule verification, and CI — **never touches the real firewall**.

---

## Open Source

[MIT License](LICENSE).

**Contributions welcome**: issues, PRs, or stars. Core todos in [plans/plan.md](plans/plan.md).

**Roadmap**: the current single-node version fits dozens of servers; future evolution is "central management + lightweight agent per server", reusing the `internal/iptables` core directly.
