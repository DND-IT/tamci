# `tamci summary`

Read input text or a file, optionally pretty-print nested JSON, and
append it to `GITHUB_STEP_SUMMARY`.

Replaces `action-summary`.

## Flags

| Flag               | Default      | Description                                                     |
|--------------------|--------------|-----------------------------------------------------------------|
| `--string`         | _(empty)_    | Input string. **Mutually exclusive with `--path`.**             |
| `--path`           | _(empty)_    | Path to a file containing text. **Mutually exclusive with `--string`.** |
| `--max-size`       | `1048576`    | Max output size in bytes (1 MiB GitHub limit).                  |
| `--summary-header` | `Summary`    | Markdown `## ` header.                                          |
| `--data-type`      | _(empty)_    | Code-block language tag (e.g. `json`, `yaml`, `bash`).          |

## JSON pretty-printing

If the input parses as JSON, it's pretty-printed with 2-space indent
**and** any string values that themselves parse as JSON are recursively
deserialized. Useful for surfacing nested escaped JSON from upstream
steps.

## Behavior when `GITHUB_STEP_SUMMARY` is unset

Output goes to stdout instead of a file — useful for local testing.

## Example

```bash
tamci summary --string '{"service":"api","version":"1.4.0"}' \
              --summary-header 'Deploy result' \
              --data-type json
```

Renders as:

````markdown
## Deploy result
<details><summary>Click to expand</summary>

```json
{
  "service": "api",
  "version": "1.4.0"
}
```
</details>
````
