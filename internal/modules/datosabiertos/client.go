// Package datosabiertos reaches MEF's open-data portal (datosabiertos.mef.gob.pe).
//
// The portal is an Angular SPA with no documented API. Two families of
// endpoints sit behind it, both taken from its own JS bundle:
//
//   - catalog: POST /Rest/PortalWebDatasets/v1.0/getDatasets and
//     /Rest/PortalWebDatasetDetalle/v1.0/getDatasetDetalle (PostgreSQL-backed);
//   - data: GET api.datosabiertos.mef.gob.pe/DatosAbiertos/v1/datastore_search,
//     a CKAN look-alike served by a Perl wrapper with its own error shape.
//
// Both are undocumented and may change without notice; docs/SOURCES.md
// records exactly what was observed and when.
package datosabiertos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// Endpoints groups the upstream URLs so tests can point them at a fake server.
type Endpoints struct {
	Search    string
	Detail    string
	Datastore string
	Files     string // allowed download host prefix
}

// Production endpoints.
var Production = Endpoints{
	Search:    "https://datosabiertos.mef.gob.pe/Rest/PortalWebDatasets/v1.0/getDatasets",
	Detail:    "https://datosabiertos.mef.gob.pe/Rest/PortalWebDatasetDetalle/v1.0/getDatasetDetalle",
	Datastore: "https://api.datosabiertos.mef.gob.pe/DatosAbiertos/v1/datastore_search",
	Files:     "https://fs.datosabiertos.mef.gob.pe/",
}

// Categories are the six portal categories (slug → title), verified live.
var Categories = []struct{ Slug, Title string }{
	{"presupuesto-publico", "Presupuesto Público"},
	{"inversion-publica", "Inversión Pública"},
	{"contabilidad-publica", "Contabilidad Pública"},
	{"endeudamiento-y-tesoro-publico", "Endeudamiento y Tesoro Público"},
	{"recursos-humanos-del-sector-publico", "Recursos Humanos del Sector Público"},
	{"abastecimiento-del-sector-publico", "Abastecimiento del Sector Público"},
}

var reUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// searchResponse mirrors getDatasets.
type searchResponse struct {
	CountTotal string `json:"countTotal"`
	PagesCount int    `json:"pagesCount"`
	Page       int    `json:"page"`
	Datasets   []struct {
		Slug        string `json:"dataset_id"`
		Title       string `json:"dataset"`
		Description string `json:"description"`
		Formats     string `json:"formats"`
		Tags        string `json:"tags"`
	} `json:"datasets"`
}

// detailResponse mirrors getDatasetDetalle.
type detailResponse struct {
	Category struct {
		Slug        string `json:"category"`
		Title       string `json:"category_title"`
		Description string `json:"category_description"`
	} `json:"category_detail"`
	Dataset struct {
		UUID        string `json:"dataset_id"`
		Slug        string `json:"dataset_name_id"`
		Title       string `json:"dataset"`
		Description string `json:"description"`
		Created     string `json:"metadata_created"`
		Modified    string `json:"metadata_modified"`
	} `json:"dataset_detail"`
	Resources []struct {
		ID          string `json:"resource_id"`
		Title       string `json:"resource_title"`
		Format      string `json:"resource_format"`
		URL         string `json:"resource_url"`
		ZipURL      string `json:"resource_download_url_zip"`
		Description string `json:"resource_description"`
	} `json:"resources"`
	Tags []struct {
		Tag string `json:"tag"`
	} `json:"tags"`
}

func (m *Module) search(ctx context.Context, env *core.Env, text, category string, page int) (*searchResponse, bool, error) {
	cats := []string{}
	if category != "" {
		cats = append(cats, category)
	}
	body := map[string]any{"search": text, "sort": nil, "tags": []string{}, "formats": []string{}, "categories": cats, "page": page}
	resp, err := env.HTTP.Do(ctx, core.Request{Method: "POST", URL: m.EP.Search, JSON: body, TTLKey: "datosabiertos/catalog"})
	if err != nil {
		return nil, false, err
	}
	if resp.Status != 200 {
		return nil, false, core.NewError(core.CodeAPI, "", "catalog search answered HTTP %d", resp.Status)
	}
	var sr searchResponse
	if err := json.Unmarshal(resp.Body, &sr); err != nil {
		return nil, false, core.Wrap(core.CodeAPI, err, "", "catalog search returned unexpected JSON")
	}
	return &sr, resp.Cached, nil
}

func (m *Module) detail(ctx context.Context, env *core.Env, slug string) (*detailResponse, bool, error) {
	resp, err := env.HTTP.Do(ctx, core.Request{Method: "POST", URL: m.EP.Detail, JSON: map[string]string{"dataset": slug}, TTLKey: "datosabiertos/catalog"})
	if err != nil {
		return nil, false, err
	}
	if resp.Status != 200 {
		return nil, false, core.NewError(core.CodeAPI, "", "dataset detail answered HTTP %d", resp.Status)
	}
	var dr detailResponse
	if err := json.Unmarshal(resp.Body, &dr); err != nil {
		return nil, false, core.Wrap(core.CodeAPI, err, "", "dataset detail returned unexpected JSON")
	}
	if dr.Dataset.Slug == "" && len(dr.Resources) == 0 {
		return nil, resp.Cached, core.NotFoundf("Find slugs with `mef datos buscar <texto>` (column dataset).", "dataset %q not found", slug)
	}
	return &dr, resp.Cached, nil
}

// DatastorePage is one decoded datastore_search page.
type DatastorePage struct {
	Rows    []core.Row
	Columns []string
	Null    bool // upstream said records:null
	Cached  bool
	URL     string
}

var reSelectCols = regexp.MustCompile(`SELECT\s+(.*?)\s+FROM`)

// Datastore fetches one page. Filters are exact-match column equality.
func (m *Module) Datastore(ctx context.Context, env *core.Env, rid string, filters [][2]string, q string, limit, offset int) (*DatastorePage, error) {
	if !reUUID.MatchString(rid) {
		return nil, core.Usagef("resource_id is a UUID, e.g. 5f3b3cbe-3955-41cc-8662-1757ebb5cf53. List them with `mef datos dataset <slug>`.", "invalid resource_id %q", rid)
	}
	qs := url.Values{"resource_id": {rid}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if len(filters) > 0 {
		f := map[string]string{}
		for _, kv := range filters {
			f[kv[0]] = kv[1]
		}
		b, _ := json.Marshal(f)
		qs.Set("filters", string(b))
	}
	if q != "" {
		qs.Set("q", q)
	}
	resp, err := env.HTTP.Do(ctx, core.Request{Method: "GET", URL: m.EP.Datastore, Query: qs, TTLKey: "datosabiertos/datastore", Timeout: 180})
	if err != nil {
		return nil, err
	}
	if resp.Status != 200 {
		// The Perl wrapper explains failures in the body of a 4xx/5xx (e.g.
		// 409 carrying SQL error 42703 and the valid column list), so the
		// body is decoded before falling back to the bare status.
		if _, err := decodeDatastore(resp.Body, rid, resp.Cached, resp.URL); err != nil {
			return nil, err
		}
		if resp.Status == 404 {
			return nil, core.NotFoundf("", "resource %s not found", rid)
		}
		return nil, core.NewError(core.CodeAPI, "", "datastore answered HTTP %d", resp.Status)
	}
	return decodeDatastore(resp.Body, rid, resp.Cached, resp.URL)
}

// perlError is the wrapper's failure document.
type perlError struct {
	Cod json.RawMessage `json:"oCod"`
	Des json.RawMessage `json:"oDes"`
}

func decodeDatastore(body []byte, rid string, cached bool, src string) (*DatastorePage, error) {
	var pe perlError
	_ = json.Unmarshal(body, &pe)
	if len(pe.Cod) > 0 {
		code := strings.Trim(string(pe.Cod), `"`)
		var details []struct {
			Err string `json:"err"`
			SQL string `json:"sql"`
		}
		_ = json.Unmarshal(pe.Des, &details)
		for _, d := range details {
			switch d.Err {
			case "42703": // undefined_column: the leaked SELECT lists every valid column.
				hint := "Filter keys are exact column names (case-sensitive)."
				if m := reSelectCols.FindStringSubmatch(d.SQL); m != nil {
					cols := strings.Split(strings.ReplaceAll(m[1], `"`, ""), ",")
					hint = "Valid columns: " + strings.Join(cols, ", ")
				}
				return nil, core.Usagef(hint, "unknown column in filters for resource %s", rid)
			case "42P01": // undefined_table
				return nil, core.NotFoundf("List resources with `mef datos dataset <slug>`.", "resource %s does not exist in the datastore", rid)
			case "22P02", "42883": // invalid input syntax / operator
				return nil, core.Usagef("Filter values are compared as text; check the value format.", "invalid filter value for resource %s", rid)
			}
		}
		if code == "404" {
			return nil, core.NotFoundf("", "resource %s not found", rid)
		}
		var msg string
		_ = json.Unmarshal(pe.Des, &msg)
		if msg == "" && len(details) > 0 {
			msg = "SQL error " + details[0].Err
		}
		return nil, core.NewError(core.CodeAPI, "", "datastore error %s: %s", code, msg)
	}
	var env struct {
		Records json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, core.Wrap(core.CodeAPI, err, "", "datastore returned unexpected JSON")
	}
	page := &DatastorePage{Cached: cached, URL: src}
	if len(env.Records) == 0 || string(env.Records) == "null" {
		page.Null = true
		return page, nil
	}
	rows, cols, err := decodeOrdered(env.Records)
	if err != nil {
		return nil, core.Wrap(core.CodeAPI, err, "", "datastore records are malformed")
	}
	page.Rows, page.Columns = rows, cols
	return page, nil
}

// decodeOrdered decodes an array of flat objects and returns their columns
// sorted. The Perl wrapper emits keys in hash order, which changes between
// responses, so "source order" does not exist; sorting keeps output (and the
// schema of synced tables) deterministic and pairs X with X_NOMBRE.
func decodeOrdered(raw json.RawMessage) ([]core.Row, []string, error) {
	rows, cols, err := decodeRecords(raw)
	sort.Strings(cols)
	return rows, cols, err
}

func decodeRecords(raw json.RawMessage) ([]core.Row, []string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, nil, fmt.Errorf("records is not an array")
	}
	var rows []core.Row
	var cols []string
	for dec.More() {
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			return nil, nil, fmt.Errorf("record is not an object")
		}
		row := core.Row{}
		first := rows == nil
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, nil, err
			}
			key, _ := kt.(string)
			var v any
			if err := dec.Decode(&v); err != nil {
				return nil, nil, err
			}
			if n, ok := v.(json.Number); ok {
				v = n.String()
			}
			row[key] = v
			if first {
				cols = append(cols, key)
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
	}
	return rows, cols, nil
}
