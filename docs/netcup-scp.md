# netcup SCP REST compatibility

Assessed **2026-10-20** against the official [SCP REST help](https://www.netcup.com/en/helpcenter/documentation/server/rest-api) and [technical schema](https://www.netcup.com/en/helpcenter/documentation/servercontrolpanel/api). This is a documentation assessment of existing servers, not account or product qualification.

## Source boundary

The published OpenAPI 3.1.0 schema identifies itself as `SCP (Server Control Panel) REST API`, version `2026.1020.132958`. Its version follows SCP releases; it is not a promise of API compatibility. Breaking changes use endpoint path versions. The decoded schema SHA-256 is `61852b19ef09faa7b666977372b8c778b1abe1d0d8a1c0dd55a16f8b837761c8`.

The only published server URL is relative: `/scp-core`. The inspected schema has no security scheme, top-level security requirement, authentication endpoint or authenticated-current-user endpoint. The same omissions are present in the [documentation's source JSON](https://www.netcup.com/api/v2/content/helpcenter/documentation/servercontrolpanel/api), not just the extracted page. The help article directs customers to the SCP's Authentication section. Its [official screenshot](https://www.netcup.com/uploads/server_scp_api_scp_Abb2_EN_610e96449d.png) shows that section collapsed. The public schema does not establish the absolute credential-bearing origin, credential fields, token acquisition/renewal or how an authenticated subject binds to a customer. These contracts are unavailable in the inspected public material; authentication and native identity are not demonstrated to be unsupported by SCP.

The help article documents optional IP/CIDR restrictions. An empty filter permits all IPs. Sama must respect the account's restriction; it must not change it. No customer account, session, token or IP configuration was accessed for this assessment.

## Documented existing-server surface

Paths below are relative to the schema server URL. An endpoint being documented does not qualify the action for execution.

| Capability | Schema evidence | Remaining gate |
| --- | --- | --- |
| List | `GET /api/v1/servers`; array of `ServerListMinimal`; integer `limit`/`offset`, optional name/IP/search/firewall filters and sorting | Establish default bounds, stable complete traversal and product coverage in a disposable account; never tombstone a partial traversal |
| Get | `GET /api/v1/servers/{serverId}`; integer ID; `loadServerLiveInfo` defaults to true | Verify ownership isolation and live-info availability; list entries alone have no power state |
| State | `ServerInfo` exposes native states; the enum includes `NOSTATE`, `RUNNING`, `BLOCKED`, `PAUSED`, `SHUTDOWN`, `SHUTOFF`, `CRASHED`, `PMSUSPENDED`, `DISK_SNAPSHOT` | Keep unfamiliar/transitional states unavailable for actions; do not conflate requested state with observed state |
| Power on | `PATCH /api/v1/servers/{serverId}` with `application/merge-patch+json`; `ServerStatePatch.state` permits `ON` | Authenticate, establish product/state restrictions and prove action receipt/completion |
| Shutdown | Same PATCH; state permits `OFF` | The schema does not explicitly establish graceful guest shutdown semantics; do not infer them from the absence of `stateOption` |
| Reboot | Same PATCH; `stateOption` for `ON` permits `POWERCYCLE` or `RESET` | Distinguish native behavior and select an explicitly qualified operation; do not silently choose between reset and power cycle |
| Forced off | Same PATCH; `stateOption` for `OFF` permits `POWEROFF` | Prove forced-off semantics separately from graceful shutdown |
| Receipt | PATCH documents HTTP 200 with an empty schema or HTTP 202 with `TaskInfo`; `uuid` identifies a task | HTTP 200 is not a native task receipt or proof of completion; transport loss stays ambiguous with no blind retry |
| Completion | `GET /api/v1/tasks/{uuid}` returns `TaskInfo`; states include pending/running/finished/error/cancellation/rollback | Verify task ownership, native failure handling and follow-up server state; never use request correlation as idempotency |

PATCH allows one attribute at a time. Sama's contemplated power slice must not include unrelated autostart, image, storage, rescue, firewall or network changes. The schema's server templates do not provide a documented mapping to all commercial Root Server/VPS generations. No commercial product is marked supported from a template name alone.

## REST and SOAP separation

The [SCP home documentation](https://www.netcup.com/en/helpcenter/documentation/server/scp-home) describes a separate SOAP webservice with explicit activation and its own password. It is not a REST credential fallback. No concrete REST function or product gap has been demonstrated by the available evidence. Inability to establish public REST authentication is not such a gap; a SOAP implementation is therefore unwarranted.

## Required qualification sequence

1. Obtain the official Authentication contract and absolute origin, without exporting live credentials. Establish the native authenticated subject/customer identity and IP policy.
2. Bind that verified contract to one immutable workspace/connection/credential version using Sama's confined transport. Reject SOAP credentials and ambient identity sources.
3. Implement bounded list/get fixtures for explicitly identified existing products, including incomplete pages and unknown states.
4. Run an explicitly authorized disposable **read-only** probe proving native identity, list/get and cross-connection denial before claiming inventory acceptance. The shared inventory implementation is on main; its unavailable external acceptance is not implied by merged code.
5. Qualify each power action independently through the existing durable review/dispatch/recovery boundary. Require action-specific disposable acceptance before enablement. Preserve ambiguity after possible acceptance and keep graceful shutdown distinct from force off.

SAMA-133 establishes schema coverage and its missing evidence. SAMA-134–140 remain gated by the missing authentication/identity contract and subsequent read/action acceptance. SAMA-141 has no demonstrated SOAP gap to implement. DNS uses separate API families and can be assessed independently. Server ordering, purchases and payments are outside this surface.
