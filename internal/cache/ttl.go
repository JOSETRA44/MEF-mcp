package cache

import "time"

// TTLFor decides how long an upstream response stays fresh, given the
// request's TTL key "<module>/<command>" (e.g. "amigable/gasto",
// "datosabiertos/catalog", "datosabiertos/datastore", "invierte/ficha").
//
// This is a policy choice, not a mechanical one — it trades freshness against
// load on MEF's servers:
//   - Consulta Amigable is refreshed once a day (the page says "Última
//     actualización: <yesterday>"), so anything under a few hours re-downloads
//     data that cannot have changed.
//   - The Datos Abiertos catalog changes a few times a year.
//   - datastore resources are republished monthly or less.
//   - Invierte.pe / SSI project sheets move daily during execution.
//
// TODO(user): replace this flat default with a per-source policy. A zero
// return disables caching for that key.
func TTLFor(key string) time.Duration {
	return 6 * time.Hour
}
