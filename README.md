# k8stalk

**GitOps-aware, conversational Kubernetes diagnostics agent.**

`k8stalk` scans your Kubernetes clusters, diagnoses issues using agentic multi-step reasoning, and correlates failures with ArgoCD/Flux GitOps activity — all in plain English.

*Out of the box integration with Ollama (fully local), OpenAI, Anthropic, Azure OpenAI, Google Gemini, Vertex AI, AWS Bedrock, and any OpenAI-compatible endpoint.*

[![CI](https://github.com/naman833/k8stalk/actions/workflows/ci.yml/badge.svg)](https://github.com/naman833/k8stalk/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Version](https://img.shields.io/github/go-mod/go-version/naman833/k8stalk.svg)](https://github.com/naman833/k8stalk)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/naman833/k8stalk)](https://github.com/naman833/k8stalk/releases)
[![GitHub last commit](https://img.shields.io/github/last-commit/naman833/k8stalk/main)](https://github.com/naman833/k8stalk/commits/main)
[![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/naman833/k8stalk)](https://github.com/naman833/k8stalk)


## Table of Contents

- [Why k8stalk](#why-k8stalk)
- [Installation](#installation)
- [Getting Started](#getting-started)
- [Quick Demo](#quick-demo)
- [Configuring an LLM Backend](#configuring-an-llm-backend)
- [Connecting to Your Cluster](#connecting-to-your-cluster)
- [Usage](#usage)
- [GitOps Awareness (ArgoCD & Flux)](#gitops-awareness-argocd--flux)
- [Data Privacy & Anonymization](#data-privacy--anonymization)
- [RBAC Requirements](#rbac-requirements)
- [Architecture](#architecture)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)


## Why k8stalk

Diagnosing Kubernetes issues today means manually correlating pod status, events, deployment state, and logs — often by running `kubectl describe` and `kubectl logs` in a loop across multiple resources, piecing together a timeline by hand. When GitOps is in the picture (ArgoCD syncs, Flux reconciliations), the surface area grows further: was the pod crashing *before* or *after* that last sync? Did the HelmRelease actually apply cleanly? These questions require checking three or four different resources in sequence, every single time.

k8stalk combines **deterministic static analysis** (scanning your cluster for known issue patterns across 16 resource types) with **agentic multi-step reasoning** (letting an LLM choose which diagnostic tools to call, correlate findings across resources, and synthesize a root cause in plain English). It also provides **first-class ArgoCD and Flux awareness** — something structurally absent from k8sgpt — so the agent can automatically check whether a recent GitOps sync correlates with the failure you're investigating.

What makes k8stalk different from [k8sgpt](https://github.com/k8sgpt-ai/k8sgpt):

- **Native GitOps correlation** — automatically detects whether ArgoCD apps or Flux Kustomizations/HelmReleases had recent syncs that correlate with workload failures, using timing proximity, owner references, and label selectors.
- **Conversational multi-turn diagnosis** — instead of one-shot "explain this finding", the agent loop lets the LLM investigate iteratively: check a pod, then its events, then the owning deployment, then correlate with a recent ArgoCD sync, then synthesize a single coherent answer.
- **Fully local/offline operation via Ollama** — run the entire tool with zero data leaving your machine. No API keys, no cloud calls. Plug in any locally-pulled model and diagnose with complete privacy.


## Installation

### Homebrew (macOS/Linux)

```bash
brew install naman833/k8stalk/k8stalk
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/naman833/k8stalk/releases) — available for Linux, macOS, and Windows (amd64/arm64). Also available as `.deb`, `.rpm`, and `.apk` packages.

### Build from source

Requires **Go 1.25+** (see `go.mod`).

```bash
git clone https://github.com/naman833/k8stalk.git
cd k8stalk
make build
```


## Getting Started

After installing, run the interactive setup wizard:

```bash
k8stalk init
```

The wizard walks you through backend selection, model configuration, and connectivity testing:

```console
$ k8stalk init

Supported backends:
  1) anthropic
  2) ollama
  3) openai
  4) azureopenai
  5) google
  6) vertexai
  7) amazonbedrock
  8) customrest

Select backend [1-8]: 2
Ollama base URL [http://localhost:11434]:

Installed Ollama models:
  1) gemma4:12b

Select model [1-1] or type a name: 1

Testing connectivity to ollama (model: gemma4:12b)...
Connected successfully (response: ok)

You're set up with ollama (gemma4:12b).
Using your current Kubernetes context: arn:aws:eks:us-east-1:123456789012:cluster/my-cluster.

Run your first scan:
  k8stalk analyze

Then try: k8stalk diagnose "<question>" or k8stalk chat for the interactive UI.
```

That's it — you're ready. Run your first cluster scan:

```bash
k8stalk analyze
```


## Quick Demo

Suppose you have a pod stuck in CrashLoopBackOff. There's also a stale `FailedScheduling` event from an earlier incident on the same node — a red herring that wastes time if you're reading events manually.

### Static analysis catches the deterministic signal

```console
$ k8stalk analyze --namespace payments --filter Pod

🔍 Analyzing resources in namespace: payments

Pod  payment-processor-6f8b4d7c9-xk2lp  [CRITICAL]
  └─ Container "worker" terminated: OOMKilled (exit code 137)
     Last state: terminated with exit code 137 at 2026-06-21T09:42:18Z
     Restart count: 7
     Memory limit: 256Mi — container exceeded its memory limit

Pod  payment-processor-6f8b4d7c9-xk2lp  [WARNING]
  └─ Pod in CrashLoopBackOff — back-off restarting failed container

Found 2 issues in 1 resource (1 critical, 1 warning)
```

The `analyze` command runs deterministic checks — no LLM needed. The OOMKilled signal is surfaced directly from the container's termination reason in the pod status, not from events.

### Agentic diagnosis correlates across resources

```console
$ k8stalk diagnose "why is payment-processor crashlooping in the payments namespace?"

⠋ Investigating...

I investigated the payment-processor pods in the payments namespace.

**Root Cause: OOMKilled (Out of Memory)**

The container "worker" in pod `payment-processor-6f8b4d7c9-xk2lp` is being
terminated by the kernel OOM killer (exit code 137). It has restarted 7 times.

**Details:**
- Memory limit is set to 256Mi, but the container exceeds this on startup
- The container reaches its memory ceiling within ~30 seconds of starting
- This produces the classic CrashLoopBackOff cycle: start → OOM → restart → backoff

**Red herring dismissed:**
- There is a stale FailedScheduling event from 3 hours ago related to node
  pressure, but scheduling succeeded subsequently. Unrelated to the current crash loop.

**Recommendation:**
Increase the memory limit for the "worker" container. Based on 7 rapid OOM kills,
256Mi is clearly insufficient. Consider profiling actual usage and setting the
limit to at least 512Mi with a request of 384Mi.
```

The `diagnose` command uses the LLM agent loop: it decides which tools to call, inspects the pod, reads events, checks for recent GitOps syncs, dismisses irrelevant signals, and produces a single coherent explanation.

### Fast mode — reliable with smaller models

For local models or smaller LLMs that struggle with multi-step tool selection, `--fast` gathers everything upfront and asks the LLM once:

```console
$ k8stalk diagnose "why is payment-processor crashlooping?" --fast

⠋ Gathering diagnostics...
⠋ Analyzing with LLM...

Root cause: OOMKilled — the "worker" container in payment-processor-6f8b4d7c9-xk2lp
is exceeding its 256Mi memory limit and being killed by the kernel (exit code 137,
7 restarts). Increase the memory limit to at least 512Mi.
```

### Interactive chat UI

For multi-turn investigation, launch the browser-based chat:

```console
$ k8stalk chat
Chat history stored locally at ~/.config/k8stalk/history.db
Starting server on http://localhost:8080
Opening browser...
```

The web UI supports full conversational diagnosis with persistent session history — ask follow-up questions, drill into specific resources, and export findings.


## Configuring an LLM Backend

k8stalk requires an LLM backend for `diagnose`, `chat`, and `analyze --explain`. The `analyze` command *without* `--explain` runs purely deterministic checks and works with no LLM configured.

### Interactive setup (recommended)

```bash
k8stalk init
```

The init wizard:

1. Displays all 8 supported backends and lets you choose one
2. **For Ollama**: checks if Ollama is running locally, lists your pulled models, and lets you pick one
3. **For cloud backends**: prompts for the environment variable name where your API key is stored (it never asks for the raw key)
4. **For backends needing a region or base URL** (Bedrock, Azure, Vertex, custom): prompts for those values
5. Runs a **live connectivity test** against the configured backend before saving
6. Writes configuration to `~/.config/k8stalk/config.yaml`

### Manual configuration

Create or edit `~/.config/k8stalk/config.yaml` directly (or `$XDG_CONFIG_HOME/k8stalk/config.yaml` on Linux):

**Ollama (fully local, no API key needed):**

```yaml
default_backend: ollama
providers:
  - backend: ollama
    model: qwen2.5:14b
    base_url: http://localhost:11434
```

**Anthropic (cloud, API key via environment variable):**

```yaml
default_backend: anthropic
providers:
  - backend: anthropic
    model: claude-sonnet-4-20250514
    api_key_env: ANTHROPIC_API_KEY
```

The `api_key_env` field stores the *name* of the environment variable — never the key itself. k8stalk reads the key from the environment at runtime.

### Supported backends

| Backend | Config key | Auth | Tool-use support |
|---------|-----------|------|------------------|
| Anthropic | `anthropic` | API key (env var) | Native |
| OpenAI | `openai` | API key (env var) | Native |
| Azure OpenAI | `azureopenai` | API key + base URL | Native |
| Google Gemini | `google` | API key (env var) | Native |
| Google Vertex AI | `vertexai` | GCP credentials, project, region | Native |
| AWS Bedrock | `amazonbedrock` | AWS credentials/IAM role + region | Native (Claude on Bedrock) |
| Ollama | `ollama` | None (local) | Native for supported models; prompted fallback for others |
| Custom REST | `customrest` | Optional API key + base URL | Any OpenAI-compatible endpoint (vLLM, LM Studio, LocalAI, llama.cpp server) |

Models without native tool-calling support automatically fall back to a **prompted-tools protocol** (`pkg/llm/prompted_tools.go`) — the agent loop still functions, just with lower reliability than models with native function calling.

### Per-invocation overrides

Override the configured backend or model for a single command:

```bash
k8stalk diagnose "why is my pod failing?" -b anthropic -m claude-sonnet-4-20250514
k8stalk analyze --explain -b ollama -m llama3.1:8b
```


## Connecting to Your Cluster

k8stalk uses your standard kubeconfig — the same file and context that `kubectl` uses. It works with any cluster accessible via kubeconfig: EKS, AKS, GKE, kind, minikube, k3s, Rancher, or any other conformant Kubernetes distribution.

```bash
# Uses current kubectl context by default
k8stalk analyze

# Override context
k8stalk analyze --context production-us-east-1

# Scope to a specific namespace
k8stalk diagnose "any pod issues?" --namespace payments

# Both
k8stalk analyze --context staging --namespace default
```

If no `--namespace` is specified, k8stalk operates across all namespaces the current credentials have access to.


## Usage

### `analyze` — Deterministic cluster scan

Scans your cluster for known issue patterns. No LLM required unless `--explain` is used.

```bash
k8stalk analyze                                  # all resources, all namespaces
k8stalk analyze -n kube-system                   # scope to namespace
k8stalk analyze --filter Pod,Deployment,Service  # specific analyzers only
k8stalk analyze -o json                          # JSON output for CI/scripting
k8stalk analyze --explain                        # LLM explains each finding
k8stalk analyze --filter Pod --explain -n prod   # combine flags
```

**Available analyzer filters:** `Pod`, `Deployment`, `StatefulSet`, `ReplicaSet`, `Service`, `Ingress`, `PVC`, `HPA`, `PDB`, `Job`, `CronJob`, `Node`, `NetworkPolicy`, `Webhook`, `Event`, `Logs`

### `diagnose` — Agentic one-shot diagnosis

Ask a natural-language question about your cluster:

```bash
k8stalk diagnose "why is checkout-service crashlooping?"
k8stalk diagnose "are there any PVC issues in the storage namespace?"
k8stalk diagnose "what's wrong with the ingress for api-gateway?"
```

**Default mode (agentic):** The LLM decides which tools to call turn by turn. It might check a pod, then read its events, then inspect the owning deployment, then correlate with ArgoCD — building context across multiple steps before synthesizing an answer. Best suited to larger, more capable models (Claude, GPT-4-class, Qwen 2.5 14B+) that handle multi-step tool selection reliably.

**`--fast` mode (single-shot):** Gathers all diagnostic data upfront (pod status, events, logs for any flagged pods) in one pass, then sends everything to the LLM in a single call. More reliable for smaller or local models that struggle with multi-step tool selection and loop termination.

```bash
k8stalk diagnose "why is gatekeeper-audit crashing?" --fast
```

### `chat` — Interactive browser-based chat

Launches a local web server and opens a browser-based chat interface for multi-turn diagnostic conversations. Session history persists locally in SQLite.

```bash
k8stalk chat                    # opens browser at http://localhost:8080
k8stalk chat --port 9090        # custom port
k8stalk chat --clear-history    # delete all stored chat history and exit
```

### `serve` — Headless server mode

Same HTTP server as `chat` but doesn't open a browser. Useful for scripting, remote localhost access, or integrating with other tools.

```bash
k8stalk serve                   # listen on :8080
k8stalk serve --port 3000       # custom port
```

### `dump` — Export diagnostic bundle

Exports all analyzer findings as a JSON bundle for offline review, sharing with teammates, or attaching to incident tickets.

```bash
k8stalk dump                              # all namespaces
k8stalk dump -n production -o json        # scoped, JSON output
k8stalk dump --context staging > report.json
```

### `auth` — Manage LLM backends

```bash
# Add backends
k8stalk auth add --backend ollama --model qwen2.5:14b --baseurl http://localhost:11434
k8stalk auth add --backend anthropic --model claude-sonnet-4-20250514
k8stalk auth add --backend amazonbedrock --model anthropic.claude-sonnet-4-6-v1:0 --region us-east-1
k8stalk auth add --backend customrest --baseurl http://localhost:8000/v1

# List configured backends
k8stalk auth list

# Set default
k8stalk auth default --backend ollama

# Remove
k8stalk auth remove --backend openai
```

### `init` — Setup wizard

The easiest way to get started — interactive guided configuration:

```bash
k8stalk init
```

### Global flags

| Flag | Short | Description |
|------|-------|-------------|
| `--backend` | `-b` | LLM backend to use (overrides config default) |
| `--model` | `-m` | Model for this invocation (overrides config) |
| `--namespace` | `-n` | Kubernetes namespace (default: all namespaces) |
| `--context` | | Kubernetes context (default: current context) |
| `--output` | `-o` | Output format: `text` or `json` (default: `text`) |
| `--no-anonymize` | | Disable data anonymization (only use with local backends) |


## GitOps Awareness (ArgoCD & Flux)

k8stalk automatically registers GitOps diagnostic tools when the relevant environment is detected.

### ArgoCD

Set these environment variables to enable:

```bash
export ARGOCD_SERVER="argocd.example.com:443"
export ARGOCD_AUTH_TOKEN="<your-argocd-api-token>"
```

When configured, the agent gains three tools:
- **`argocd_app_status`** — sync status, health status, last sync time, result message
- **`argocd_app_resources`** — resource tree for an application (maps a failing pod back to its ArgoCD Application)
- **`argocd_app_list`** — enumerate all ArgoCD applications

### Flux

Flux integration requires no additional configuration — Flux state lives in CRDs on the cluster, so these tools are available whenever k8stalk can reach the cluster. The agent gains:

- **`flux_kustomization_status`** — ready condition, last applied revision, reconciliation errors
- **`flux_helmrelease_status`** — release status, last attempted revision, failure messages
- **`flux_source_status`** — GitRepository/OCIRepository fetch status (catches upstream failures before reconciliation)

### Cross-resource correlation

The `correlate_gitops` tool checks whether a failing resource's first-seen failure time falls within a **15-minute window** of the last ArgoCD sync or Flux reconciliation. This is the single highest-value heuristic for incident response in GitOps environments: "did a GitOps sync touch this recently?"


## Data Privacy & Anonymization

k8stalk is designed for privacy by default:

- **Cloud backends**: all data is anonymized before being sent to the LLM. IPs, hostnames, email addresses, and token-like strings are masked with reversible placeholders. The final output shown to you is de-anonymized — the LLM never sees real values.
- **Local backends**: when using Ollama or a `customrest` endpoint on localhost, you can skip anonymization with `--no-anonymize` since nothing leaves your machine.
- **Chat history**: stored locally at `~/.config/k8stalk/history.db` (pure-Go SQLite via `modernc.org/sqlite`). Never transmitted anywhere.
- **Read-only**: k8stalk never modifies your cluster. All Kubernetes API calls are read operations.


## RBAC Requirements

k8stalk needs **read-only** access to the resources it analyzes. Minimum ClusterRole:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: k8stalk-reader
rules:
- apiGroups: [""]
  resources: ["pods", "pods/log", "services", "events", "nodes",
              "namespaces", "persistentvolumeclaims"]
  verbs: ["get", "list"]
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "replicasets"]
  verbs: ["get", "list"]
- apiGroups: ["batch"]
  resources: ["jobs", "cronjobs"]
  verbs: ["get", "list"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses", "networkpolicies"]
  verbs: ["get", "list"]
- apiGroups: ["autoscaling"]
  resources: ["horizontalpodautoscalers"]
  verbs: ["get", "list"]
- apiGroups: ["policy"]
  resources: ["poddisruptionbudgets"]
  verbs: ["get", "list"]
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["mutatingwebhookconfigurations", "validatingwebhookconfigurations"]
  verbs: ["get", "list"]
# Flux CRDs (if Flux is installed)
- apiGroups: ["kustomize.toolkit.fluxcd.io"]
  resources: ["kustomizations"]
  verbs: ["get", "list"]
- apiGroups: ["helm.toolkit.fluxcd.io"]
  resources: ["helmreleases"]
  verbs: ["get", "list"]
- apiGroups: ["source.toolkit.fluxcd.io"]
  resources: ["gitrepositories", "ocirepositories", "helmrepositories"]
  verbs: ["get", "list"]
```

ArgoCD access is handled via ArgoCD's own API token (set via `ARGOCD_AUTH_TOKEN`), not through Kubernetes RBAC.


## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      CLI (cobra)                             │
│  analyze │ diagnose │ chat │ serve │ dump │ auth │ init     │
└────────────────────────────┬────────────────────────────────┘
                             │
            ┌────────────────┼────────────────┐
            │                │                │
     ┌──────▼───────┐ ┌─────▼──────┐  ┌──────▼───────┐
     │ Analyze mode  │ │ Agent Core │  │  Web UI      │
     │ fixed sweep,  │ │ pkg/agent/ │  │  pkg/webui/  │
     │ deterministic │ │ tool loop  │  │  SSE stream  │
     └──────┬───────┘ └─────┬──────┘  └──────┬───────┘
            └────────┬───────┴─────────┬──────┘
                     │                 │
      ┌──────────────▼───┐     ┌───────▼─────────┐
      │  K8s Analyzers    │     │  LLM Providers   │
      │  16 resource types│     │  8 backends      │
      ├──────────────────-┤     ├──────────────────┤
      │  GitOps Adapters  │     │  Sanitizer       │
      │  ArgoCD (3 tools) │     │  pkg/sanitize/   │
      │  Flux (3 tools)   │     └──────────────────┘
      └────────┬──────────┘
               │
      ┌────────▼──────────┐
      │  pkg/k8s/          │
      │  client-go wrapper │
      └───────────────────┘
```

**Key design principle**: every analyzer and GitOps adapter implements the `agent.Tool` interface exactly once. In `analyze` mode they run as a fixed deterministic sweep; in `diagnose`/`chat` mode the same tools are exposed to the LLM so it can select which ones to invoke based on the user's question. One implementation, two consumption modes.


## Roadmap

- [x] First tagged release + Homebrew tap publishing
- [ ] Additional providers for full k8sgpt parity (watsonx, Cohere, SageMaker — stretch goals)
- [ ] ConfigMap/Secret analyzer (detect referenced-but-missing configmaps)
- [ ] Richer web UI (session search, conversation export, tool-call visualization)
- [ ] Helm chart for optional in-cluster deployment
- [ ] Remediation suggestions with copy-pasteable `kubectl` commands

**Non-goals for v1:** no hosted multi-tenant SaaS, no in-cluster relay agent, no automated write actions. Everything is local, read-only, BYO-LLM-key.


## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code structure, and guidelines for adding new analyzers or LLM providers.

```bash
make build       # compile binary
make test        # run tests with race detector
make lint        # golangci-lint
```


## License

[Apache License 2.0](LICENSE)
