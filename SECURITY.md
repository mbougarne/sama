# Security policy

Sama currently contains architecture documentation only. There is no running application or credential-bearing service. The target [security architecture](docs/architecture/security.md) describes required controls before real cloud credentials or mutations are enabled.

## Reporting

Do not publish credentials, exploitable vulnerability details, or private account data in public issues. While this repository remains local, share findings directly with the repository owner through an existing private channel. A public security contact and private reporting channel must be configured before the first public release; no email address or reporting SLA is claimed yet.

Include affected version/commit, expected versus actual behavior, a minimal reproduction using synthetic data, and potential impact. Do not test against another person’s account or infrastructure. For an exposed provider key, revoke it with the provider first and then report the relevant sanitized evidence.

## Supported releases

There are no supported production releases yet. Before release, define the support window, response process, dependency update policy, and security advisories workflow. See [roadmap](docs/roadmap.md) for release gates.

## Before production use

When an implementation is available, evaluate it in your own isolated sandbox with disposable provider resources before connecting production accounts. Check permissions, failure recovery, backups, and upgrades for your deployment; start with read-only access and minimal privileges. Users operate their own provider accounts and are responsible for deployment choices and provider charges.

Sama is provided under the [MIT license](LICENSE), which contains warranty and liability disclaimers. Those terms are not a guarantee that every form of liability is excluded under every applicable law. The sandbox guidance is operational advice, not an additional license restriction or a warranty that testing removes all risk. Obtain jurisdiction-specific legal advice if you need a determination of how those terms apply to a particular deployment or commercial agreement.
