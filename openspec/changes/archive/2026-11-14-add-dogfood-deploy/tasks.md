## 1. Contract and regression tests

- [x] 1.1 Specify deploy, reload, lease, run, and set_dogfood behavior.
- [x] 1.2 Add failing isolated regression tests for config, readiness, checkout, lease, and async behavior.

## 2. Implementation

- [x] 2.1 Add shared readiness configuration and validation.
- [x] 2.2 Implement lease-gated dogfood deploy and preserve checkout during reload/set_dogfood.
- [x] 2.3 Add async MCP handlers, catalog registration, CLI and wiring tests.
- [x] 2.4 Update README and agent instructions.

## 3. Verification and delivery

- [x] 3.1 Run make test, make vet, make fmt, and make build; review diff.
- [x] 3.2 Archive change and validate all specs strictly.
- [x] 3.3 Prepare PR summary and caller migration instructions for delivery against main.
