# dmr-plugin-jenkins

DMR external plugin for **multiple Jenkins instances** (HashiCorp [go-plugin](https://github.com/hashicorp/go-plugin), same model as [dmr-plugin-gitlab](https://github.com/seanly/dmr-plugin-gitlab)). Tool names: `jenkins*` per DMR `docs/tool-naming.md`.

## Tools

| Tool | Description |
|------|-------------|
| `jenkinsInstances` | Lists configured instance `id` and host (no secrets). |
| `jenkinsGetJob` | `GET .../api/json` for a job (full name with folders). |
| `jenkinsListBuilds` | 有 `job`：最近 builds；无 `job`：全局 running + queued（等待资源）。 |
| `jenkinsGetBuild` | Build metadata for a build number. |
| `jenkinsTriggerBuild` | `POST` `build` or `buildWithParameters`. |
| `jenkinsGetConsoleText` | Console log; optional `max_chars` (UTF-8). |

Every tool accepts optional `instance` (string). If omitted, `default_instance` from config is used.

## Configuration

DMR merges `plugins[].config` into `Init` JSON (see DMR `AGENTS.md`). Use **`api_token`** per instance (plain or DMR `enc:` after host unseal).

```toml
[[plugins]]
name = "jenkins"
enabled = true
path = "/opt/dmr/plugins/dmr-plugin-jenkins"

[plugins.config]
default_instance = "ci-prod"

[[plugins.config.instances]]
id = "ci-prod"
base_url = "https://jenkins.prod.example.com"
username = "svc-dmr"
api_token = "enc:..."  # or dev plaintext
verify_tls = true
timeout_seconds = 60

[[plugins.config.instances]]
id = "ci-lab"
base_url = "https://jenkins.lab.local:8080"
username = "dmr"
api_token = "..."
verify_tls = false
timeout_seconds = 30
```

- **`job` parameter**: Jenkins **full name** as in the UI (e.g. `team/android/build`), not only the leaf name when folders exist.
- **全局 in-progress（无 job / job 为空）**：返回 `running`（执行中）与 `queued`（排队中/等待资源）两个列表。可用参数：`include_running`、`include_queued`、`running_limit`、`queue_limit`。若 `/computer` 或 `/queue` 之一失败，另一部分仍会返回，并在 `errors` 里给出对应错误信息。`running` 中若无法从 URL 可靠解析 job/build，会设 `unparsed: true` 并保留 `url`、`full_display_name`、`build_number`（若有）供人工辨认；可解析时会尝试 `fullDisplayName`（如 `a » b #42`）启发式恢复路径式 job 名。

## OPA policy (`jenkins.rego`)

Shipped under `policies/jenkins.rego`. Load it next to the default bundle:

```toml
[[plugins]]
name = "opa_policy"
enabled = true

[plugins.config]
policies = [
    "/path/to/dmr/plugins/opapolicy/policies/default.rego",
    "/path/to/dmr-plugin-jenkins/policies/jenkins.rego"
]
```

Write tools (`jenkinsTriggerBuild`, and Phase 2 names in the set) require **approval** by default. Extend `jenkins_write_tools` when adding new mutating tools.

## Build

From repo root (with `dmr` as sibling `./dmr` for `replace`):

```bash
go build -o dmr-plugin-jenkins .
```

## Develop

```bash
go test ./...
```
