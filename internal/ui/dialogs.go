package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"defeatmap/internal/store"
)

// ---- incident list row ----

type incidentRowRefs struct {
	date *widget.Label
	city *widget.Label
	obj  *widget.Label
}

func newIncidentRow() fyne.CanvasObject {
	date := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	city := widget.NewLabel("")
	obj := widget.NewLabel("")
	obj.Wrapping = fyne.TextTruncate
	refs := &incidentRowRefs{date: date, city: city, obj: obj}
	row := container.NewVBox(
		container.NewHBox(date, city),
		obj,
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
	r.date.SetText(in.Date)
	r.city.SetText(in.City)
	r.obj.SetText(in.ObjectName)
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
	dateEntry.SetText(in.Date)
	dateEntry.PlaceHolder = "ГГГГ-ММ-ДД"

	descEntry := widget.NewMultiLineEntry()
	descEntry.SetText(in.Description)
	descEntry.PlaceHolder = "Дополнительная информация о происшествии..."
	descEntry.Wrapping = fyne.TextWrapWord
	descEntry.SetMinRowsVisible(3)

	form := widget.NewForm(
		widget.NewFormItem("Город", cityEntry),
		widget.NewFormItem("Объект", objEntry),
		widget.NewFormItem("Дата", dateEntry),
		widget.NewFormItem("Доп. информация", descEntry),
	)

	title := "Новое происшествие"
	if !isNew {
		title = "Происшествие"
	}

	var d dialog.Dialog

	errLabel := canvas.NewText("", colPrimary)
	errLabel.TextSize = 11

	save := widget.NewButtonWithIcon("Сохранить", nil, func() {
		if _, err := time.Parse("2006-01-02", dateEntry.Text); err != nil {
			errLabel.Text = "Дата должна быть в формате ГГГГ-ММ-ДД"
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
		in.Date = dateEntry.Text
		in.Description = descEntry.Text
		in.RegionID = a.currentRegion.ID

		if _, err := a.st.EnsureCity(a.currentRegion.ID, in.City, in.Lat, in.Lon); err != nil {
			errLabel.Text = err.Error()
			errLabel.Refresh()
			return
		}

		var err error
		if isNew {
			in.CreatedAt = time.Now().UTC()
			err = a.st.AddIncident(*in)
		} else {
			err = a.st.UpdateIncident(*in)
		}
		if err != nil {
			errLabel.Text = err.Error()
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
	d.Show()
}

// ---- add region dialog ----

func (a *App) showAddRegionDialog(lat, lon float64) {
	nameEntry := widget.NewEntry()
	nameEntry.PlaceHolder = "Например: Одесская область"

	zoomEntry := widget.NewEntry()
	zoomEntry.SetText("12")

	latEntry := widget.NewEntry()
	latEntry.SetText(fmt.Sprintf("%.5f", lat))
	lonEntry := widget.NewEntry()
	lonEntry.SetText(fmt.Sprintf("%.5f", lon))

	radiusEntry := widget.NewEntry()
	radiusEntry.SetText("0.6")
	radiusEntry.PlaceHolder = "Радиус области в градусах (~60 км)"

	form := widget.NewForm(
		widget.NewFormItem("Название", nameEntry),
		widget.NewFormItem("Широта центра", latEntry),
		widget.NewFormItem("Долгота центра", lonEntry),
		widget.NewFormItem("Радиус (°)", radiusEntry),
		widget.NewFormItem("Зум по умолчанию", zoomEntry),
	)

	var d dialog.Dialog
	errLabel := canvas.NewText("", colPrimary)
	errLabel.TextSize = 11

	create := widget.NewButtonWithIcon("Создать", nil, func() {
		var clat, clon, radius float64
		var zoom int
		if _, err := fmt.Sscanf(latEntry.Text, "%f", &clat); err != nil {
			errLabel.Text = "Некорректная широта"
			errLabel.Refresh()
			return
		}
		if _, err := fmt.Sscanf(lonEntry.Text, "%f", &clon); err != nil {
			errLabel.Text = "Некорректная долгота"
			errLabel.Refresh()
			return
		}
		if _, err := fmt.Sscanf(radiusEntry.Text, "%f", &radius); err != nil || radius <= 0 {
			radius = 0.6
		}
		if _, err := fmt.Sscanf(zoomEntry.Text, "%d", &zoom); err != nil || zoom < 3 || zoom > 19 {
			zoom = 12
		}
		if nameEntry.Text == "" {
			errLabel.Text = "Укажите название"
			errLabel.Refresh()
			return
		}
		r := store.Region{
			ID:          "region-" + fmt.Sprint(time.Now().UnixNano()),
			Name:        nameEntry.Text,
			CenterLat:   clat,
			CenterLon:   clon,
			DefaultZoom: zoom,
			MinLat:      clat - radius,
			MaxLat:      clat + radius,
			MinLon:      clon - radius*1.4,
			MaxLon:      clon + radius*1.4,
		}
		if err := a.st.AddRegion(r); err != nil {
			errLabel.Text = err.Error()
			errLabel.Refresh()
			return
		}
		a.switchToRegion(r.ID)
		d.Hide()
	})
	create.Importance = widget.HighImportance
	cancel := widget.NewButton("Отмена", func() { d.Hide() })

	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(440, 0))

	content := container.NewVBox(sizer, form, errLabel, container.NewHBox(cancel, create))
	d = dialog.NewCustomWithoutButtons("Новая карта", content, a.win)
	d.Show()
}
