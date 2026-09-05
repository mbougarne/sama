# Product direction

## Purpose and audience

Sama helps individuals, developers, and small operations teams manage resources spread across cloud providers. Self-hosting is the default distribution model. A personal installation still has one workspace, one owner, and the same authorization boundaries as a team installation.

The first release is an operational console: browse what already exists, inspect it, make a deliberate change, and see a reliable outcome. Infrastructure provisioning becomes a later, explicit workflow once the safety foundation is proven. Sama does not initially replace infrastructure-as-code, become a reseller, perform automatic cost optimization, or promise identical functionality across vendors.

## Product principles

1. **Simple use, rich capability.** Familiar resource views, readable status, progressive disclosure of advanced options. Complexity belongs in accurate explanations and implementation, not extra clicks everywhere.
2. **Truthful state.** Distinguish current observations, stale data, a pending operation, and an unknown outcome. Never display a successful mutation based only on a toast or HTTP acceptance.
3. **Provider identity stays visible.** Show account, project, region, billing implications, and original provider status. Provider-specific features are first-class extensions of a shared workflow.
4. **Safe defaults.** Least-privilege connections, no public credential APIs, no browser-stored provider tokens, explicit review for disruption or spend, no background destructive automation.
5. **A small installation.** One application image plus PostgreSQL for the first useful release. No mandatory analytics service or telemetry collection.
6. **Open development.** Document limits and tradeoffs. Make an adapter understandable without needing to read every provider implementation.

## Payments stay with the provider

**Confirmed user requirement, not a deferred feature:** Sama provides API-based product management only. Users own their provider accounts and maintain their billing and payment relationships directly with those providers. Sama must not handle, manage, collect, or process payments.

Creating a VPS, server, bucket, or another supported product is within the product vision. Such an action may add charges to the user's provider account under its existing terms. The provider handles those charges and any payment; Sama does not submit a payment instruction or act as a merchant or reseller.

There must be no checkout, payment gateway integration, payment-method entry or storage, wallet, account top-up, invoice settlement, refund handling, or provider subscription billing management in Sama. If the provider requires payment or billing-account action before provisioning can proceed, Sama explains the blocker and directs the user to the provider's own portal. It does not complete that step on the user's behalf.

An optional read-only price estimate or explanation of an operation's cost is informational, not a billing service or payment authorization. References to “spend review” elsewhere mean confirming a potentially chargeable resource action, never approving or executing a payment. Future commercial or Pro offerings must preserve this boundary; this architecture includes no payment system.

## Initial release scope

The first usable private alpha supports OIDC sign-in, workspace membership, connection onboarding, read-only inventory, resource details, refresh status, and an audit trail. Hetzner is the first adapter; DigitalOcean validates the abstraction before generalizing further.

The first mutation release adds individual server power operations for qualified providers: power on, graceful shutdown where available, and reboot. Forced power off is a separate disruptive capability. Each action has authorization, a review, a durable operation, and reconciliation. Snapshots, network changes, creation, resize, rebuild, and deletion follow separate acceptance gates. Multi-resource destructive actions stay out of the initial release.

Inventory covers the resource families implemented for each adapter. A missing feature says “not available in this integration” with a reason; an absent permission says “your connection lacks permission.” These are different states.

## Main journeys

### Connect an account

An administrator chooses a provider and API family, sees the smallest required credential scope, supplies credentials over HTTPS, and runs a non-mutating validation. Sama shows the account/project identity and verified read capabilities before saving. Unverifiable permission is shown as unknown, never inferred from a successful login. The first inventory sync runs as a durable job with progress and errors. Additional products with different credentials are separate connections.

### Understand resources

A viewer opens a paginated inventory, filters by account/provider/type/region, and opens a resource. Every view includes the last successful observation time and provider state. A failing provider does not erase other providers’ results. An explicit refresh schedules a deduplicated sync; opening a browser tab does not fan out to every provider.

### Change a resource

An operator selects an action. Sama fetches fresh preconditions and presents the target, account, impact, estimated cost if known, and what cannot be undone. The operator confirms the exact reviewed intent. Sama queues the operation, returns an operation ID, and shows progress until success, failure, or “outcome unknown — checking provider.” A dropped browser connection does not cancel accepted work. The history links back to the affected resource.

### Recover from a problem

When an operation is uncertain, the user sees the last known stage, provider request reference, and supported next steps. Retrying is enabled only when the adapter can prove it is safe. A stale inventory can be inspected but cannot bypass mutation revalidation. Disabling a connection prevents new work and stops unsent work; it does not undo already submitted provider actions.

## Navigation and accessibility

Target navigation: Overview, Resources, Connections, Operations, Activity, Settings. Personal users see one workspace without an unnecessary picker. Keyboard navigation, visible focus, labels, non-color status cues, reduced motion, and mobile reflow are release requirements. English first; preserve Unicode and use logical CSS properties in translated features so Arabic and RTL support can be added without redesigning domain models. Display the name as Sama / سماء without presenting the interface as already localized.

## Explicit exclusions and extension path

No embedded SSH terminal, arbitrary cloud HTTP proxy, provider console scraping, raw policy editor, automatic resource migration, workload scheduler, or full AWS console replacement in v1. Payment and billing management are excluded from the product entirely, as defined above. Existing infrastructure-as-code ownership is shown as a warning when reliably known; Sama does not claim to discover all external owners. Object content browsing, uploads, queue reads, and notification delivery are separate data-access capabilities and are not implied by control-plane inventory.

## Acceptance measures

Release gates, not current measured results: a new contributor can run the future application in under 10 minutes after dependencies; a connected account reaches a clearly reported sync outcome; each mutation can be traced to a person and immutable intent; a worker crash never causes a blind duplicate create/delete; isolation checks cover all tenant-bearing endpoints. Performance budgets and test conditions are defined in [deployment](architecture/deployment.md).
