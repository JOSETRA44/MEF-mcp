// Package invierte reaches the Sistema de Seguimiento de Inversiones (SSI) of
// Invierte.pe, MEF's public-investment tracker (ofi5.mef.gob.pe/ssi).
//
// The SSI page is driven by JSON endpoints under /invierteWS that answer
// form-encoded POSTs. Absent projects come back as an empty array, not a 404.
// Dates use Microsoft's legacy "/Date(ms)/" JSON form.
package invierte

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// DefaultBase is the production web-service root.
const DefaultBase = "https://ofi5.mef.gob.pe/invierteWS"

// Module implements core.Module.
type Module struct{ Base string }

// New returns the module pointed at production.
func New() *Module { return &Module{Base: DefaultBase} }

func (m *Module) ID() string      { return "invierte" }
func (m *Module) Title() string   { return "Invierte.pe — Seguimiento de Inversiones (SSI)" }
func (m *Module) BaseURL() string { return m.Base }

func (m *Module) Commands() []*core.Command {
	return []*core.Command{m.ficha(), m.buscar(), m.detalle()}
}

var reMSDate = regexp.MustCompile(`^/Date\((-?\d+)\)/$`)

// idKeys are identifiers that arrive as JSON numbers but must render as
// codes: "2081467", not "2,081,467" or 2.081467e+06.
var idKeys = map[string]bool{"CODIGO_UNICO": true, "COD_UNICO": true, "COD_SNIP": true, "SEC_EJEC": true, "ID_PRODUCTO": true, "UNIDAD_OPMI": true, "COD_UNIDAD_UF": true, "COD_UNIDAD_UEI": true}

// normalizeKey applies idKeys before normalizeValue.
func normalizeKey(k string, v any) any {
	if f, ok := v.(float64); ok && idKeys[k] {
		if f == 0 {
			return nil
		}
		return strconv.FormatInt(int64(f), 10)
	}
	return normalizeValue(v)
}

// normalizeValue converts /Date(ms)/ to ISO dates and trims padding.
func normalizeValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if m := reMSDate.FindStringSubmatch(s); m != nil {
		ms, _ := strconv.ParseInt(m[1], 10, 64)
		return time.UnixMilli(ms).In(time.FixedZone("PET", -5*3600)).Format("2006-01-02")
	}
	return strings.TrimSpace(s)
}

// post calls one SSI endpoint and decodes a JSON array of objects.
func (m *Module) post(ctx context.Context, env *core.Env, path string, form url.Values, ttl string) ([]map[string]any, bool, error) {
	resp, err := env.HTTP.Do(ctx, core.Request{Method: "POST", URL: m.Base + path, Form: form, TTLKey: ttl, Timeout: 60})
	if err != nil {
		return nil, false, err
	}
	if resp.Status != 200 {
		return nil, false, core.NewError(core.CodeAPI, "", "SSI %s answered HTTP %d", path, resp.Status)
	}
	body := strings.TrimSpace(string(resp.Body))
	if strings.HasPrefix(body, "<") {
		return nil, false, core.NewError(core.CodeAPI, "The SSI web service rejected the parameters or is down; run `mef doctor`.", "SSI %s returned an HTML error page", path)
	}
	var out []map[string]any
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, false, core.Wrap(core.CodeAPI, err, "", "SSI %s returned unexpected JSON", path)
	}
	for _, rec := range out {
		for k, v := range rec {
			rec[k] = normalizeKey(k, v)
		}
	}
	return out, resp.Cached, nil
}

// compact drops empty placeholders (null, 0, "") that SSI pads every record
// with, keeping the payload readable for humans and models alike.
func compact(rec map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range rec {
		switch t := v.(type) {
		case nil:
			continue
		case string:
			if t == "" {
				continue
			}
		case float64:
			if t == 0 {
				continue
			}
		}
		out[k] = v
	}
	return out
}

func parseCUI(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err != nil || n <= 0 || len(s) > 9 {
		return "", core.Usagef("A CUI (código único de inversiones) is a 7-digit number, e.g. 2194487. Find one with `mef invierte buscar <nombre>`.", "invalid CUI %q", s)
	}
	return s, nil
}

// fichaFields maps SSI keys to stable, documented row columns.
var fichaFields = [][2]string{
	{"cui", "CODIGO_UNICO"}, {"snip", "COD_SNIP"}, {"nombre", "NOMBRE_INVERSION"}, {"estado", "ESTADO"},
	{"situacion", "SITUACION"}, {"tipo", "TIPO_FORMATO"}, {"marco", "MARCO"}, {"nivel", "NIVEL"},
	{"sector", "SECTOR"}, {"entidad", "ENTIDAD"}, {"funcion", "FUNCION"}, {"programa", "DES_PROGRAMA"},
	{"subprograma", "DES_SUB_PROGRAMA"}, {"opmi", "NOMBRE_OPMI"}, {"unidad_formuladora", "DES_UNIDAD_UF"},
	{"unidad_ejecutora_inversiones", "DES_UNIDAD_UEI"}, {"modalidad", "MODAL_EJEC"},
	{"monto_viable", "MTO_VIABLE"}, {"costo_actualizado", "COSTO_ACTUALIZADO"},
	{"devengado_acumulado", "DEV_ACUMULADO"}, {"pim_anio", "PIM_ANO_VIGENTE"}, {"devengado_anio", "DEV_ANO_VIGENTE"},
	{"avance_fisico", "AVAN_FISICO"}, {"beneficiarios", "BENEFICIARIO"},
	{"fecha_registro", "FEC_REGISTRO"}, {"fecha_viabilidad", "FEC_VIABLE"},
	{"tiene_expediente", "ET_REGISTRADO"}, {"tiene_f12b", "TIENE_F12B"}, {"cierre_registrado", "CIERRE_REGISTRADO"},
}

func fichaColumns() []string {
	cols := make([]string, len(fichaFields))
	for i, f := range fichaFields {
		cols[i] = f[0]
	}
	return cols
}

func (m *Module) ficha() *core.Command {
	return &core.Command{
		Path:    []string{"invierte", "ficha"},
		Tool:    "mef_inversion_ficha",
		Title:   "Ficha de una inversión pública (Invierte.pe)",
		Summary: "Public investment project sheet by CUI: status, cost, viability, executor, accrued spending",
		Description: `Returns the official Invierte.pe tracking sheet (SSI) of one public investment by its CUI (código único de inversiones): name, status (e.g. ACTIVO, CERRADO, DESACTIVADO PERMANENTE), viability, sector and entity, function/programme, OPMI and formulating unit, execution modality, viable amount, updated cost, accrued spending to date and for the current year, physical progress, beneficiaries and key dates.

Use it when the question is about a specific project ("¿en qué va el hospital X?") and you have or can find its CUI; use mef_inversion_buscar to get a CUI from a name, and mef_inversion_detalle for its spending by year, month, executor or funding source, SEACE contracts and InfoObras works. Set "snip" true if the number is an old SNIP code rather than a CUI.

Behaviour: one request, ~0.3 s, cached for hours; a single row, with the full raw SSI record in data. Unknown CUIs fail with not_found (exit 3). Amounts are soles. Not for aggregate budget execution by sector or region — use mef_gasto.`,
		Params: []core.Param{
			{Name: "cui", Type: core.TypeString, Positional: true, Required: true, Help: `Código único de inversiones, e.g. "2194487".`},
			{Name: "snip", Type: core.TypeBool, Default: false, Help: "Treat the number as a legacy SNIP code instead of a CUI."},
		},
		RowFields: fichaColumns(),
		Network:   true,
		ReadOnly:  true,
		Examples:  []string{"mef invierte ficha 2194487", "mef invierte ficha 2194487 -f json"},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			cui, err := parseCUI(in.Str("cui"))
			if err != nil {
				return nil, err
			}
			tipo := "SIAF"
			if in.Bool("snip") {
				tipo = "SNIP"
			}
			recs, cached, err := m.post(ctx, env, "/Ssi/traeDetInvSSI", url.Values{"id": {cui}, "tipo": {tipo}}, "invierte/ficha")
			if err != nil {
				return nil, err
			}
			if len(recs) == 0 || recs[0]["NOMBRE_INVERSION"] == nil {
				return nil, core.NotFoundf("Check the number, or search by name with `mef invierte buscar <texto>`. Use --snip for legacy SNIP codes.", "no investment found for %s %s", strings.ToLower(tipo), cui)
			}
			rec := recs[0]
			row := core.Row{}
			for _, f := range fichaFields {
				if v, ok := rec[f[1]]; ok && v != nil {
					row[f[0]] = v
				}
			}
			if err := sink.Emit(row); err != nil {
				return nil, err
			}
			return &core.Result{
				Columns: fichaColumns(),
				Data:    compact(rec),
				Meta: map[string]any{
					"upstream_cached": cached,
					"portal_url":      "https://ofi5.mef.gob.pe/ssi/Ssi/Index?codigo=" + cui + "&tipo=2",
				},
			}, nil
		},
	}
}

func (m *Module) buscar() *core.Command {
	return &core.Command{
		Path:    []string{"invierte", "buscar"},
		Tool:    "mef_inversion_buscar",
		Title:   "Buscar inversiones por nombre",
		Summary: "Find public investments whose name contains a phrase, returning CUIs",
		Description: `Searches Invierte.pe investments whose name contains the given phrase and returns their CUI, name and status (ACTIVO, DESACTIVADO TEMPORAL/PERMANENTE…).

Use it to turn a description ("hospital de Ayacucho", "puente") into a CUI, then call mef_inversion_ficha for cost and execution or mef_inversion_detalle for history. Behaviour: contiguous substring match on the investment name, so distinctive words work best ("HOSPITAL AYACUCHO" misses "HOSPITAL DE AYACUCHO"). MEF returns at most 20 matches per search and no amounts, so add a place name to narrow a common word ("carretera acos vinchos"). One request, ~0.3 s, cached for hours. An empty result means no name contains that exact phrase; retry with fewer words.`,
		Params: []core.Param{
			{Name: "text", Type: core.TypeString, Positional: true, Required: true, Help: `Phrase contained in the investment name, e.g. "hospital de ayacucho".`},
			{Name: "max", Type: core.TypeInt, Default: core.DefaultMax, Min: core.IntPtr(1), Max: core.IntPtr(100000), Help: "Maximum rows, e.g. 50."},
		},
		RowFields: []string{"cui", "nombre", "estado", "situacion", "marco"},
		Network:   true,
		ReadOnly:  true,
		Examples:  []string{`mef invierte buscar "hospital de ayacucho"`, `mef invierte buscar puente --max 20 -f csv`},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			text := strings.TrimSpace(in.Str("text"))
			if len([]rune(text)) < 3 {
				return nil, core.Usagef("Use at least 3 characters.", "search text %q is too short", text)
			}
			recs, cached, err := m.post(ctx, env, "/Dashboard/busInvNombreSSI", url.Values{"des_inv": {text}, "tipo": {"NOM"}}, "invierte/buscar")
			if err != nil {
				return nil, err
			}
			for _, rec := range recs {
				row := core.Row{"cui": rec["CODIGO_UNICO"], "nombre": rec["NOMBRE_INVERSION"]}
				for col, key := range map[string]string{"estado": "ESTADO", "situacion": "SITUACION", "marco": "MARCO", "sector": "SECTOR", "entidad": "ENTIDAD", "costo_actualizado": "COSTO_ACTUALIZADO", "devengado_acumulado": "DEV_ACUMULADO"} {
					if v := rec[key]; v != nil && v != "" && v != 0.0 {
						row[col] = v
					}
				}
				if err := sink.Emit(row); err != nil {
					return nil, err
				}
			}
			res := &core.Result{Columns: []string{"cui", "nombre", "estado", "situacion", "marco"}, Meta: map[string]any{"upstream_cached": cached, "matched": len(recs)}}
			if len(recs) == 0 {
				res.Notes = append(res.Notes, "no match: the search is a contiguous substring — try fewer words")
			}
			return res, nil
		},
	}
}

// views maps detalle views to SSI endpoints and parameters.
var views = map[string]struct {
	Path string
	Tipo string
	Help string
}{
	"ejecutora":  {"/Dashboard/traeDevengSSI", "UEP", "PIA, PIM, certificado, comprometido, devengado and girado by executing unit"},
	"anual":      {"/Dashboard/traeDevengSSI", "ANIO", "accrued spending per year"},
	"mensual":    {"/Dashboard/traeDevengSSI", "MES", "accrued spending per month of the current year"},
	"fuente":     {"/Dashboard/traeDevengSSI", "FTE", "spending by funding source"},
	"especifica": {"/Dashboard/traeDevengSSI", "ESPECIF", "spending by expenditure classifier (específica)"},
	"contratos":  {"/Ssi/traeContratoSeaceDWH", "", "SEACE procurement contracts"},
	"obras":      {"/Ssi/traeDatosObraCGR", "", "InfoObras (Contraloría) works records and physical progress"},
}

func viewNames() []string {
	return []string{"ejecutora", "anual", "mensual", "fuente", "especifica", "contratos", "obras"}
}

func (m *Module) detalle() *core.Command {
	return &core.Command{
		Path:    []string{"invierte", "detalle"},
		Tool:    "mef_inversion_detalle",
		Title:   "Detalle de ejecución, contratos y obras de una inversión",
		Summary: "An investment's spending by executor/year/month/source/classifier, SEACE contracts or InfoObras works",
		Description: `Returns one breakdown of a public investment identified by CUI, chosen with "view":
- ejecutora: PIA, PIM, certificado, comprometido, devengado, girado per executing unit;
- anual: accrued spending per year (the project's execution history);
- mensual: accrued spending per month;
- fuente: spending by funding source (recursos ordinarios, canon, endeudamiento…);
- especifica: spending by expenditure classifier;
- contratos: procurement contracts registered in SEACE;
- obras: works registered in the Contraloría's InfoObras, with physical progress.

Use it after mef_inversion_ficha when the question needs history, money flow, contractors or works progress. Behaviour: one request (two for contratos, which first reads the project's SNIP code), ~0.3 s, cached for hours; columns are SSI's own field names with empty placeholders removed. An empty result means SSI has no records of that kind for the project (common for contratos/obras on older or non-works investments) — it is not an error.`,
		Params: []core.Param{
			{Name: "cui", Type: core.TypeString, Positional: true, Required: true, Help: `Código único de inversiones, e.g. "2194487".`},
			{Name: "view", Type: core.TypeString, Default: "ejecutora", Enum: viewNames(), Help: `Breakdown to return: ejecutora, anual, mensual, fuente, especifica, contratos or obras.`},
		},
		Network:  true,
		ReadOnly: true,
		Examples: []string{"mef invierte detalle 2194487", "mef invierte detalle 2194487 --view anual -f csv", "mef invierte detalle 2194487 --view contratos -f json"},
		Run: func(ctx context.Context, env *core.Env, in core.Input, sink core.Sink) (*core.Result, error) {
			cui, err := parseCUI(in.Str("cui"))
			if err != nil {
				return nil, err
			}
			view := in.Str("view")
			spec := views[view]
			form := url.Values{"id": {cui}}
			switch view {
			case "contratos":
				ficha, _, err := m.post(ctx, env, "/Ssi/traeDetInvSSI", url.Values{"id": {cui}, "tipo": {"SIAF"}}, "invierte/ficha")
				if err != nil {
					return nil, err
				}
				snip := ""
				if len(ficha) > 0 {
					if s, ok := ficha[0]["COD_SNIP"].(string); ok {
						snip = s
					}
				}
				form.Set("codsnip", snip)
				form.Set("vers", "v2")
			case "obras":
			default:
				form.Set("tipo", spec.Tipo)
			}
			recs, cached, err := m.post(ctx, env, spec.Path, form, "invierte/detalle")
			if err != nil {
				return nil, err
			}
			var cols []string
			seen := map[string]bool{}
			rows := make([]core.Row, 0, len(recs))
			for _, rec := range recs {
				row := compact(rec)
				for k := range row {
					if !seen[k] {
						seen[k] = true
						cols = append(cols, k)
					}
				}
				rows = append(rows, row)
			}
			cols = preferred(cols)
			// SSI returns history rows in no particular order; chronological
			// order is what every reader of a spending history expects.
			sort.SliceStable(rows, func(i, j int) bool {
				for _, k := range []string{"NUM_ANIO", "COD_MES"} {
					a, b := fmt.Sprint(rows[i][k]), fmt.Sprint(rows[j][k])
					if a != b {
						if len(a) != len(b) {
							return len(a) < len(b)
						}
						return a < b
					}
				}
				return false
			})
			sink.SetColumns(cols)
			if err := sink.Emit(rows...); err != nil {
				return nil, err
			}
			return &core.Result{Columns: cols, Meta: map[string]any{"cui": cui, "view": view, "description": spec.Help, "upstream_cached": cached}}, nil
		},
	}
}

// preferred puts identifying columns first, the rest alphabetically.
func preferred(cols []string) []string {
	first := []string{"CODIGO_UNICO", "NUM_ANIO", "DES_LARGA_MES", "PERIODO", "SEC_EJEC", "DES_UEP", "DES_FUENTE_FINANC", "DES_TIPO", "MTO_PIA", "MTO_PIM", "MTO_CERT", "MTO_COMPROM", "MTO_DEVEN", "MTO_GIRADO"}
	have := map[string]bool{}
	for _, c := range cols {
		have[c] = true
	}
	var out []string
	used := map[string]bool{}
	for _, c := range first {
		if have[c] {
			out = append(out, c)
			used[c] = true
		}
	}
	var rest []string
	for _, c := range cols {
		if !used[c] {
			rest = append(rest, c)
		}
	}
	sortStrings(rest)
	return append(out, rest...)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
