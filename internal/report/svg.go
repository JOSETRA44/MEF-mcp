package report

import (
	"fmt"
	"html"
	"strings"

	"github.com/JOSETRA44/mef-mcp/internal/analysis"
)

// barsSVG draws a horizontal bar chart as inline SVG.
//
// Inline SVG rather than a charting library: the HTML report must open from a
// file with no network, and MEF data is often read on machines behind
// restrictive proxies. It also keeps the page a single file to email.
func barsSVG(c Chart) string {
	if len(c.Values) == 0 {
		return ""
	}
	const (
		width    = 760
		rowH     = 26
		labelW   = 230
		valueW   = 130
		padTop   = 8
		barInset = 6
	)
	maxV := 0.0
	for _, v := range c.Values {
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= 0 {
		maxV = 1
	}
	plotW := width - labelW - valueW
	height := padTop*2 + rowH*len(c.Values)

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %d %d" width="100%%" height="%d" role="img" aria-label="%s">`,
		width, height, height, html.EscapeString(c.Title))
	for i, v := range c.Values {
		y := padTop + i*rowH
		barW := v / maxV * float64(plotW)
		if barW < 1 {
			barW = 1
		}
		name := c.Names[i]
		if len(name) > 34 {
			name = name[:33] + "…"
		}
		fmt.Fprintf(&sb,
			`<text x="%d" y="%d" class="cat">%s</text>`+
				`<rect x="%d" y="%d" width="%.1f" height="%d" rx="3" class="bar"/>`+
				`<text x="%.1f" y="%d" class="val">%s</text>`,
			labelW-8, y+rowH-barInset-2, html.EscapeString(name),
			labelW, y+2, barW, rowH-barInset*2+4,
			float64(labelW)+barW+6, y+rowH-barInset-2, html.EscapeString(analysis.Soles(v)))
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}
