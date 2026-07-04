package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"defeatmap/internal/geo"
	"defeatmap/internal/store"
)

// ---- dialog stack: lets Escape (wired in NewApp) close whichever custom
// dialog is currently on top, since Fyne's dialogs have no built-in
// Escape-to-dismiss ----

// showDialog shows d and pushes it onto a.openDialogs so a single Escape
// press (see NewApp) closes it. Use this instead of calling d.Show()
// directly for every dialog.NewCustomWithoutButtons this package creates.
func (a *App) showDialog(d dialog.Dialog) {
	a.openDialogs = append(a.openDialogs, d)
	d.SetOnClosed(func() { a.dialogClosed(d) })
	d.Show()
}

// dialogClosed removes d from the open-dialog stack once it's hidden
// (whether via its own Close/Save button, or via Escape).
func (a *App) dialogClosed(d dialog.Dialog) {
	for i, x := range a.openDialogs {
		if x == d {
			a.openDialogs = append(a.openDialogs[:i], a.openDialogs[i+1:]...)
			return
		}
	}
}

// closeTopDialog hides the most-recently-opened dialog, if any — e.g. the
// incident view opened from within the report closes first, revealing the
// report again, rather than both closing at once.
func (a *App) closeTopDialog() {
	if n := len(a.openDialogs); n > 0 {
		a.openDialogs[n-1].Hide()
	}
}

// ---- date formatting: incidents are stored as "YYYY-MM-DD" but shown to
// the user as "ДД.ММ.ГГГГ" everywhere in the UI ----

var dateInputLayouts = []string{"02.01.2006", "2.1.2006"}

// formatDisplayDate converts a stored "YYYY-MM-DD" date to "ДД.ММ.ГГГГ" for
// display. Unparseable input is returned unchanged.
func formatDisplayDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("02.01.2006")
}

// parseDisplayDate converts a user-entered "ДД.ММ.ГГГГ" (or "Д.М.ГГГГ")
// date into the stored "YYYY-MM-DD" form.
func parseDisplayDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	var lastErr error
	for _, layout := range dateInputLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), nil
		} else {
			lastErr = err
		}
	}
	return "", lastErr
}

// descriptionRowWidth is the assumed available width for the incident list
// row's description line, used only to decide where to truncate it (see
// fitTextToLines). The sidebar is resizable (its default is the right-hand
// ~21% of the 1000px default window from the HSplit ratios in Content()),
// so this can't track the *actual* current width without a layout pass;
// it's deliberately picked on the narrow side so the 2-line cap holds even
// if the panel ends up narrower than the default.
const descriptionRowWidth float32 = 180

// fitTextToLines returns text unchanged if it wraps to at most maxLines
// lines at the given width/text size, otherwise the longest prefix (in
// runes) that does, with "…" appended — an exact, rendered-text-based cap
// (see mapRenderer's two-pass resize/measure for why this can't just be a
// single MinSize() call), unlike guessing a character count, which doesn't
// hold up across different column widths.
func fitTextToLines(width float32, text string, maxLines int, sizeName fyne.ThemeSizeName) string {
	probe := widget.NewLabel("")
	probe.SizeName = sizeName
	probe.Wrapping = fyne.TextWrapWord
	probe.Resize(fyne.NewSize(width, 2000))

	measure := func(s string) float32 {
		probe.SetText(s)
		return probe.MinSize().Height
	}
	// Calibrate from the *difference* between one and two lines rather
	// than just multiplying a single line's height by maxLines: a wrapped
	// text block has some fixed overhead (padding etc.) on top of its
	// per-line height, so height(1 line) * N overstates height(N lines) —
	// which in practice let one extra line sneak in under the cap.
	h1 := measure("Ag")
	h2 := measure("Ag\nAg")
	lineHeight := h2 - h1
	maxH := h1 + lineHeight*float32(maxLines-1) + lineHeight*0.05

	if measure(text) <= maxH {
		return text
	}
	runes := []rune(text)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if measure(string(runes[:mid])+"…") <= maxH {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return "…"
	}
	return string(runes[:lo]) + "…"
}

// ---- incident list row ----
//
// Rows are deliberately ultra-compact and built fresh per incident (no
// pooling/rebinding): date/city/object share a single line (with a small
// color dot for the incident's accent color), and the description line is
// only added AT ALL when there is one — so an incident with no "доп.
// информация" genuinely takes up less vertical space, not just a
// blank-but-reserved line. This only works because the list itself is a
// plain VBox+VScroll (see buildIncidentPanel/showReportDialog) rather than
// widget.List, which forces every row to the same fixed height.
func newIncidentRow(in store.Incident) fyne.CanvasObject {
	dot := canvas.NewRectangle(parseHexColor(in.Color))
	dot.CornerRadius = 3
	dot.SetMinSize(fyne.NewSize(8, 8))

	date := widget.NewLabelWithStyle(formatDisplayDate(in.Date), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	date.SizeName = theme.SizeNameCaptionText
	city := widget.NewLabel(in.City)
	city.SizeName = theme.SizeNameCaptionText
	obj := widget.NewLabel(in.ObjectName)
	obj.SizeName = theme.SizeNameCaptionText
	obj.Truncation = fyne.TextTruncateEllipsis

	line1 := container.New(layout.NewCustomPaddedHBoxLayout(4),
		container.NewCenter(dot), date, city, obj)

	items := []fyne.CanvasObject{line1}
	if in.Description != "" {
		fitted := fitTextToLines(descriptionRowWidth, in.Description, 2, theme.SizeNameCaptionText)
		desc := widget.NewLabel(fitted)
		desc.SizeName = theme.SizeNameCaptionText
		desc.Wrapping = fyne.TextWrapWord
		desc.TextStyle = fyne.TextStyle{Italic: true}
		items = append(items, desc)
	}
	return container.New(layout.NewCustomPaddedVBoxLayout(0), items...)
}

// doubleClickWindow is the max gap between two taps on the same incident
// row that counts as a double-click (see tappableRow).
// tappableRow wraps arbitrary content (an incident row) with click/
// double-click handling, since a plain *fyne.Container isn't tappable and
// the incident lists are now hand-built VBoxes rather than widget.List (see
// newIncidentRow). A single click calls onTapped (center the map on the
// incident); a double-click calls onDouble instead (open it for editing).
// Implementing fyne.DoubleTappable makes Fyne's own driver do the
// single-vs-double-click disambiguation (a fixed delay before Tapped fires,
// to see if a second click follows) — a hand-rolled timer comparing
// consecutive Tapped calls does not reliably see a real double-click,
// since some drivers deliver it as one OS-level double-click message
// rather than two separate taps.
type tappableRow struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	onTapped func()
	onDouble func()
}

func newTappableRow(content fyne.CanvasObject, onTapped, onDouble func()) *tappableRow {
	t := &tappableRow{content: content, onTapped: onTapped, onDouble: onDouble}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappableRow) Tapped(*fyne.PointEvent) {
	if t.onTapped != nil {
		t.onTapped()
	}
}

func (t *tappableRow) DoubleTapped(*fyne.PointEvent) {
	if t.onDouble != nil {
		t.onDouble()
	}
}

func (t *tappableRow) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (t *tappableRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}

// ---- color swatch picker ----

// colorSwatch is a small tappable square used to pick one of the preset
// incident colors (see incidentColors in theme.go). Selected swatches get a
// highlighted ring.
type colorSwatch struct {
	widget.BaseWidget
	col      color.Color
	selected bool
	onTap    func()
}

func newColorSwatch(col color.Color, onTap func()) *colorSwatch {
	s := &colorSwatch{col: col, onTap: onTap}
	s.ExtendBaseWidget(s)
	return s
}

func (s *colorSwatch) Tapped(*fyne.PointEvent) {
	if s.onTap != nil {
		s.onTap()
	}
}

func (s *colorSwatch) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (s *colorSwatch) CreateRenderer() fyne.WidgetRenderer {
	rect := canvas.NewRectangle(s.col)
	rect.CornerRadius = 5
	ring := canvas.NewRectangle(color.Transparent)
	ring.StrokeWidth = 2
	ring.CornerRadius = 5
	return &colorSwatchRenderer{swatch: s, rect: rect, ring: ring}
}

type colorSwatchRenderer struct {
	swatch     *colorSwatch
	rect, ring *canvas.Rectangle
}

func (r *colorSwatchRenderer) Layout(size fyne.Size) {
	r.rect.Resize(size)
	r.ring.Resize(size)
}
func (r *colorSwatchRenderer) MinSize() fyne.Size { return fyne.NewSize(24, 24) }
func (r *colorSwatchRenderer) Refresh() {
	r.rect.FillColor = r.swatch.col
	if r.swatch.selected {
		r.ring.StrokeColor = colForeground
	} else {
		r.ring.StrokeColor = color.Transparent
	}
	r.rect.Refresh()
	r.ring.Refresh()
}
func (r *colorSwatchRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.rect, r.ring}
}
func (r *colorSwatchRenderer) Destroy() {}

// newColorPicker builds a row of preset color swatches; selected starts as
// the incident's current color (or the first preset if unset), and picked
// is called with the newly chosen hex whenever the user taps a different
// swatch. Returns the row plus a getter for the current selection.
func newColorPicker(initial string) (row fyne.CanvasObject, getSelected func() string) {
	selected := initial
	if selected == "" {
		selected = incidentColors[0]
	}
	swatches := make([]*colorSwatch, len(incidentColors))
	objs := make([]fyne.CanvasObject, len(incidentColors))
	refreshAll := func() {
		for i, hex := range incidentColors {
			swatches[i].selected = hex == selected
			swatches[i].Refresh()
		}
	}
	for i, hex := range incidentColors {
		hex := hex
		sw := newColorSwatch(parseHexColor(hex), func() {
			selected = hex
			refreshAll()
		})
		swatches[i] = sw
		objs[i] = sw
	}
	refreshAll()
	return container.New(layout.NewCustomPaddedHBoxLayout(6), objs...), func() string { return selected }
}

// ---- incident view (read-only) dialog ----

// openIncidentViewDialog shows an incident's full details read-only — in
// particular the complete, untruncated description (the map card and list
// rows both cap it to a few lines for compactness) — with a
// "Редактировать" button that opens the actual edit form
// (openIncidentDialog). beforeEdit, if not nil, runs right before that
// happens — e.g. so the report dialog can close itself only once the user
// actually commits to editing, not just to look.
func (a *App) openIncidentViewDialog(in store.Incident, beforeEdit func()) {
	dot := canvas.NewRectangle(parseHexColor(in.Color))
	dot.CornerRadius = 4
	dot.SetMinSize(fyne.NewSize(14, 14))

	dateLabel := widget.NewLabelWithStyle(formatDisplayDate(in.Date), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	cityLabel := widget.NewLabelWithStyle(in.City, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	header := container.New(layout.NewCustomPaddedHBoxLayout(6), container.NewCenter(dot), dateLabel, cityLabel)

	objLabel := widget.NewLabel(in.ObjectName)
	objLabel.Wrapping = fyne.TextWrapWord

	descText := in.Description
	if descText == "" {
		descText = "—"
	}
	descLabel := widget.NewLabel(descText)
	descLabel.Wrapping = fyne.TextWrapWord
	if in.Description != "" {
		descLabel.TextStyle = fyne.TextStyle{Italic: true}
	}

	coordText := fmt.Sprintf("%s  •  %s", geo.FormatDecimal(in.Lat, in.Lon), geo.FormatUTM(in.Lat, in.Lon))
	coordLabel := widget.NewLabel(coordText)
	coordLabel.Wrapping = fyne.TextWrapOff

	form := widget.NewForm(
		widget.NewFormItem("Объект", objLabel),
		widget.NewFormItem("Доп. информация", descLabel),
		widget.NewFormItem("Координаты", coordLabel),
	)

	var d dialog.Dialog

	closeBtn := widget.NewButton("Закрыть", func() { d.Hide() })
	editBtn := widget.NewButtonWithIcon("Редактировать", theme.DocumentCreateIcon(), func() {
		d.Hide()
		if beforeEdit != nil {
			beforeEdit()
		}
		a.openIncidentDialog(&in)
	})
	editBtn.Importance = widget.HighImportance
	buttons := container.NewHBox(closeBtn, editBtn)

	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(420, 0))

	// The form (in particular the description) is wrapped in its own
	// bounded, scrollable area instead of sitting directly in the VBox —
	// otherwise a long description grows the dialog past the window's
	// edge, with the Закрыть/Редактировать buttons pushed off-screen and
	// no scrollbar to reach them (Escape, wired in NewApp, is the other
	// way out, but shouldn't be the only one).
	formScroll := container.NewVScroll(form)
	formScroll.SetMinSize(fyne.NewSize(0, 320))

	content := container.NewBorder(
		container.NewVBox(sizer, header, widget.NewSeparator()),
		buttons, nil, nil,
		formScroll,
	)
	d = dialog.NewCustomWithoutButtons("Происшествие", content, a.win)
	a.showDialog(d)
}

// ---- incident add/edit dialog ----

func (a *App) openIncidentDialog(in *store.Incident) {
	isNew := in.ID == ""

	cityNames := make([]string, 0)
	for _, c := range a.st.CitiesByRegion(a.currentRegion.ID) {
		cityNames = append(cityNames, c.Name)
	}
	cityEntry := widget.NewSelectEntry(cityNames)
	cityEntry.SetText(in.City)
	cityEntry.PlaceHolder = "Например: Симферополь"

	objEntry := widget.NewEntry()
	objEntry.SetText(in.ObjectName)
	objEntry.PlaceHolder = "Что повреждено (объект)"

	dateEntry := widget.NewEntry()
	if in.Date == "" {
		in.Date = time.Now().Format("2006-01-02")
	}
	dateEntry.SetText(formatDisplayDate(in.Date))
	dateEntry.PlaceHolder = "ДД.ММ.ГГГГ"

	dateCalBtn := widget.NewButtonWithIcon("", theme.CalendarIcon(), func() {
		a.showDatePicker(dateEntry.Text, func(iso string) {
			dateEntry.SetText(formatDisplayDate(iso))
		})
	})
	dateRow := container.NewBorder(nil, nil, nil, dateCalBtn, dateEntry)

	descEntry := widget.NewMultiLineEntry()
	descEntry.SetText(in.Description)
	descEntry.PlaceHolder = "Дополнительная информация о происшествии..."
	descEntry.Wrapping = fyne.TextWrapWord
	descEntry.SetMinRowsVisible(3)

	coordText := fmt.Sprintf("%s  •  %s", geo.FormatDecimal(in.Lat, in.Lon), geo.FormatUTM(in.Lat, in.Lon))
	coordLabel := widget.NewLabel(coordText)
	coordLabel.Wrapping = fyne.TextWrapOff

	colorRow, getSelectedColor := newColorPicker(in.Color)

	form := widget.NewForm(
		widget.NewFormItem("Город", cityEntry),
		widget.NewFormItem("Объект", objEntry),
		widget.NewFormItem("Дата", dateRow),
		widget.NewFormItem("Доп. информация", descEntry),
		widget.NewFormItem("Цвет", colorRow),
		widget.NewFormItem("Координаты", coordLabel),
	)

	title := "Новое происшествие"
	if !isNew {
		title = "Происшествие"
	}

	var d dialog.Dialog

	errLabel := canvas.NewText("", colPrimary)
	errLabel.TextSize = 11

	save := widget.NewButtonWithIcon("Сохранить", nil, func() {
		isoDate, err := parseDisplayDate(dateEntry.Text)
		if err != nil {
			errLabel.Text = "Дата должна быть в формате ДД.ММ.ГГГГ"
			errLabel.Refresh()
			return
		}
		if objEntry.Text == "" || cityEntry.Text == "" {
			errLabel.Text = "Укажите город и объект"
			errLabel.Refresh()
			return
		}
		in.City = cityEntry.Text
		in.ObjectName = objEntry.Text
		in.Date = isoDate
		in.Description = descEntry.Text
		in.Color = getSelectedColor()
		in.RegionID = a.currentRegion.ID

		if _, err := a.st.EnsureCity(a.currentRegion.ID, in.City, in.Lat, in.Lon); err != nil {
			errLabel.Text = err.Error()
			errLabel.Refresh()
			return
		}

		var saveErr error
		if isNew {
			in.CreatedAt = time.Now().UTC()
			saveErr = a.st.AddIncident(*in)
		} else {
			saveErr = a.st.UpdateIncident(*in)
		}
		if saveErr != nil {
			errLabel.Text = saveErr.Error()
			errLabel.Refresh()
			return
		}
		a.refreshCities()
		a.refreshIncidentList()
		d.Hide()
	})
	save.Importance = widget.HighImportance

	cancel := widget.NewButton("Отмена", func() { d.Hide() })

	buttons := container.NewHBox(cancel)
	if !isNew {
		del := widget.NewButtonWithIcon("Удалить", nil, func() {
			dialog.ShowConfirm("Удалить происшествие?", "Это действие нельзя отменить.", func(ok bool) {
				if !ok {
					return
				}
				_ = a.st.DeleteIncident(in.ID)
				a.refreshIncidentList()
				d.Hide()
			}, a.win)
		})
		buttons.Add(del)
	}
	buttons.Add(save)

	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(440, 0))

	// Bounded/scrollable for the same reason as openIncidentViewDialog — a
	// long "Доп. информация" entry shouldn't be able to push
	// Сохранить/Удалить/Отмена off-screen with no way to reach them.
	formScroll := container.NewVScroll(container.NewVBox(sizer, form))
	formScroll.SetMinSize(fyne.NewSize(0, 320))

	content := container.NewBorder(nil, container.NewVBox(errLabel, buttons), nil, nil, formScroll)
	d = dialog.NewCustomWithoutButtons(title, content, a.win)
	if isNew {
		// Keep the clicked point visibly marked on the map while this
		// dialog is open, whether it ends up saved or cancelled.
		a.mapWidget.SetPendingPoint(in.Lat, in.Lon)
		d.SetOnClosed(a.mapWidget.ClearPendingPoint)
	}
	a.showDialog(d)
}

// showDatePicker opens a small calendar popup for picking a single date, as
// an alternative to typing it in by hand. current is the date currently
// shown in the entry (ДД.ММ.ГГГГ, may be invalid/empty) and is used only to
// preset which month the calendar opens on. onPick receives the chosen date
// in stored "YYYY-MM-DD" form.
func (a *App) showDatePicker(current string, onPick func(iso string)) {
	cal := NewCalendarWidget()
	if iso, err := parseDisplayDate(current); err == nil {
		if t, err := time.Parse("2006-01-02", iso); err == nil {
			cal.year, cal.month = t.Year(), int(t.Month())
			cal.refreshGrid()
		}
	}

	var d dialog.Dialog
	cal.OnRangeSelected = func(start, end string) {
		onPick(start)
		d.Hide()
	}

	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(280, 0))
	content := container.NewVBox(sizer, cal)
	d = dialog.NewCustomWithoutButtons("Выбор даты", content, a.win)
	a.showDialog(d)
}

// ---- report dialog ----

// showReportDialog compiles a report of incidents within [start, end]
// (inclusive; an empty start means "all incidents") for the current region.
func (a *App) showReportDialog(start, end string) {
	all := a.st.IncidentsByRegion(a.currentRegion.ID)
	var items []store.Incident
	for _, in := range all {
		if start != "" && (in.Date < start || in.Date > end) {
			continue
		}
		items = append(items, in)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })

	period := "за всё время"
	if start != "" && start == end {
		period = "за " + formatDisplayDate(start)
	} else if start != "" {
		period = fmt.Sprintf("с %s по %s", formatDisplayDate(start), formatDisplayDate(end))
	}

	header := widget.NewLabelWithStyle(
		fmt.Sprintf("Отчёт по происшествиям %s\nВсего: %d", period, len(items)),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true},
	)

	byCity := map[string]int{}
	for _, in := range items {
		byCity[in.City]++
	}
	cityNames := make([]string, 0, len(byCity))
	for c := range byCity {
		cityNames = append(cityNames, c)
	}
	sort.Slice(cityNames, func(i, j int) bool {
		if byCity[cityNames[i]] != byCity[cityNames[j]] {
			return byCity[cityNames[i]] > byCity[cityNames[j]]
		}
		return cityNames[i] < cityNames[j]
	})
	var breakdown strings.Builder
	for i, c := range cityNames {
		if i > 0 {
			breakdown.WriteString("   ")
		}
		fmt.Fprintf(&breakdown, "%s: %d", c, byCity[c])
	}
	breakdownLabel := widget.NewLabel(breakdown.String())
	breakdownLabel.Wrapping = fyne.TextWrapWord

	var d dialog.Dialog

	rowsBox := container.NewVBox()
	for _, in := range items {
		in := in
		rowsBox.Add(newTappableRow(newIncidentRow(in),
			func() { a.mapWidget.SetView(in.Lat, in.Lon, 16) },
			func() { a.openIncidentViewDialog(in, func() { d.Hide() }) },
		))
	}
	reportList := container.NewVScroll(rowsBox)

	listSizer := canvas.NewRectangle(color.Transparent)
	listSizer.SetMinSize(fyne.NewSize(0, 340))
	listArea := container.NewStack(listSizer, reportList)

	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(480, 0))

	closeBtn := widget.NewButton("Закрыть", func() { d.Hide() })

	content := container.NewVBox(sizer, header, breakdownLabel, widget.NewSeparator(), listArea, closeBtn)
	d = dialog.NewCustomWithoutButtons("Отчёт", content, a.win)
	a.showDialog(d)
}
