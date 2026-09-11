package amigable

import (
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Line is one parsed row of the navigator grid.
type Line struct {
	Code        string  `json:"codigo"`
	ShownCode   string  `json:"codigo_mostrado,omitempty"`
	Name        string  `json:"nombre"`
	PIA         float64 `json:"pia"`
	PIM         float64 `json:"pim"`
	Certificado float64 `json:"certificacion"`
	CompAnual   float64 `json:"compromiso_anual"`
	CompMensual float64 `json:"compromiso_mensual"`
	Devengado   float64 `json:"devengado"`
	Girado      float64 `json:"girado"`
	AvancePct   float64 `json:"avance_pct"`
	// NoBudget marks rows without PIA/PIM (time dimensions).
	NoBudget bool `json:"-"`
}

// Page is a parsed navigator response.
type Page struct {
	Lines       []Line
	Dimension   string // the grid's column header, e.g. "Sector"
	LastUpdated string // ISO date of "Última actualización", when present
}

var (
	// The radio value carries exact, unrounded amounts in a fixed order:
	// CODE/PIA/PIM/COMPROMISO_ANUAL/COMPROMISO_MENSUAL/DEVENGADO/GIRADO/CERTIFICACION.
	// Reading it avoids re-parsing the rounded, comma-grouped display cells.
	// Alternate rows carry class="alt" before id, so id is not anchored first.
	reRow     = regexp.MustCompile(`(?s)<tr\b[^>]*\bid="tr\d+".*?name="grp1" value="([^"]*)".*?<td align="left"[^>]*>(.*?)</td>`)
	reHeader  = regexp.MustCompile(`(?s)id="ctl00_CPH1_Mt0_td1"[^>]*>(.*?)</td>`)
	reTags    = regexp.MustCompile(`(?s)<[^>]+>`)
	reUpdated = regexp.MustCompile(`(?i)id="ctl00_CPH1_LblLastUpdate"[^>]*>\s*(\d{1,2})\s+de\s+([a-zá-ú]+)\s+(?:de|del)\s+(\d{4})`)
	reAppErr  = regexp.MustCompile(`(?i)(Server Error in|Runtime Error|Error de servidor)`)
)

var months = map[string]time.Month{
	"enero": 1, "febrero": 2, "marzo": 3, "abril": 4, "mayo": 5, "junio": 6, "julio": 7,
	"agosto": 8, "septiembre": 9, "setiembre": 9, "octubre": 10, "noviembre": 11, "diciembre": 12,
}

func text(fragment string) string {
	s := reTags.ReplaceAllString(fragment, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\u00a0", " ")), " ")
}

// ParsePage extracts the grid. It never guesses: rows whose radio value does
// not have the expected eight fields are skipped rather than misaligned.
func ParsePage(body string) (Page, bool) {
	var p Page
	if m := reHeader.FindStringSubmatch(body); m != nil {
		p.Dimension = text(m[1])
	}
	if m := reUpdated.FindStringSubmatch(body); m != nil {
		day, _ := strconv.Atoi(m[1])
		year, _ := strconv.Atoi(m[3])
		if mon, ok := months[strings.ToLower(m[2])]; ok {
			p.LastUpdated = time.Date(year, mon, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		}
	}
	for _, m := range reRow.FindAllStringSubmatch(body, -1) {
		parts := strings.Split(m[1], "/")
		if len(parts) != 8 {
			continue
		}
		nums := make([]float64, 7)
		ok := true
		budgetless := false
		for i := 0; i < 7; i++ {
			field := strings.TrimSpace(parts[i+1])
			if field == "" {
				// Time dimensions (mes, trimestre) have no PIA/PIM: the budget
				// is annual. Absent is not zero, so it is reported as null.
				if i <= 1 {
					budgetless = true
					continue
				}
				ok = false
				break
			}
			f, err := strconv.ParseFloat(field, 64)
			if err != nil {
				ok = false
				break
			}
			nums[i] = f
		}
		if !ok {
			continue
		}
		l := Line{
			Code: strings.TrimSpace(parts[0]),
			PIA:  nums[0], PIM: nums[1], CompAnual: nums[2], CompMensual: nums[3],
			Devengado: nums[4], Girado: nums[5], Certificado: nums[6],
			NoBudget: budgetless,
		}
		label := text(m[2])
		if shown, name, found := strings.Cut(label, ": "); found {
			l.Name = name
			if shown != l.Code {
				l.ShownCode = shown
			}
		} else {
			l.Name = strings.TrimSuffix(label, ":")
		}
		// Month names arrive as "'Enero" — an Excel text-cell artefact.
		l.Name = strings.TrimPrefix(strings.TrimSpace(l.Name), "'")
		if l.PIM != 0 {
			l.AvancePct = math.Round(l.Devengado/l.PIM*1000) / 10
		}
		p.Lines = append(p.Lines, l)
	}
	return p, reAppErr.MatchString(body)
}
