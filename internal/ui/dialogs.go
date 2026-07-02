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
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"defeatmap/internal/geo"
	"defeatmap/internal/store"
)

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

// ---- incident list row ----

type incidentRowRefs struct {
	date *widget.Label
	city *widget.Label
	obj  *widget.Label
	desc *widget.Label
}

func newIncidentRow() fyne.CanvasObject {
	date := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	city := widget.NewLabel("")
	obj := widget.NewLabel("")
	obj.Wrapping = fyne.TextTruncate
	desc := widget.NewLabel("")
	desc.Wrapping = fyne.TextWrapWord
	desc.TextStyle = fyne.TextStyle{Italic: true}
	refs := &incidentRowRefs{date: date, city: city, obj: obj, desc: desc}
	row := container.NewVBox(
		container.NewHBox(date, city),
		obj,
		desc,
		widget.NewSeparator(),
	)
	setRowRefs(row, refs)
	return row
}

// a tiny side-table to attach struct refs to a container without abusing Fyne's object model
var rowRefTable = map[fyne.CanvasObject]*incidentRowRefs{}

func setRowRefs(o fyne.CanvasObject, r *incidentRowRefs) { rowRefTable[o] = r }

func bindIncidentRow(o fyne.CanvasObject, in store.Incident) {
	r, ok := rowRefTable[o]
	if !ok {
		return
	}
	r.date.SetText(formatDisplayDate(in.Date))
	r.city.SetText(in.City)
	r.obj.SetText(in.ObjectName)
	r.desc.SetText(in.Description)
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

	form := widget.NewForm(
		widget.NewFormItem("Город", cityEntry),
		widget.NewFormItem("Объект", objEntry),
		widget.NewFormItem("Дата", dateRow),
		widget.NewFormItem("Доп. информация", descEntry),
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

	content := container.NewVBox(sizer, form, errLabel, buttons)
	d = dialog.NewCustomWithoutButtons(title, content, a.win)
	if isNew {
		// Keep the clicked point visibly marked on the map while this
		// dialog is open, whether it ends up saved or cancelled.
		a.mapWidget.SetPendingPoint(in.Lat, in.Lon)
		d.SetOnClosed(a.mapWidget.ClearPendingPoint)
	}
	d.Show()
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
	d.Show()
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

	reportList := widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject { return newIncidentRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) { bindIncidentRow(o, items[i]) },
	)
	reportList.OnSelected = func(i widget.ListItemID) {
		d.Hide()
		a.jumpToIncident(items[i])
	}

	listSizer := canvas.NewRectangle(color.Transparent)
	listSizer.SetMinSize(fyne.NewSize(0, 340))
	listArea := container.NewStack(listSizer, reportList)

	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(480, 0))

	closeBtn := widget.NewButton("Закрыть", func() { d.Hide() })

	content := container.NewVBox(sizer, header, breakdownLabel, widget.NewSeparator(), listArea, closeBtn)
	d = dialog.NewCustomWithoutButtons("Отчёт", content, a.win)
	d.Show()
}
