package report

import (
	"fmt"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"github.com/JOSETRA44/mef-mcp/internal/analysis"
	"github.com/JOSETRA44/mef-mcp/internal/render"
)

// Sheet names. "Resumen" first because that is what a reader opens on.
const (
	sheetSummary = "Resumen"
	sheetData    = "Datos"
	sheetSource  = "Fuente"
)

// writeXLSX builds a workbook a public-finance analyst can work in directly:
// conclusions and a chart on the first sheet, the full table with autofilter
// and frozen header on the second, provenance on the third.
func writeXLSX(path string, d *Doc) error {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", sheetSummary); err != nil {
		return err
	}
	for _, s := range []string{sheetData, sheetSource} {
		if _, err := f.NewSheet(s); err != nil {
			return err
		}
	}

	titleStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 15, Color: "1F4E79"}})
	if err != nil {
		return err
	}
	mutedStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "5B6B7C", Size: 10}})
	wrapStyle, _ := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	headStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F4E79"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	labelStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	amountFmt := "#,##0.00"
	amountStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &amountFmt})
	pctFmt := "0.0"
	pctStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &pctFmt})
	totalFmt := "#,##0.00"
	totalStyle, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true},
		CustomNumFmt: &totalFmt,
		Border:       []excelize.Border{{Type: "top", Color: "1F4E79", Style: 2}},
	})
	totalTextStyle, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Bold: true},
		Border: []excelize.Border{{Type: "top", Color: "1F4E79", Style: 2}},
	})

	if err := dataSheet(f, d, headStyle, amountStyle, pctStyle, totalStyle, totalTextStyle); err != nil {
		return err
	}
	if err := summarySheet(f, d, titleStyle, mutedStyle, wrapStyle, labelStyle, headStyle, amountStyle, pctStyle); err != nil {
		return err
	}
	if err := sourceSheet(f, d, labelStyle, mutedStyle); err != nil {
		return err
	}
	f.SetActiveSheet(0)
	return f.SaveAs(path)
}

func colName(i int) string {
	n, _ := excelize.ColumnNumberToName(i + 1)
	return n
}

func dataSheet(f *excelize.File, d *Doc, head, amount, pct, total, totalText int) error {
	for i, c := range d.Columns {
		cell := colName(i) + "1"
		if err := f.SetCellValue(sheetData, cell, d.Header(c)); err != nil {
			return err
		}
		width := utf8.RuneCountInString(d.Header(c)) + 2
		for _, row := range d.Rows {
			if n := utf8.RuneCountInString(render.Cell(row[c], false)); n+2 > width {
				width = n + 2
			}
		}
		if width > 46 {
			width = 46
		}
		if width < 10 {
			width = 10
		}
		if err := f.SetColWidth(sheetData, colName(i), colName(i), float64(width)); err != nil {
			return err
		}
	}
	last := colName(len(d.Columns) - 1)
	if err := f.SetCellStyle(sheetData, "A1", last+"1", head); err != nil {
		return err
	}
	f.SetRowHeight(sheetData, 1, 28)

	rowAt := func(n int, row map[string]any, textStyle, numStyle int) error {
		for i, c := range d.Columns {
			cell := colName(i) + fmt.Sprint(n)
			v := row[c]
			num, isNum := analysis.Num(v)
			_, isStr := v.(string)
			if isNum && !isStr {
				if err := f.SetCellFloat(sheetData, cell, num, -1, 64); err != nil {
					return err
				}
				st := numStyle
				if IsPercent(c) {
					st = pct
				}
				if err := f.SetCellStyle(sheetData, cell, cell, st); err != nil {
					return err
				}
				continue
			}
			if v == nil {
				continue
			}
			if err := f.SetCellValue(sheetData, cell, render.Cell(v, false)); err != nil {
				return err
			}
			if textStyle != 0 {
				if err := f.SetCellStyle(sheetData, cell, cell, textStyle); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for i, row := range d.Rows {
		if err := rowAt(i+2, row, 0, amount); err != nil {
			return err
		}
	}
	n := len(d.Rows) + 2
	if d.Totals != nil {
		if err := rowAt(n, d.Totals, totalText, total); err != nil {
			return err
		}
	}

	// Freeze the header and offer filters: the first two things anyone does
	// to a 400-row budget table by hand.
	if err := f.SetPanes(sheetData, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
		Selection: []excelize.Selection{{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"}},
	}); err != nil {
		return err
	}
	if len(d.Rows) > 0 {
		if err := f.AutoFilter(sheetData, "A1:"+last+fmt.Sprint(len(d.Rows)+1), []excelize.AutoFilterOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func summarySheet(f *excelize.File, d *Doc, title, muted, wrap, label, head, amount, pct int) error {
	set := func(cell string, v any, style int) error {
		if err := f.SetCellValue(sheetSummary, cell, v); err != nil {
			return err
		}
		if style != 0 {
			return f.SetCellStyle(sheetSummary, cell, cell, style)
		}
		return nil
	}
	f.SetColWidth(sheetSummary, "A", "A", 30)
	f.SetColWidth(sheetSummary, "B", "B", 24)
	f.SetColWidth(sheetSummary, "C", "F", 22)

	if err := set("A1", d.Title, title); err != nil {
		return err
	}
	if d.Subtitle != "" {
		if err := set("A2", d.Subtitle, muted); err != nil {
			return err
		}
	}
	row := 4
	if a := d.Analysis; a != nil {
		if a.Summary != "" {
			f.MergeCell(sheetSummary, "A4", "F6")
			if err := set("A4", a.Summary, wrap); err != nil {
				return err
			}
			f.SetRowHeight(sheetSummary, 4, 30)
			row = 8
		}
		kpis := [][2]any{
			{"PIM", a.TotalBudget}, {"Devengado", a.Total}, {"Avance %", a.AvancePct},
			{"Filas", a.Rows}, {"Concentración top 5 %", a.Top5Share}, {"Presupuesto sin ejecutar", a.IdleBudget},
		}
		if err := set("A"+fmt.Sprint(row), "Indicador", head); err != nil {
			return err
		}
		if err := set("B"+fmt.Sprint(row), "Valor", head); err != nil {
			return err
		}
		row++
		for _, k := range kpis {
			if err := set("A"+fmt.Sprint(row), k[0], label); err != nil {
				return err
			}
			st := amount
			if name, _ := k[0].(string); name == "Avance %" || name == "Concentración top 5 %" {
				st = pct
			}
			if n, ok := analysis.Num(k[1]); ok {
				if err := f.SetCellFloat(sheetSummary, "B"+fmt.Sprint(row), n, -1, 64); err != nil {
					return err
				}
				f.SetCellStyle(sheetSummary, "B"+fmt.Sprint(row), "B"+fmt.Sprint(row), st)
			}
			row++
		}
		if len(a.Findings) > 0 {
			row++
			if err := set("A"+fmt.Sprint(row), "Hallazgo", head); err != nil {
				return err
			}
			if err := set("B"+fmt.Sprint(row), "Detalle", head); err != nil {
				return err
			}
			f.SetColWidth(sheetSummary, "B", "B", 90)
			row++
			for _, fi := range a.Findings {
				if err := set("A"+fmt.Sprint(row), fi.Title, label); err != nil {
					return err
				}
				if err := set("B"+fmt.Sprint(row), fi.Detail, wrap); err != nil {
					return err
				}
				row++
			}
		}
	}

	// A native Excel chart, not a picture: the reader can retarget it.
	if chart := nativeChart(d); chart != nil {
		anchor := "H4"
		if err := f.AddChart(sheetSummary, anchor, chart); err != nil {
			return err
		}
	}
	return nil
}

// nativeChart points an Excel bar chart at ranges of the Datos sheet, so the
// chart stays live if the reader filters or edits the table.
func nativeChart(d *Doc) *excelize.Chart {
	if len(d.Rows) == 0 {
		return nil
	}
	nameCol, metricCol := -1, -1
	metric := "devengado"
	if d.Analysis != nil && d.Analysis.Metric != "" {
		metric = d.Analysis.Metric
	}
	for i, c := range d.Columns {
		switch c {
		case "nombre", "concepto", "entidad":
			if nameCol < 0 {
				nameCol = i
			}
		case metric:
			metricCol = i
		}
	}
	if nameCol < 0 || metricCol < 0 {
		return nil
	}
	n := min(10, len(d.Rows))
	cats := fmt.Sprintf("%s!$%s$2:$%s$%d", sheetData, colName(nameCol), colName(nameCol), n+1)
	vals := fmt.Sprintf("%s!$%s$2:$%s$%d", sheetData, colName(metricCol), colName(metricCol), n+1)
	title := fmt.Sprintf("%s — primeras %d filas", defaultHeader(metric), n)
	show := true
	return &excelize.Chart{
		Type: excelize.Bar,
		Series: []excelize.ChartSeries{{
			Name:       fmt.Sprintf("%s!$%s$1", sheetData, colName(metricCol)),
			Categories: cats,
			Values:     vals,
			Fill:       excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"2E74B5"}},
		}},
		Title:      excelize.ChartTitle{Paragraph: []excelize.RichTextRun{{Text: title}}},
		Legend:     excelize.ChartLegend{Position: "none"},
		Dimension:  excelize.ChartDimension{Width: 640, Height: 360},
		PlotArea:   excelize.ChartPlotArea{ShowVal: false, ShowCatName: false},
		VaryColors: &[]bool{false}[0],
		XAxis:      excelize.ChartAxis{ReverseOrder: true},
		YAxis:      excelize.ChartAxis{MajorGridLines: show},
	}
}

func sourceSheet(f *excelize.File, d *Doc, label, muted int) error {
	f.SetColWidth(sheetSource, "A", "A", 28)
	f.SetColWidth(sheetSource, "B", "B", 110)
	row := 1
	put := func(k, v string) error {
		if err := f.SetCellValue(sheetSource, "A"+fmt.Sprint(row), k); err != nil {
			return err
		}
		f.SetCellStyle(sheetSource, "A"+fmt.Sprint(row), "A"+fmt.Sprint(row), label)
		if err := f.SetCellValue(sheetSource, "B"+fmt.Sprint(row), v); err != nil {
			return err
		}
		row++
		return nil
	}
	for _, line := range d.provenance() {
		if err := f.SetCellValue(sheetSource, "A"+fmt.Sprint(row), line); err != nil {
			return err
		}
		f.SetCellStyle(sheetSource, "A"+fmt.Sprint(row), "A"+fmt.Sprint(row), muted)
		row++
	}
	row++
	if d.SourceURL != "" {
		if err := put("URL de la consulta", d.SourceURL); err != nil {
			return err
		}
	}
	for _, p := range d.metaPairs() {
		if err := put(p[0], p[1]); err != nil {
			return err
		}
	}
	for _, n := range d.Notes {
		if err := put("nota", n); err != nil {
			return err
		}
	}
	return nil
}
