package amigable

import (
	"context"
	"net/url"

	"github.com/JOSETRA44/mef-mcp/internal/core"
	"github.com/JOSETRA44/mef-mcp/internal/httpx"
)

// timeoutFor gives the slow dimensions room: grouping by meta or finalidad
// without filters takes MEF 15–30 s, sometimes more.
func timeoutFor(q Query) int {
	if q.GroupBy.Slow {
		return 150
	}
	return 60
}

// fetchPage is the single primitive every Consulta Amigable command uses:
// one stateless GET, decoded and parsed. Keeping it in one place means the
// charset repair, the error-page detection and the URL shape cannot drift
// between gasto, serie and entidades.
func (m *Module) fetchPage(ctx context.Context, env *core.Env, q Query) (Page, bool, error) {
	raw := httpx.OrderedQuery(q.Pairs())
	resp, err := env.HTTP.Do(ctx, core.Request{
		Method: "GET", URL: m.Endpoint, Query: url.Values{"_raw": {raw}},
		TTLKey: "amigable/gasto", Timeout: timeoutFor(q), Charset: "mixed",
	})
	if err != nil {
		return Page{}, false, err
	}
	if resp.Status != 200 {
		return Page{}, false, core.NewError(core.CodeAPI, "", "Consulta Amigable answered HTTP %d", resp.Status)
	}
	pg, appErr := ParsePage(string(resp.Body))
	if appErr && len(pg.Lines) == 0 {
		return Page{}, resp.Cached, core.NewError(core.CodeAPI,
			"The navigator raised a server error for this combination; try fewer filters.",
			"Consulta Amigable returned an error page")
	}
	return pg, resp.Cached, nil
}

// fetchAll walks pages until the source is exhausted or maxPages is reached.
// It is used where a command must see every row before answering (sorting,
// searching, totals) and therefore cannot stream.
func (m *Module) fetchAll(ctx context.Context, env *core.Env, q Query, maxPages int) (lines []Line, meta Page, cached bool, truncated bool, err error) {
	cached = true
	q.Page = 1
	for page := 0; page < maxPages; page++ {
		pg, fromCache, err := m.fetchPage(ctx, env, q)
		if err != nil {
			return nil, meta, cached, false, err
		}
		cached = cached && fromCache
		if pg.LastUpdated != "" {
			meta.LastUpdated = pg.LastUpdated
		}
		if pg.Dimension != "" {
			meta.Dimension = pg.Dimension
		}
		lines = append(lines, pg.Lines...)
		if len(pg.Lines) < q.PageSize {
			return lines, meta, cached, false, nil
		}
		q.Page++
		if env.Progress != nil {
			env.Progress("%d filas…", len(lines))
		}
	}
	return lines, meta, cached, true, nil
}
