# Sama · سماء

**Your cloud, clearly.** Sama is an open source, self-hosted cloud backoffice designed to give people and teams one place to manage the products in their own cloud accounts. سماء means “sky” in Arabic.

Sama connects through provider APIs. Its goal is to make everyday work—viewing resources, creating servers, managing networks and storage, and following operations—simple while preserving each provider’s capabilities and permission boundaries. See [product scope](docs/product.md) and [provider research](docs/providers.md) for the intended coverage and qualification requirements.

**Sama manages products, never payments.** Providers bill users directly. Sama does not collect payments, store payment methods, settle invoices, or manage billing. Creating resources can incur charges with the provider. See the [payment boundary](docs/product.md#payments-stay-with-the-provider).

## Repository

- [backend/](backend/README.md): Go application and browser-facing API, with backend tests and tooling.
- [frontend/](frontend/README.md): React + TypeScript backoffice, Webpack, and frontend tests and tooling.
- [docs/](docs/README.md): product scope, architecture, provider research, and decisions.
- [scripts/](scripts/): local collaboration and Git hook helpers.

The architecture uses PostgreSQL for durable state and operations, server-held credentials, and explicit review of cloud changes. Backend and frontend have independent source trees and build commands. Node is frontend development/build tooling. The selected stack and its rationale are in the [architecture](docs/architecture/README.md).

## Development

Follow [Contributing](CONTRIBUTING.md#development-setup) to install the pinned toolchains, dependencies, and pre-commit hook. Start each application from its own directory using the commands in the backend and frontend READMEs. Those guides describe the available routes and local behavior; architecture documents define the intended design.

## Contributing and support

Read [Contributing](CONTRIBUTING.md), [community conduct](CODE_OF_CONDUCT.md), and [security reporting](SECURITY.md). AI-assisted contributions follow [AGENTS.md](AGENTS.md). Agent inputs, conversations, research, and command records remain local in the ignored `agents/` directory.

Sama is independent of the cloud providers it integrates with.

## License

[MIT](LICENSE). Commercial use, resale, modification, and proprietary derivatives are permitted under its terms, including notice retention. The license includes warranty and liability disclaimers.
