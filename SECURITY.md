# Security

## Reporting a vulnerability

Open a private security advisory on GitHub
(`Security → Report a vulnerability` in the repository) or email
drivearchiver@gmail.com. Please include the command or tool call, the version
(`mef --version`) and what you observed. You will get a reply within a week.

## What the server sends, and where

mef-mcp only makes outbound HTTPS requests to public services of Peru's
Ministerio de Economía y Finanzas:

| Host | Used by | What is sent |
|---|---|---|
| `apps5.mineco.gob.pe` | `mef_gasto`, `mef doctor` | The year, spending type and the dimension codes you query |
| `datosabiertos.mef.gob.pe` | `mef_datos_buscar`, `mef_datos_dataset` | Your search text or a dataset slug |
| `api.datosabiertos.mef.gob.pe` | `mef_datos_consultar`, `mef_sync` | A resource id, filters and a free-text search |
| `fs.datosabiertos.mef.gob.pe` | `mef datos descargar` (CLI only) | A file request |
| `ofi5.mef.gob.pe` | `mef_inversion_*` | A CUI/SNIP code or a project-name search |

No credentials exist, none are read from the environment, and no telemetry is
sent anywhere. Requests carry a `User-Agent` of `mef-mcp/<version>`.

## Local state

- **Cache** — upstream responses are stored as JSON files under
  `MEF_HOME/cache` (see `mef cache path`). They contain only public MEF data.
  `mef cache clear` deletes them.
- **Database** — `mef_sync` writes public datasets to `MEF_HOME/mef.db`.

## Hardening in place

- `mef_sql` opens the database with SQLite's `query_only` pragma, so writes are
  refused by the engine itself, not just by a keyword check; multiple
  statements are rejected.
- Table names given to `mef_sync` are validated identifiers; column names from
  upstream data are always quoted.
- `mef datos descargar` only downloads from `fs.datosabiertos.mef.gob.pe`.
- Parameters are validated against declared types, enums and bounds before any
  request is made; unknown parameters are rejected.
- Per-host concurrency and request spacing keep load on MEF's servers low.
