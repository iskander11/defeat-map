package ui

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"defeatmap/internal/kml"
	"defeatmap/internal/store"
)

// ---- layers tab: imported KML/KMZ reference points, shown as their own
// map overlay separate from the user's own incidents (see MapWidget's
// layerPoints) ----

// buildLayersTab is the left panel's third tab: the list of imported
// layers (each independently shown/hidden or removed) plus buttons to
// import a new one and export the current incidents.
func (a *App) buildLayersTab() fyne.CanvasObject {
	a.layersListBox = container.NewVBox()

	importBtn := widget.NewButtonWithIcon("Импортировать KML/KMZ", theme.FolderOpenIcon(), a.onImportLayer)
	exportBtn := widget.NewButtonWithIcon("Экспортировать происшествия", theme.DownloadIcon(), a.onExportIncidents)

	a.refreshLayersList()

	return container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), importBtn, exportBtn),
		nil, nil,
		container.NewVScroll(a.layersListBox),
	)
}

// refreshLayersList rebuilds the layer list UI and pushes the flattened
// set of currently-visible layers' points to the map.
func (a *App) refreshLayersList() {
	layers := a.st.LayersByRegion(a.currentRegion.ID)
	if a.layersListBox != nil {
		a.layersListBox.RemoveAll()
		if len(layers) == 0 {
			empty := widget.NewLabel("Нет импортированных слоёв")
			empty.Importance = widget.LowImportance
			a.layersListBox.Add(container.NewPadded(empty))
		}
		for _, l := range layers {
			l := l
			nameLabel := widget.NewLabel(fmt.Sprintf("%s (%d)", l.Name, len(l.Points)))
			nameLabel.Truncation = fyne.TextTruncateEllipsis

			visCheck := widget.NewCheck("", func(checked bool) {
				_ = a.st.SetLayerVisible(l.ID, checked)
				a.refreshLayersList()
			})
			visCheck.SetChecked(l.Visible)

			delBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				dialog.ShowConfirm("Удалить слой?",
					fmt.Sprintf("Слой «%s» (%d точек) будет удалён целиком.", l.Name, len(l.Points)),
					func(ok bool) {
						if !ok {
							return
						}
						_ = a.st.DeleteLayer(l.ID)
						a.refreshLayersList()
					}, a.win)
			})

			a.layersListBox.Add(container.NewBorder(nil, nil, visCheck, delBtn, nameLabel))
			a.layersListBox.Add(widget.NewSeparator())
		}
	}

	var points []store.LayerPoint
	for _, l := range layers {
		if !l.Visible {
			continue
		}
		points = append(points, l.Points...)
	}
	a.mapWidget.SetLayerPoints(points)
}

// newProgressDialog shows an indeterminate progress bar under a message,
// for background work the UI needs to stay responsive during (file I/O,
// parsing) — without this, doing that work directly in a file dialog's
// callback blocks Fyne's event loop and the whole window shows as "Не
// отвечает" until it finishes, which for a large KMZ can take a while.
func (a *App) newProgressDialog(title, message string) dialog.Dialog {
	label := widget.NewLabel(message)
	bar := widget.NewProgressBarInfinite()
	content := container.NewVBox(label, bar)
	d := dialog.NewCustomWithoutButtons(title, content, a.win)
	d.Show()
	return d
}

// onImportLayer lets the user pick a .kml/.kmz file and adds its
// Point-geometry Placemarks as a new, independently toggleable layer.
// Reading and parsing the file happens on a background goroutine (see
// newProgressDialog) so a large file doesn't freeze the window.
func (a *App) onImportLayer() {
	fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		if r == nil {
			return // user cancelled
		}

		name := r.URI().Name()
		progress := a.newProgressDialog("Импорт KML/KMZ", fmt.Sprintf("Импортируется «%s»…", name))

		go func() {
			defer r.Close()
			data, err := io.ReadAll(r)
			if err != nil {
				fyne.Do(func() { progress.Hide(); dialog.ShowError(err, a.win) })
				return
			}

			var points []kml.Point
			if strings.EqualFold(filepath.Ext(name), ".kmz") {
				points, err = kml.ParseKMZ(data)
			} else {
				points, err = kml.ParseKML(data)
			}
			if err != nil {
				fyne.Do(func() { progress.Hide(); dialog.ShowError(err, a.win) })
				return
			}
			if len(points) == 0 {
				fyne.Do(func() {
					progress.Hide()
					dialog.ShowInformation("Импорт KML/KMZ", "В файле не найдено ни одной точки (Placemark с координатами).", a.win)
				})
				return
			}

			layerPoints := make([]store.LayerPoint, len(points))
			for i, p := range points {
				layerPoints[i] = store.LayerPoint{Name: p.Name, Description: p.Description, Lat: p.Lat, Lon: p.Lon}
			}
			_, saveErr := a.st.AddLayer(store.Layer{
				RegionID: a.currentRegion.ID,
				Name:     name,
				Visible:  true,
				Points:   layerPoints,
			})

			fyne.Do(func() {
				progress.Hide()
				if saveErr != nil {
					dialog.ShowError(saveErr, a.win)
					return
				}
				a.refreshLayersList()
			})
		}()
	}, a.win)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".kml", ".kmz"}))
	fd.Show()
}

// onExportIncidents lets the user save the current region's incidents as a
// KML or KMZ file — the format is picked from whichever extension they
// type or select in the save dialog. Building/writing happens on a
// background goroutine for the same reason as onImportLayer.
func (a *App) onExportIncidents() {
	fd := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		if w == nil {
			return // user cancelled
		}

		progress := a.newProgressDialog("Экспорт KML/KMZ", "Формируется файл…")
		name := w.URI().Name()

		go func() {
			defer w.Close()
			incidents := a.st.IncidentsByRegion(a.currentRegion.ID)
			points := make([]kml.Point, len(incidents))
			for i, in := range incidents {
				points[i] = kml.Point{Name: in.ObjectName, Description: in.Description, Lat: in.Lat, Lon: in.Lon}
			}

			var data []byte
			var buildErr error
			if strings.EqualFold(filepath.Ext(name), ".kmz") {
				data, buildErr = kml.BuildKMZ(points)
			} else {
				data = kml.BuildKML(points)
			}
			if buildErr == nil {
				_, buildErr = w.Write(data)
			}

			fyne.Do(func() {
				progress.Hide()
				if buildErr != nil {
					dialog.ShowError(buildErr, a.win)
				}
			})
		}()
	}, a.win)
	fd.SetFileName("происшествия.kml")
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".kml", ".kmz"}))
	fd.Show()
}

// onLayerPointTap shows a quick, read-only look at an imported layer
// point's name/description — these aren't editable, only the whole layer
// can be removed (see refreshLayersList).
func (a *App) onLayerPointTap(name, description string) {
	title := name
	if title == "" {
		title = "Точка слоя"
	}
	msg := description
	if msg == "" {
		msg = "Без описания"
	}
	dialog.ShowInformation(title, msg, a.win)
}
