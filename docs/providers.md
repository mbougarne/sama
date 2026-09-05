# Provider research and integration strategy

Research date: **2026-09-04**. This is a documentation-based feasibility assessment, not live account validation. No cloud credentials were used. Product availability, permissions, regional coverage, pagination, and lifecycle behavior must be verified per adapter before it is advertised as supported. The provider delivery order is accepted; exact capability coverage still requires qualification. There is no application catalog or capability API yet.

## Verified API surfaces

| Provider | Documentation finding | Sama decision |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hetzner | The API overview distinguishes Cloud, Storage Box, and Robot APIs. Cloud covers servers and related resources; actions can remain asynchronous and rate limiting is project-scoped. | Start with Cloud server inventory and native action polling. Keep Robot and Storage Box integrations separate. [Overview](https://docs.hetzner.cloud/), [Cloud reference](https://docs.hetzner.cloud/reference/cloud) |
| DigitalOcean | Its platform API manages Droplets and other resources; Spaces exposes a separate S3-compatible API. Personal access tokens and OAuth are documented separately. | Use a scoped platform token first; treat Spaces credentials and S3 behavior as a separate adapter family. [API overview](https://docs.digitalocean.com/reference/api/), [token scopes](https://docs.digitalocean.com/reference/api/scopes/) |
| Vultr | The provider’s official generated client documentation describes API v2, bearer authentication, JSON requests, and cursor pagination. | Plan v2 compute inventory and power operations; qualify current semantics against the live reference before implementation. The marketing/API pages could not be retrieved by the research browser; the official repository was the fallback evidence. [Official client/reference](https://github.com/vultr/vultr-java), [API entry point](https://www.vultr.com/api/) |
| Contabo | The API reference shows client ID/secret plus API username/password exchanged for a bearer token, and request IDs on calls. It documents instance and snapshot endpoints. | Isolate token acquisition, expiry handling, and request correlation in the adapter. Treat request IDs as tracing, not proof of idempotency. [API reference](https://api.contabo.com/) |
| netcup server | The current help center documents an SCP REST API with authentication instructions in its API documentation and configurable IP filters. It also documents a separate older SOAP webservice. | Prefer a REST qualification spike; retain SOAP only if needed for a specifically supported function. Do not assume API coverage or authentication from old community examples. [REST API](https://www.netcup.com/en/helpcenter/documentation/server/rest-api), [SCP webservice settings](https://www.netcup.com/en/helpcenter/documentation/server/scp-home) |
| netcup DNS | CCP documentation distinguishes Legacy DNS credentials from CloudDNS API keys. Legacy authentication uses a session. Domain reselling requires a separate agreement. | Model legacy DNS and CloudDNS as separate API families. Defer domain registration and purchasing. [CCP API](https://www.netcup.com/en/helpcenter/documentation/domain/our-api) |
| AWS | The official Go v2 SDK supports S3, SQS, and SNS. Its default credential chain can read environment, profiles, and workload credentials. | Use Go SDK v2 with explicitly bound connection credentials. Add S3 first; keep SQS and SNS later. [SDK](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/welcome.html), [services](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/go_code_examples.html), [credential chain](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html) |

## Delivery scope, not promised feature parity

| Adapter family | First slice | Later candidate slices | Qualification uncertainty |
| --------------------- | ----------------------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Hetzner Cloud | List/get servers, account/project identity, observed status | Power actions, snapshots, volumes, networking, creation | Scope permissions, deletion protection, polling and operation correlation |
| DigitalOcean platform | Droplet inventory and account identity | Actions, snapshots, volumes, networking | Token scope combinations, action completion and regional behavior |
| Vultr v2 | Instance inventory | Power operations, snapshots, network controls | Current rate headers, async status, permission/IP restrictions |
| Contabo v1 | Instance inventory and token lifecycle | Power operations, snapshots | Token/session failure behavior, product-specific coverage and polling |
| netcup SCP REST | Document authentication and list/get coverage in a compatibility report | Qualified server control | Access to the actual API schema, auth rules, supported existing products; do not promise ordering servers |
| netcup DNS | Select API family and validate zone/record reads | Reviewed record-set changes | Legacy vs CloudDNS migration, whole-zone update semantics and concurrent edits |
| S3 family | Bucket metadata and explicitly scoped object listing | Controlled upload/download, lifecycle visibility | Endpoint/addressing differences, IAM permissions, pagination, versioning, multipart support |
| AWS SQS/SNS | Queue/topic configuration inventory | Explicit management actions | Region/IAM rules, delivery side effects, subscription confirmation |

Resource prices are always fetched from the appropriate provider at the time of review when possible, with currency, units, and timestamp. “Unknown” is preferable to a fabricated estimate. Stopping a resource must not be described as stopping billing without provider-specific evidence.

These prices are informational only. Adapters may create and manage supported resources on the user's own provider account, but must never submit payments, top up balances, manage payment methods, settle invoices, or resell services. Any required billing/payment step belongs in the provider's portal. This is a permanent product boundary, including for future integrations.

## Capability design

An adapter declares implemented operations per resource type and API version. A connection separately records permissions that were actually verified. A resource carries state-specific restrictions. The effective action list is their intersection plus Sama’s own authorization policy.

Use stable operation names such as `compute.server.read`, `compute.server.power_on`, `compute.server.shutdown`, `compute.server.power_off`, `compute.server.reboot`, `dns.recordset.update`, `storage.object.list`. Do not collapse graceful shutdown into forced power off. Each capability records risk, required inputs, read/write classification, async behavior, and retry safety. A browser-hidden action remains forbidden by the server.

Keep native fields in namespaced, versioned, allowlisted views. Prefer a small useful common resource model plus typed details. Do not build one generic provider object with hundreds of optional fields, arbitrary user-defined methods, or raw API passthrough.

## AWS and S3 boundaries

S3 compatibility is a protocol family, not a guarantee of AWS feature parity. A connection contains provider family, region, approved endpoint profile, addressing style, and its own credential reference. Capability tests determine support; AWS-specific encryption, IAM, lifecycle, versioning, and object-lock behavior must not silently propagate to other services.

Sama must not accidentally inherit the host’s AWS identity for a user-created connection. Prefer configured role assumption with short-lived sessions and connection-specific external IDs where appropriate; use a dedicated bootstrap identity only for that documented role flow. Otherwise accept separately scoped credentials. Reject root-account credentials where detectable. Tenant reads and presigning resolve their own authorized connection.

SQS message receive can change visibility and SNS publish delivers messages. They are not harmless “preview” actions. Queue purge, topic deletion, subscription changes, object overwrite, and bucket deletion require separate high-impact reviews. Notification and queue delivery are outside the proposed initial release scope.

## Adapter acceptance checklist

Before an adapter becomes available:

1. Record the official API version, base URLs, credential fields and storage classification, verified scopes, region coverage, and pagination strategy.
2. List implemented resource/actions; mark unknown/unverified features unavailable. Add fixtures from synthetic or scrubbed data with provenance and fixture schema version.
3. Test invalid/expired credentials, denied permission, 404, conflict/locked state, rate limiting, timeout, malformed/oversized responses, partial pagination, and unknown status values.
4. Classify each mutation’s idempotency and ambiguity recovery. A provider correlation header alone is not sufficient. Verify polling behavior and deadlines.
5. Prove connection/workspace isolation, log redaction, TLS and endpoint controls, and enforced request/concurrency budgets.
6. Run an opt-in live contract check with a disposable account/project and an explicit spend ceiling. Read-only checks first. Mutation acceptance must include cleanup and a follow-up inventory proving the final state.
7. Publish capability-level evidence and known limits. Have a kill switch per adapter/action without disabling inventory inspection.

No adapter code is implemented yet. Official documentation confirms feasibility; it does not establish production acceptance.
