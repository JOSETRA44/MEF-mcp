# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-10-03

Five new tools (16 in total), aimed at the two things the server could not do
before: let an agent navigate from a name to a query, and hand back a
deliverable instead of a table.

### Added

- **`mef_entidades`** — finds a sector, pliego, unidad ejecutora or
  municipalidad by name (accent- and case-insensitive, words in any order, or
  by code) and returns a ready-to-paste `filtro` such as `pliego=011`. Until
  now a named institution meant guessing codes or drilling blindly.
- **`mef_serie`** — the same grouping across a range of fiscal years, with
  year-on-year variation and per-year totals. Years are fetched concurrently;
  it replaces one `mef_gasto` call per year.
- **`mef_analizar`** — conclusions instead of rows: totals and overall avance,
  ranking with each row's share, concentration of the top five, laggards that
  do have budget, and rows with PIM and zero execution. Every figure is
  computed server-side, each finding carrying its own sentence.
- **`mef_reporte`** — writes the deliverable: an Excel workbook (sheet
  *Resumen* with indicators, findings and a native bar chart; *Datos* with the
  full table, frozen header and autofilter; *Fuente* with provenance), a
  self-contained HTML page (inline SVG chart, no external asset, prints to
  PDF) or a Markdown memo. Sources: `gasto`, `serie` or `sql`.
- **`mef_ayuda`** — maps a question in Spanish to the right tools with example
  arguments and the reason for the order; without a question it returns every
  playbook, which doubles as a map of the server. Local and instant.
- **Server-side sorting in `mef_gasto`**: `sort` (devengado, pim, avance,
  nombre…) with `desc`, so `sort` + `max` is a true top-N.
- `MEF_OUTPUT` to choose where reports are written (default
  `$MEF_HOME/output`); `mef cache path` prints it.
- New internal layers: `internal/analysis` (statistics and findings) and
  `internal/report` (xlsx/html/md writers and the SVG chart).

### Changed

- Reports and analyses run the existing commands through a capturing sink, so
  a report of spending is the output of `mef_gasto` rather than a second
  implementation that could disagree with it.
- The Consulta Amigable commands share one `fetchPage` primitive, so charset
  repair, error-page detection and URL shape cannot drift between them.

### Fixed

- Concentration of the top five was adding rounded shares, which could total
  100.1 %. It is now computed from the raw amounts.
- Multi-word entity searches matched nothing: the dimension-alias normaliser
  (which turns spaces into dashes) was being used on free text.

## [0.1.0] - 2026-09-11

### Added

- `mef` CLI and `mef-mcp` stdio server as two Go binaries built from one
  command registry: flags, MCP tool schemas and the `describe --json` manifest
  are derived from a single declaration per command.
- **Consulta Amigable** module (`mef amigable gasto`, `mef_gasto`): daily
  budget execution — PIA, PIM, certificación, compromiso, devengado, girado,
  avance % — for 1999–today, grouped and filtered by 29 dimensions, through
  the navigator's stateless export URL. Handles the service's mixed
  UTF-8/Windows-1252 responses and budget-less time dimensions.
- **Datos Abiertos** module: catalog search, dataset detail, datastore reads
  with exact-match filters (errors list the valid columns), streaming file
  downloads.
- **Invierte.pe / SSI** module: project sheet by CUI or SNIP, name search, and
  execution by executor, year, month, funding source and classifier, plus SEACE
  contracts and InfoObras works.
- Local SQLite store (pure Go, no CGO): `mef sync`, read-only `mef sql`,
  `mef tablas` with column profiling and provenance.
- ARSENAL-SPEC contract: five output formats, JSON envelope, seven exit codes,
  cross-process disk cache with `--refresh`/`--no-cache`, `--all`/`--max`,
  `mef doctor`.
- MCP resources (`mef://guia`, `mef://dimensiones`, `mef://catalogo`) and
  prompts (`analisis_ejecucion_presupuestal`, `ficha_inversion`,
  `comparar_regiones`).
