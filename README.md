# dmr-plugin-jenkins

DMR external plugin for **multiple Jenkins instances** (HashiCorp [go-plugin](https://github.com/hashicorp/go-plugin), same model as [dmr-plugin-gitlab](https://github.com/seanly/dmr-plugin-gitlab)). Tool names: `jenkins*` per DMR `docs/tool-naming.md`.

## Tools

| Tool | Description |
|------|-------------|
| `jenkinsInstances` | Lists configured instance `id` and host (no secrets). |
| `jenkinsGetJob` | `GET .../api/json` for a job (full name with folders). |
| `jenkinsSearchJobs` | Search autocomplete via core `search/suggest` (`query` required; optional `folder` scopes under that full name). Results are suggestions—not guaranteed job full names; confirm with `jenkinsGetJob`. |
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

Uses standard layout: **`cmd/dmr-plugin-jenkins`** (main) + **`internal/jenkins`** (plugin).

For local sibling **`dmr`**, copy **`go.work.example`** to **`go.work`** (see **`.gitignore`**). The workspace links this module with `../dmr` instead of editing `replace` directives.

```bash
cp go.work.example go.work   # adjust ../dmr path if needed
go build -o bin/dmr-plugin-jenkins ./cmd/dmr-plugin-jenkins
# or: make build
```

Pin `github.com/seanly/dmr` after API changes (against sibling checkout):

```bash
make bump-dmr
# Optional: make bump-dmr DMR_DIR=/path/to/dmr
```

Uses `GOPRIVATE` / `GONOSUMDB` for `github.com/seanly/dmr` during `go get` when tidy cannot reach the public checksum DB.

## Install

After **`make build`**, install the plugin binary into DMR’s plugin directory (default **`~/.dmr/plugins`**), matching `plugins[].path` in config:

```bash
make install
# Override destination: make install INSTALL_DIR=/opt/dmr/plugins
```

Install the bundled OPA policy next to other Rego bundles (default **`~/.dmr/etc/policies`**). The target sets **`0700`** on the directory and **`0600`** on `jenkins.rego` so DMR `opa_policy` hot reload does not block on permissions (see `plugins/opapolicy/reload.go`).

```bash
make install-policy
# Override: make install-policy POLICY_DIR=/path/to/policies
```

Both:

```bash
make install-all
```

Run **`make help`** for a short target summary.

## Develop

```bash
go test ./...
```
