<div align="center">

# Slaw

[![CI](https://github.com/SandwichLabs/slaw/actions/workflows/ci.yml/badge.svg)](https://github.com/SandwichLabs/slaw/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**The SandwichLabs analysis workspace.**

*by [Sandwich Labs](https://github.com/SandwichLabs)*

</div>

---

Slaw is a desktop app for working with small and medium-sized datasets, on your machine or in the cloud. It manages your connections and keeps credentials in your OS keychain. Your notebooks, saved queries and charts are plain files in a folder you can keep in git and share with your team. The Slaw assistant writes queries and builds notebooks for you; it shows its work as it goes and can create new data products with your permission. Claude Code and other MCP clients get the same tools with the same limits.

<p align="center">
  <a href="docs/assets/hero.mp4">
    <img src="docs/assets/hero.gif" alt="Slaw opening a notebook: stat cards and a bar chart render from live SQL, then a monthly revenue line chart and a result table as the notebook scrolls" width="880">
  </a>
</p>

<details>
<summary>Still screenshot</summary>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/hero-notebook-dark.png">
  <img src="docs/assets/hero-notebook.png" alt="A Slaw notebook with revenue stat cards, a bar chart of revenue by shop and a monthly revenue line chart" width="880">
</picture>

</details>

## Install

```sh
curl -fsSL https://sandwichlabs.dev/slaw/install.sh | sh
```

macOS (Apple silicon 14+, Intel 13+) and Linux (x86-64, arm64). Installs `slaw` and Slaw into your home folder, no `sudo`. Options, manual download, macOS notes and uninstall: [docs/install.md](docs/install.md).

## 60-second quickstart

```sh
slaw workspace init sales-demo && cd sales-demo        # a folder with slaw.yaml, notebooks/, data/ ...
printf 'customer,amount\nAcme,120.5\nBirch,80\nAcme,45.25\n' > data/orders.csv
slaw import data/orders.csv --into orders                # load it into the workspace's DuckDB
slaw query "SELECT customer, sum(amount) AS total FROM orders GROUP BY 1 ORDER BY 2 DESC"
claude mcp add --scope user slaw-sales-demo -- "$(command -v slaw)" mcp serve --workspace "$PWD"   # Claude Code over MCP
slaw studio --gui                                        # open the desktop app on this workspace
```

Studio holds a workspace's database while it is open, and its MCP server is off until you press **Start** once in Settings, Workspace, Claude and other tools; Studio remembers that for this workspace on this computer and turns it on each time it opens the workspace. If you want Claude Code and Studio at the same time, press Start (writes then show approval cards in Studio); otherwise quit Studio before Claude Code connects.

No accounts or API keys needed. The full walkthrough, with notebooks and MCP, is the [Quickstart](docs/quickstart.md).

## What you get

- [Connection management](docs/connections.md)
- [Notebooks, saved queries, live views and pages](docs/reference/artifacts.md)
- [Semantic metric definitions](docs/reference/semantics.md)
- [The Slaw assistant](docs/reference/agent-policy.md)
- [Bring your own AI over MCP](docs/mcp-clients.md)

## Supported platforms

| Platform | Support |
|---|---|
| Linux x86-64 / arm64 | glibc 2.35+ (Ubuntu 22.04+, Debian 12+, Fedora 36+). Studio needs WebKitGTK 4.1 and GTK 3. |
| macOS, Apple silicon | macOS 14 or later |
| macOS, Intel | macOS 13 or later |
| Windows | Not supported yet |

## Status

Slaw is **pre-1.0**. Studio, the command line and MCP all work on workspace folders with connections and keychain credentials. Known limits for 0.1.0:

- **macOS builds are unsigned** (ad-hoc signed, not notarised). The install script avoids the Gatekeeper prompt; a browser download needs **Open Anyway**. See [docs/install.md](docs/install.md#macos-and-gatekeeper).
- No Windows build. Single-user: sharing is through shared backends and git.
- The Slaw assistant needs a model you configure; a local model is the default. DuckDB extensions download once, when you ask for a feature that needs one.

See the [CHANGELOG](CHANGELOG.md) and [docs/direction.md](docs/direction.md) for what is next.

## The name

Slaw was called Vibe Studio until v0.6.0.

## Privacy

Slaw sends nothing to Sandwich Labs: no telemetry, no analytics, no update checks. Network traffic goes only to the data sources you connect, to the LLM provider you configure (which sees the prompts, schemas and query results the assistant uses), and to `extensions.duckdb.org` when you ask for a feature that needs a DuckDB extension (Install, `--install`, or an Excel import or export). Details: [docs/privacy.md](docs/privacy.md).

## Documentation

| | |
|---|---|
| [Install](docs/install.md) | The installer, platforms, macOS, upgrade and uninstall |
| [Quickstart](docs/quickstart.md) | Install to a query, a notebook and Claude Code |
| [Connection recipes](docs/connections.md) | MotherDuck, Postgres, S3 and the rest |
| [Use from Claude and other MCP clients](docs/mcp-clients.md) | Setup, sandbox tiers and approvals |
| [Troubleshooting](docs/troubleshooting.md) | Common problems and how to report a bug |
| [Privacy](docs/privacy.md) | What leaves your machine |
| [Architecture](docs/architecture.md) | How the pieces fit |
| [Reference](docs/reference/README.md) | The shipped surface in detail |
| [Direction](docs/direction.md) | The north star and what changed from the plan |
| [All docs](docs/README.md) | The full index |

## Contributing, security and licence

- Build from source and contribute: [CONTRIBUTING.md](CONTRIBUTING.md) (DCO sign-off, no CLA). Be kind: [Code of Conduct](CODE_OF_CONDUCT.md).
- Report a vulnerability privately through GitHub: [SECURITY.md](SECURITY.md).
- Licensed under the [Apache License 2.0](LICENSE); see [NOTICE](NOTICE).
