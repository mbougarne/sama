# Read-only provider qualification

Reviewed 2026-10-12. The adapters implement documented HTTP contracts with
synthetic credentials and responses. Production validation/worker registries
remain empty. Successful fixture reads do not qualify a real account, credential
rotation, rate limit, or provider permission. No provider mutation is implemented.

## Contract sources and boundaries

| Family | Authentication and read contract | Identity limitation |
| --- | --- | --- |
| `hetzner.cloud` | Cloud v1 project bearer token with Read permission; `/servers`, `/servers/{id}`; page/per_page, next_page. | Inspected public schemas do not supply a stable project ID. Read access cannot establish rotation continuity. |
| `digitalocean.platform` | Platform v2 bearer token; `account:read` and `droplet:read`; `/account`, `/droplets`, `/droplets/{id}`; links.pages.next. | Documented account.uuid identifies the current **user**. Team/project continuity remains unresolved. |
| `vultr.compute` | API v2 bearer token; `/account`, `/instances`, `/instances/{id}`; meta.links.next is a cursor. | Official account schema has name/email/ACL but no immutable account ID. Exact least-privilege ACL and continuity remain unverified. |
| `contabo.compute` | Compute v1; explicit client ID/secret and API username/password; OAuth password grant; `/me/accounts`, `/compute/instances`, `/compute/instances/{id}`. | Default-account tenant/customer IDs are provider-derived; token-active account versus default-account selection and rotation continuity remain unverified. |

Official sources:

- Hetzner [token scope](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token/), [server schema](https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/schema/server.go), [pagination schema](https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/schema/meta.go). The current server schema uses `location`; deprecated datacenter fields are not a fallback identity.
- DigitalOcean [scopes](https://docs.digitalocean.com/reference/api/scopes/), [account schema](https://github.com/digitalocean/openapi/blob/main/specification/resources/account/models/account.yml), [Droplet schema](https://github.com/digitalocean/openapi/blob/main/specification/resources/droplets/models/droplet.yml).
- Vultr official [account client](https://github.com/vultr/govultr/blob/master/account.go), [instance client](https://github.com/vultr/govultr/blob/master/instance.go), [pagination fixtures](https://github.com/vultr/govultr/blob/master/instance_test.go). The browser could not retrieve the live reference; official SDK contracts are the limited fallback evidence.
- Contabo [API reference](https://api.contabo.com/) documents account/instance schemas, OAuth and UUID4 `x-request-id`. That ID is correlation only. Tokens are kept solely in the owning client, serialized per connection/version, renewed before expiry with bounded jitter, and refreshed once after 401. A second 401 stops; 403 never refreshes.

Every constructor requires an exact workspace/connection/family/version binding,
a typed credential and shared outbound budget. Every page/get checks that binding.
Credential-bearing traffic uses fixed compiled HTTPS hosts and paths; redirects
are denied. No browser-supplied URL, ambient credentials, raw SDK object, password,
user data, console URL, balance or payment metadata enters inventory.

List pages use 50 entries; the shared scanner caps 100 pages, 10,000 rows, 32 MiB
and five minutes. HTTP responses cap 8 MiB and 30 seconds. Unknown statuses remain
unknown with native status retained. DigitalOcean/Contabo links are validated and
reconstructed; Vultr cursors reject URL/path syntax. Empty valid collections are
successful reads; failed or partial reads cannot publish a generation.

Exact provider quota ceilings, Retry-After behavior and quota sharing across two
connections to the same real account remain live-unverified. The shared transport
bounds per-connection concurrency and applies scoped cooldowns. Until provider
identity is established, constructors use the owning connection for the quota
key and cannot enter production registration. This is a qualification limit,
not evidence of project/account-wide rate coordination.

## Mock and database checks

From `backend/`:

```sh
go test -race ./internal/provider/... ./internal/connection
go test -race -tags=integration -p 1 -count=1 ./tests/integration
```

Package fixtures exercise each documented API family: list/get, paging,
empty/partial collections, denials, malformed/oversized responses, 429/deadline,
unknown native states, credential binding and metadata exclusion. Contabo adds
parallel acquisition/renewal, canceled waiting and single-refresh denial tests.
The two-provider PostgreSQL test uses explicitly synthetic normalized ports;
HTTP schema mapping is verified separately in each adapter package. It verifies
publication/query identity separation and last-success retention across failure.
These commands do not call providers or establish live qualification.

## Opt-in disposable acceptance

SAMA-068/071/075/079 share the explicit `sama-provider-read-probe` command. It
accepts no environment credential chain. Use only separately authorized
**disposable** read-only credentials; this implementation request did not
authorize running the live command. Do not place real inputs in the repository.

Create a private mode-0600 JSON file with UUIDv7 `workspace_id`, `connection_id`,
positive `credential_version`, one exact family above, `credential`, and a separate
`denied_credential` whose read permission is intentionally denied. Bearer families
use `{type: "bearer_v1", token: "..."}`; Contabo uses
`{type: "contabo_v1", client_id: "...", client_secret: "...", username: "...", password: "..."}`.
Neither schema accepts an account ID or endpoint. The denied input is mandatory;
a timeout or empty collection never counts as denied-permission evidence.

```sh
go run ./cmd/sama-provider-read-probe --live-read-only --input-file /private/operator-owned/probe.json
```

The command emits aggregate page/resource counts and booleans only. It checks
bounded pagination, one detail when inventory is nonempty, version isolation,
identity validation and the separate denial probe. Empty inventory cannot prove
get semantics. An unresolved identity yields partial evidence and exit 1; it
never saves credentials, publishes capability evidence or enables registration.
A missing explicit input fails before networking. SIGTERM cancels reads.

No live probe has been run for this change. Before enabling any adapter, establish
provider-derived stable account identity (including empty inventory and credential
rotation), compare two explicitly scoped connections, qualify least-privilege
read/get denial and rate behavior, then run the scheduler/HTTP workflow against
the disposable scope. Record API/fixture versions, dated command outcomes and
remaining unknowns separately. Existing user/account identifiers, token hashes
and server fingerprints cannot fill an unresolved project identity requirement.

## Snapshot metadata contracts (SAMA-149)

Reviewed 2026-10-26 against the official Hetzner
[image schema](https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/schema/image.go),
[image client](https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/image.go)
and [token permissions](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token/).
Use a project Read token (GET only). `SnapshotReader` separately maps snapshot
images, source server when supplied, creation timestamp, native/normalized state,
image size and minimum disk size in provider GB. Null fields remain unavailable;
unknown states remain unknown. Description, labels and contents are excluded.
Pages use fixed snapshot filtering, 50 rows and at most 100 pages. A failed page
returns no partial page; callers must finish the walk before publishing a scan.
These ports are not wired into the server-only inventory scanner, public resource
DTOs or production registries. Stable project identity and live read acceptance
remain unverified; no provider credentials or live calls were used.

SAMA-150/151 are blocked on SAMA-117's unrun action-specific disposable evidence.
No snapshot create/delete implementation or enablement is claimed. Their required
recent-auth, bound review, reservation and reconciliation integration remains work
for after that prerequisite; mock results cannot satisfy its live evidence gate.

### DigitalOcean snapshot metadata (SAMA-152)

Reviewed 2026-10-26 against the official
[list permissions](https://github.com/digitalocean/openapi/blob/master/specification/resources/snapshots/snapshots_list.yml),
[get permissions](https://github.com/digitalocean/openapi/blob/master/specification/resources/snapshots/snapshots_get.yml)
and [metadata schema](https://github.com/digitalocean/openapi/blob/master/specification/resources/snapshots/models/snapshots_base.yml).
List documents `snapshot:read`; get documents both `snapshot:read` and
`block_storage_snapshot:read`. The separate port retains source kind/ID for
Droplet and volume snapshots, region availability and GB sizes. No creation
status is documented: status is unknown with reason `not_reported`, even if an
upstream body adds an undocumented status field. Tags and contents are omitted.
Fixed 50-row pages, a 100-page bound and exact host/path/query checks prevent
unsafe continuation. Failure never returns a partial page. Team/project identity,
live read acceptance, scanner/public DTO wiring and production registration remain
unverified or unavailable, as for the existing provider inventory contracts.
