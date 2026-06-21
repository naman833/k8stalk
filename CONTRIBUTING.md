# Contributing to k8stalk

Thank you for your interest in contributing to k8stalk!

## Development Setup

1. **Go 1.22+** required
2. Clone the repo: `git clone https://github.com/naman833/k8stalk.git`
3. Install dependencies: `go mod download`
4. Build: `make build`
5. Run tests: `make test`

## Code Structure

- `cmd/` — CLI commands (cobra)
- `pkg/analyzers/` — One file per analyzer, implements `agent.Tool`
- `pkg/llm/` — One file per LLM provider, implements `llm.Provider`
- `pkg/agent/` — Tool interface, registry, agent loop
- `pkg/k8s/` — Kubernetes client wrapper
- `pkg/sanitize/` — Anonymization before sending to cloud providers
- `pkg/config/` — Config file management

## Adding a New Analyzer

1. Create `pkg/analyzers/<resource>.go`
2. Implement `agent.Tool` interface (`Spec()` + `Execute()`)
3. Register in `pkg/analyzers/registry.go`
4. Add table-driven tests using `k8s.io/client-go/kubernetes/fake`

## Adding a New LLM Provider

1. Create `pkg/llm/<provider>.go`
2. Implement `llm.Provider` interface
3. Register constructor in `pkg/llm/registry.go`

## Conventions

- Small, single-purpose files
- Table-driven tests
- No live cluster required for unit tests (use fake clientset)
- All data anonymized before leaving to cloud LLM providers
- Conform exactly to `Tool` and `Provider` interfaces — no special cases

## Pull Request Process

1. Fork and create a feature branch
2. Ensure all tests pass: `make test`
3. Ensure lint passes: `make lint`
4. Write clear commit messages
5. Open a PR with a description of what changed and why

## License

By contributing, you agree that your contributions will be licensed under the Apache-2.0 License.
