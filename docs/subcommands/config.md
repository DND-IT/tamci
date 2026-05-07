# `tamci config`

Read a matrix-config file (`.github/matrix-config.yaml`) and emit a
workflow matrix as JSON, with optional change-detection filtering.

Replaces `action-config`.

## Flags

| Flag                | Default                          | Description                                    |
|---------------------|----------------------------------|------------------------------------------------|
| `--config-path`     | `.github/matrix-config.yaml`     | Path to the configuration file (JSON or YAML). |
| `--dimension`       | _(empty)_                        | Primary dimension override.                    |
| `--target`          | _(empty)_                        | Filter by dimension value(s); comma-separated. |
| `--environment`     | _(empty)_                        | Filter environments; comma-separated.          |
| `--exclude`         | _(empty)_                        | JSON array of patterns to exclude.             |
| `--include`         | _(empty)_                        | JSON array of entries to append.               |
| `--change-detection`| `false`                          | Filter the matrix to entries with changes.     |
| `--summary`         | `true`                           | Write outputs to `GITHUB_STEP_SUMMARY`.        |

## Outputs (via `GITHUB_OUTPUT`)

- `matrix` — JSON array, the full expanded matrix
- `length` — number of entries
- `config` — JSON object keyed by dimension values (for `fromJson()`)
- `changes_detected` — `true`/`false` when `--change-detection` is set
- `config_file` — path actually read
- Plus any "uniform" field across all entries (e.g. `directory`, `aws_region`)

## Example

```bash
tamci config --config-path .github/matrix-config.yaml --target api,frontend
```

See [action-config's README](https://github.com/DND-IT/action-config) for the
full `matrix-config.yaml` schema (will move into this docs site).
