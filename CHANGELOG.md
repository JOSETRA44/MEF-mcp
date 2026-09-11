# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

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
