package amigable

import (
	"strconv"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/core"
)

// Dimension is one axis of Consulta Amigable. Its numeric Code is literally
// the URL parameter name the navigator uses; the codes were mapped by probing
// every value 0–45 against the live service (see docs/SOURCES.md).
type Dimension struct {
	Code    int      `json:"codigo"`
	Alias   string   `json:"alias"`
	Label   string   `json:"nombre"`
	Group   string   `json:"pregunta"`
	Aliases []string `json:"-"`
	Slow    bool     `json:"lento"`
	Note    string   `json:"nota,omitempty"`
}

const (
	qQuien  = "¿Quién gasta?"
	qQue    = "¿En qué se gasta?"
	qFinan  = "¿Con qué se financian los gastos?"
	qEstruc = "¿Cómo se estructura el gasto?"
	qDonde  = "¿Dónde se gasta?"
	qCuando = "¿Cuándo se hizo el gasto?"
)

// Dimensions is ordered as the navigator presents them.
var Dimensions = []Dimension{
	{Code: 1, Alias: "nivel", Label: "Nivel de Gobierno", Group: qQuien, Aliases: []string{"nivel-gobierno", "nivel_gobierno"}, Note: "E = Gobierno Nacional, R = Gobiernos Regionales, M = Gobiernos Locales (acepta nacional/regional/local)"},
	{Code: 2, Alias: "sector", Label: "Sector", Group: qQuien},
	{Code: 3, Alias: "pliego", Label: "Pliego", Group: qQuien},
	{Code: 4, Alias: "ejecutora", Label: "Unidad Ejecutora", Group: qQuien, Aliases: []string{"unidad-ejecutora", "ue"}},
	{Code: 37, Alias: "gobierno-local", Label: "Gobiernos Locales / Mancomunidades", Group: qQuien, Aliases: []string{"gl"}, Note: "Útil tras nivel=M: separa municipalidades de mancomunidades"},
	{Code: 38, Alias: "gobierno-regional", Label: "Gobiernos Regionales / Mancomunidades", Group: qQuien, Aliases: []string{"gr"}},
	{Code: 5, Alias: "departamento", Label: "Departamento (de la ejecutora)", Group: qQuien, Aliases: []string{"dpto"}, Note: "Ubicación de la entidad que gasta; para dónde se ejecuta el gasto usa departamento-meta"},
	{Code: 6, Alias: "provincia", Label: "Provincia (de la ejecutora)", Group: qQuien},
	{Code: 7, Alias: "municipalidad", Label: "Municipalidad / Ejecutora por ubicación", Group: qQuien, Aliases: []string{"distrito"}},
	{Code: 36, Alias: "mancomunidad", Label: "Mancomunidad", Group: qQuien},
	{Code: 30, Alias: "categoria", Label: "Categoría Presupuestal", Group: qQue, Aliases: []string{"programa", "programa-presupuestal"}},
	{Code: 31, Alias: "proyecto", Label: "Producto / Proyecto", Group: qQue, Aliases: []string{"producto"}},
	{Code: 32, Alias: "actividad", Label: "Actividad / Acción de Inversión / Obra", Group: qQue, Aliases: []string{"obra"}, Slow: true},
	{Code: 8, Alias: "funcion", Label: "Función", Group: qQue},
	{Code: 33, Alias: "division", Label: "División Funcional", Group: qQue, Aliases: []string{"division-funcional"}},
	{Code: 34, Alias: "grupo", Label: "Grupo Funcional", Group: qQue, Aliases: []string{"grupo-funcional"}, Slow: true},
	{Code: 35, Alias: "finalidad", Label: "Finalidad", Group: qQue, Slow: true, Note: "Sin filtros tarda ~15 s en el MEF"},
	{Code: 13, Alias: "meta", Label: "Meta", Group: qQue, Slow: true, Note: "Sin filtros tarda ~30 s en el MEF; filtra antes por pliego o ejecutora"},
	{Code: 14, Alias: "fuente", Label: "Fuente de Financiamiento", Group: qFinan, Aliases: []string{"fuente-financiamiento"}},
	{Code: 15, Alias: "rubro", Label: "Rubro", Group: qFinan},
	{Code: 17, Alias: "tipo-recurso", Label: "Tipo de Recurso", Group: qFinan, Aliases: []string{"recurso"}},
	{Code: 24, Alias: "generica", Label: "Genérica de Gasto", Group: qEstruc},
	{Code: 25, Alias: "subgenerica", Label: "Sub-Genérica", Group: qEstruc},
	{Code: 26, Alias: "subgenerica-detalle", Label: "Detalle Sub-Genérica", Group: qEstruc},
	{Code: 27, Alias: "especifica", Label: "Específica", Group: qEstruc},
	{Code: 28, Alias: "especifica-detalle", Label: "Detalle Específica", Group: qEstruc},
	{Code: 21, Alias: "departamento-meta", Label: "Departamento (de la meta)", Group: qDonde, Aliases: []string{"donde", "region"}},
	{Code: 22, Alias: "trimestre", Label: "Trimestre", Group: qCuando},
	{Code: 23, Alias: "mes", Label: "Mes", Group: qCuando},
}

var byName = func() map[string]Dimension {
	m := map[string]Dimension{}
	for _, d := range Dimensions {
		m[strconv.Itoa(d.Code)] = d
		m[d.Alias] = d
		for _, a := range d.Aliases {
			m[a] = d
		}
	}
	return m
}()

// normalize folds case, accents and separators so "Función", "funcion" and
// "FUNCION" resolve to the same dimension.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n", "_", "-", " ", "-")
	return r.Replace(s)
}

// Lookup resolves an alias or numeric code.
func Lookup(name string) (Dimension, error) {
	if d, ok := byName[normalize(name)]; ok {
		return d, nil
	}
	return Dimension{}, core.Usagef(
		"Valid dimensions: "+strings.Join(aliases(), ", ")+". Run `mef amigable dimensiones`.",
		"unknown dimension %q", name)
}

func aliases() []string {
	out := make([]string, 0, len(Dimensions))
	for _, d := range Dimensions {
		out = append(out, d.Alias)
	}
	return out
}

// normalizeValue lets callers write nivel=nacional instead of nivel=E.
func normalizeValue(d Dimension, v string) string {
	if d.Code == 1 {
		switch normalize(v) {
		case "e", "nacional", "gobierno-nacional", "gn":
			return "E"
		case "r", "regional", "regionales", "gobiernos-regionales", "gr":
			return "R"
		case "m", "local", "locales", "gobiernos-locales", "gl":
			return "M"
		}
	}
	return strings.TrimSpace(v)
}
