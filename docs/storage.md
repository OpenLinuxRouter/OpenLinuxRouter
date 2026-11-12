# Configuration and runtime storage

`/etc/open-linux-router/olr.json` is the source of operator intent. Custom
latency targets now live in `gateway.latency_sites`, including an explicit
empty list when cleared. Backends still receive generated files, certificates
and keys. IP city lookups use ip.sb with a persistent bounded cache. Kernel
state and backend lease files remain owned by their respective systems.

OLR-owned runtime persistence lives in `/var/lib/open-linux-router/runtime.db`.
The pure Go SQLite driver supports static amd64/arm64 builds without CGO.
The file has mode 0600; new state directories have mode 0700. SQLite sidecars
inherit the database's permissions. One shared connection uses WAL,
`synchronous=FULL`, a five-second busy timeout, and transactional schema
migrations tracked by `PRAGMA user_version`. A newer schema is never rewritten
by an older binary.

| Table | Contents and lifetime |
| --- | --- |
| `activity` | Half-hour traffic differences per client, keyed by canonical MAC and by address for traffic no presence source could place; 90 days, at most 250,000 retained client/bucket pairs (reaching the ceiling drops the oldest buckets, so the window shortens instead of recording stopping) |
| `device_seen` | Canonical MAC, last-heard timestamp and the last hostname the client announced with the time it was heard; 90 days |
| `sessions` | SHA-256 token hashes, password binding and expiry; 90 days, maximum 256 active sessions |
| `qos_state` | Queue ownership and snapshot, scoped to Linux boot ID |
| `recovery_state` | Original IPv6 settings, original resolv.conf, last applied delegated prefix |
| `imports` | Completed legacy-import markers, preventing stale files from resurrecting data |
| `geoip_cache` | ip.sb city/coordinates/ASN results; 30-day freshness, 2,000-entry LRU |

World traffic and remote clients share one asynchronous ip.sb lookup queue,
paced at one request per 600 ms (100/minute). Cache hits update memory recency;
disk recency is touched at most once per minute per IP. Expired results remain
visible during refresh. Failures retry after one minute, and HTTP 429 pauses
the shared queue for at least one minute or the supplied Retry-After duration.
Private/local addresses never reach the service. Public IP cache misses are
sent to ip.sb; the map uses returned city coordinates, falling back to country
centers when coordinates are missing. Existing DB-IP files are no longer used
by the daemon and are left untouched on upgrade. Database failure falls back
to the same bounded in-memory cache.

Activity samples every ten seconds and checkpoints changed buckets every minute
and on graceful shutdown. Baselines are never restored: the first post-restart
sample establishes a new baseline. Gaps, resets and bucket-boundary crossings
remain omitted. Uint64 byte totals use decimal TEXT to avoid SQLite's signed
integer limit. Address/time indexes permit later analysis without storing raw
flows. Existing history is associated with IP addresses, not proven device
identity: DHCP address reuse can attribute older activity to a different device.
A future device-based series must record identity at observation time; migration
cannot infer historical ownership accurately.

Last-seen and sessions use transactional checkpoints. QoS state from a previous
boot is never used against new kernel resources. Safety-critical recovery
records are saved before modifying the corresponding system setting.

## Upgrade and failure behavior

Each legacy runtime file imports once in a transaction, together with its marker.
Failure rolls back both data and marker. Source contents remain untouched for
manual recovery and are no longer read after successful import. This covers
`activity.json`, `devices/seen.json`, `sessions.json`, `/run/olr/qos.json`,
`host/ipv6-auto.json`, `host/resolv.conf.orig`, and `dial/delegated-prefix`.
Legacy file permissions are tightened to 0600 before import. QoS imports occur
once, and the imported state is scoped to the boot in which it was read; a
retained legacy file cannot restore ownership after a later reboot.

Malformed activity, last-seen or session JSON is logged and skipped, allowing
new state to accumulate in SQLite. Skipped sources remain available for manual
recovery. Ownership and original-setting imports are never silently skipped;
their failure blocks the affected operation.

Legacy `olr.json.latency-sites` is copied into `olr.json`, then renamed to
`.latency-sites.migrated`. An existing `latency_sites` field takes precedence.
The primary configuration is never reconstructed from the runtime database.

An unavailable or corrupt database is preserved and logged. The daemon serves
configuration and uses memory-only sessions and observation history; QoS and
host/prefix operations requiring recovery records refuse to change resources.
The daemon does not delete or automatically replace the database. Fix the file
or storage and restart to restore persistence. A write failure preserves memory
and reports/logs the failure; logout does not report success without committing
revocation.

## Backup and future analysis

Use `olr backup <file>` and `olr restore <file> --dry-run` for the complete intent
document, or Advanced → Backup in the browser. [backup.md](backup.md) describes
protected restore, credential handling and the external files to retain.

Back up `olr.json` for intent, and use SQLite's backup API or the sqlite3
`.backup` command for runtime history. Copying only the database file while WAL
is active is not a consistent backup. With the daemon stopped, the database and
any remaining WAL sidecar form the recovery set. Backups contain credential
hashes and device history and need the same restricted permissions.

No new DNS-event, flow or latency-history collection is enabled by this change.
Future features must define bounded retention, sampling, user controls and
indexes explicitly. Database support makes those features possible without
turning every packet into a disk write.
