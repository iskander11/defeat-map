package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"defeatmap/internal/geo"
	"defeatmap/internal/maptiles"
	"defeatmap/internal/store"
)

const crimeaRegionID = "crimea"

// App wires together the store, map widget, calendar and sidebar into the
// full window content.
type App struct {
	win              fyne.Window
	st               *store.Store
	provider         *maptiles.Provider
	bundleRootCrimea string

	currentRegion store.Region

	mapWidget      *MapWidget
	calendarWidget *CalendarWidget

	providerSelect  *widget.Select
	searchEntry     *widget.Entry
	cityList        *widget.List
	incidentListBox *fyne.Container
	layersListBox   *fyne.Container
	periodLabel     *widget.Label
	zoomLabel       *widget.Label
	coordLabel      *widget.Label

	filteredCities       []store.City
	filteredIncidents    []store.Incident
	rangeStart, rangeEnd string

	tabs *container.AppTabs
}

func NewApp(win fyne.Window, st *store.Store, bundleRootCrimea string) *App {
	a := &App{win: win, st: st, bundleRootCrimea: bundleRootCrimea}
	a.ensureCrimeaRegion()
	a.currentRegion = st.Regions()[0]

	cache := maptiles.NewDiskCache(mustTileCacheDir())
	fetcher := maptiles.NewFetcher(maptiles.DefaultUserAgent)
	a.provider = maptiles.NewProvider(cache, fetcher, bundleRootCrimea, maptiles.DefaultSource, maptiles.DefaultTileURL)

	a.mapWidget = NewMapWidget(a.provider)
	a.mapWidget.SetOnContextMenu(a.onMapContextMenu)
	a.mapWidget.SetOnMarkerTap(a.onMarkerTap)
	a.mapWidget.SetOnZoomChanged(a.onZoomChanged)
	a.mapWidget.SetOnHover(a.onHover)
	a.mapWidget.SetOnHoverEnd(a.onHoverEnd)
	a.mapWidget.SetOnCalloutContextMenu(a.onCalloutContextMenu)
	a.mapWidget.SetOnCalloutChanged(a.onCalloutChanged)
	a.mapWidget.SetOnCalloutTap(a.onCalloutTap)
	a.mapWidget.SetOnLayerPointTap(a.onLayerPointTap)

	a.calendarWidget = NewCalendarWidget()
	a.calendarWidget.OnRangeSelected = a.onRangeSelected

	a.applyRegionToMap(a.currentRegion)
	return a
}

func mustTileCacheDir() string {
	dir, err := store.AppDataDir()
	if err != nil {
		return "tiles-cache"
	}
	return filepath.Join(dir, "tiles")
}

func (a *App) ensureCrimeaRegion() {
	for _, r := range a.st.Regions() {
		if r.ID == crimeaRegionID {
			return
		}
	}
	_ = a.st.AddRegion(store.Region{
		ID:          crimeaRegionID,
		Name:        "Крым",
		CenterLat:   45.05,
		CenterLon:   34.2,
		DefaultZoom: 9,
		MinLat:      geo.CrimeaBBox.MinLat,
		MaxLat:      geo.CrimeaBBox.MaxLat,
		MinLon:      geo.CrimeaBBox.MinLon,
		MaxLon:      geo.CrimeaBBox.MaxLon,
		BundleDir:   "crimea",
	})
	if len(a.st.CitiesByRegion(crimeaRegionID)) == 0 {
		for _, s := range geo.CrimeaSettlements {
			_, _ = a.st.EnsureCity(crimeaRegionID, s.Name, s.Lat, s.Lon)
		}
	}
}

// ---- layout ----

func (a *App) Content() fyne.CanvasObject {
	left := a.buildLeftPanel()
	right := a.buildIncidentPanel()

	mapArea := container.NewStack(a.mapWidget, a.buildMapControlsOverlay())

	centerRight := container.NewHSplit(mapArea, right)
	centerRight.Offset = 0.75
	full := container.NewHSplit(left, centerRight)
	full.Offset = 0.15

	return full
}

// buildMapControlsOverlay is a single compact card pinned to the top-left
// corner of the map — zoom, map source and the live coordinate readout —
// as an independent widget layered on top via container.NewStack, kept
// separate from the map's own renderer so it is never affected by how many
// tiles/markers the map is drawing.
func (a *App) buildMapControlsOverlay() fyne.CanvasObject {
	a.zoomLabel = widget.NewLabelWithStyle(fmt.Sprint(a.mapWidget.Zoom()), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	zoomOutBtn := widget.NewButton("−", a.mapWidget.ZoomOut)
	zoomInBtn := widget.NewButton("+", a.mapWidget.ZoomIn)
	zoomRow := container.NewGridWithColumns(3, zoomOutBtn, a.zoomLabel, zoomInBtn)

	names := make([]string, len(maptiles.TileSources))
	for i, s := range maptiles.TileSources {
		names[i] = s.Name
	}
	a.providerSelect = widget.NewSelect(names, a.onProviderSelected)
	a.providerSelect.SetSelected(maptiles.TileSources[0].Name)

	a.coordLabel = widget.NewLabel("Наведите курсор на карту")
	a.coordLabel.Wrapping = fyne.TextWrapOff

	cluster := container.NewVBox(
		zoomRow,
		widget.NewSeparator(),
		a.providerSelect,
		widget.NewSeparator(),
		a.coordLabel,
	)
	card := container.NewStack(
		canvas.NewRectangle(colMapOverlayBg),
		container.NewPadded(cluster),
	)
	fixedCard := container.New(layout.NewGridWrapLayout(fyne.NewSize(230, 190)), card)

	// A trailing spacer keeps the fixed-size card pinned to the top of the
	// map instead of being stretched to fill the full height by the Stack.
	return container.NewVBox(container.NewPadded(fixedCard), layout.NewSpacer())
}

func (a *App) onHover(lat, lon float64) {
	if a.coordLabel == nil {
		return
	}
	text := fmt.Sprintf("WGS84: %s\nDMS: %s\nUTM: %s",
		geo.FormatDecimal(lat, lon), geo.FormatDMS(lat, lon), geo.FormatUTM(lat, lon))
	a.coordLabel.SetText(text)
}

func (a *App) onHoverEnd() {
	if a.coordLabel != nil {
		a.coordLabel.SetText("Наведите курсор на карту")
	}
}

// buildLeftPanel holds city search/selection and the calendar — the two
// ways to navigate to a place or a time period.
func (a *App) buildLeftPanel() fyne.CanvasObject {
	a.searchEntry = widget.NewEntry()
	a.searchEntry.SetPlaceHolder("Поиск по городу...")
	a.searchEntry.OnChanged = a.onSearchChanged
	clearBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		a.searchEntry.SetText("")
		a.onSearchChanged("")
	})
	a.searchEntry.ActionItem = clearBtn

	a.cityList = widget.NewList(
		func() int { return len(a.filteredCities) },
		func() fyne.CanvasObject {
			return widget.NewLabel("Город")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(a.filteredCities[i].Name)
		},
	)
	a.cityList.OnSelected = func(i widget.ListItemID) {
		c := a.filteredCities[i]
		a.mapWidget.SetView(c.Lat, c.Lon, 14)
		a.clearRange()
		a.refreshIncidentList()
	}

	citiesTab := container.NewBorder(
		container.NewVBox(a.searchEntry, widget.NewSeparator()),
		nil, nil, nil,
		a.cityList,
	)

	a.periodLabel = widget.NewLabel("")
	// Unwrapped, this label's longest text ("Период не выбран — отчёт по
	// всем происшествиям") reports as one long line — since AppTabs sizes
	// itself to the WIDEST of all its tabs' content (not just the visible
	// one), that alone was enough to stop the whole left panel (including
	// the Города tab) from shrinking below it.
	a.periodLabel.Wrapping = fyne.TextWrapWord
	a.updatePeriodLabel()
	reportBtn := widget.NewButtonWithIcon("Составить отчёт", theme.DocumentIcon(), a.onBuildReport)
	reportBtn.Importance = widget.HighImportance
	calTab := container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), a.periodLabel, reportBtn),
		nil, nil,
		a.calendarWidget,
	)

	layersTab := a.buildLayersTab()

	a.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Города", theme.ListIcon(), citiesTab),
		container.NewTabItemWithIcon("Календарь", theme.HistoryIcon(), calTab),
		container.NewTabItemWithIcon("Слои", theme.GridIcon(), layersTab),
	)

	a.refreshCities()
	return a.tabs
}

// buildIncidentPanel is its own column on the right, listing incidents that
// match the current city/text search and calendar period. It's a plain
// VBox+VScroll (not widget.List) so each row can be its own natural,
// variable height — an incident with no description genuinely takes less
// space, rather than every row being padded out to a fixed size (see
// newIncidentRow). A single click on a row centers the map on it; a
// second, quick click on the same row opens it for editing (tappableRow).
func (a *App) buildIncidentPanel() fyne.CanvasObject {
	header := widget.NewLabelWithStyle("Происшествия", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	a.incidentListBox = container.NewVBox()

	content := container.NewBorder(
		container.NewVBox(container.NewPadded(header), widget.NewSeparator()),
		nil, nil, nil,
		container.NewVScroll(a.incidentListBox),
	)
	a.refreshIncidentList()
	return content
}

// ---- data refresh ----

func (a *App) refreshCities() {
	q := strings.ToLower(strings.TrimSpace(a.searchEntry.Text))
	all := a.st.CitiesByRegion(a.currentRegion.ID)
	a.filteredCities = a.filteredCities[:0]
	for _, c := range all {
		if q == "" || strings.Contains(strings.ToLower(c.Name), q) {
			a.filteredCities = append(a.filteredCities, c)
		}
	}
	if a.cityList != nil {
		a.cityList.Refresh()
	}
}

// refreshIncidentList shows incidents for the calendar's selected period
// (or all of them, if no period is selected) — independent of the city
// search box, which only searches the city list.
func (a *App) refreshIncidentList() {
	all := a.st.IncidentsByRegion(a.currentRegion.ID)
	a.filteredIncidents = a.filteredIncidents[:0]
	for _, in := range all {
		if !a.inSelectedRange(in.Date) {
			continue
		}
		a.filteredIncidents = append(a.filteredIncidents, in)
	}
	if a.incidentListBox != nil {
		a.incidentListBox.RemoveAll()
		for _, in := range a.filteredIncidents {
			in := in
			a.incidentListBox.Add(newTappableRow(newIncidentRow(in),
				func() { a.mapWidget.SetView(in.Lat, in.Lon, 16) },
				func() { a.openIncidentViewDialog(in, nil) },
			))
		}
	}
	a.mapWidget.SetIncidents(all)
	a.refreshCalendarCounts()
}

// inSelectedRange reports whether date falls within the calendar's
// currently selected range (or is always true when no range is picked).
func (a *App) inSelectedRange(date string) bool {
	if a.rangeStart == "" {
		return true
	}
	return date >= a.rangeStart && date <= a.rangeEnd
}

func (a *App) clearRange() {
	a.rangeStart, a.rangeEnd = "", ""
	a.calendarWidget.SetRange("", "")
	a.updatePeriodLabel()
}

func (a *App) updatePeriodLabel() {
	if a.periodLabel == nil {
		return
	}
	if a.rangeStart == "" {
		a.periodLabel.SetText("Период не выбран — отчёт по всем происшествиям")
		return
	}
	if a.rangeStart == a.rangeEnd {
		a.periodLabel.SetText(fmt.Sprintf("Выбран день: %s", a.rangeStart))
		return
	}
	a.periodLabel.SetText(fmt.Sprintf("Период: %s — %s", a.rangeStart, a.rangeEnd))
}

func (a *App) refreshCalendarCounts() {
	all := a.st.IncidentsByRegion(a.currentRegion.ID)
	counts := map[string]int{}
	for _, in := range all {
		counts[in.Date]++
	}
	a.calendarWidget.SetCounts(counts)
}

// ---- event handlers ----

func (a *App) onSearchChanged(string) {
	a.refreshCities()
}

// onRangeSelected is called by the calendar whenever the user picks a day
// or a day range. It live-filters the incident list; the user can then
// press "Составить отчёт" to see a summary for that period.
func (a *App) onRangeSelected(start, end string) {
	a.rangeStart, a.rangeEnd = start, end
	a.updatePeriodLabel()
	a.refreshIncidentList()
}

func (a *App) applyRegionToMap(r store.Region) {
	bundle := ""
	if r.BundleDir == "crimea" {
		bundle = a.bundleRootCrimea
	}
	a.provider.SetBundleRoot(bundle)
	a.mapWidget.SetProvider(a.provider)
	a.mapWidget.SetView(r.CenterLat, r.CenterLon, r.DefaultZoom)
}

// onMapContextMenu is called on right-click over the map: it shows a small
// context menu at the click position with an "Добавить происшествие" entry
// that opens the incident form for that exact spot.
func (a *App) onMapContextMenu(absPos fyne.Position, lat, lon float64) {
	menu := fyne.NewMenu("",
		fyne.NewMenuItem("Добавить происшествие", func() {
			a.openIncidentDialog(&store.Incident{
				RegionID: a.currentRegion.ID,
				City:     a.nearestCityName(lat, lon),
				Lat:      lat,
				Lon:      lon,
				Date:     time.Now().Format("2006-01-02"),
			})
		}),
	)
	widget.ShowPopUpMenuAtPosition(menu, a.win.Canvas(), absPos)
}

// nearestCityName returns the name of the region's known city/settlement
// closest to (lat, lon), so a fresh incident starts with a sensible
// pre-filled city instead of an empty field. Returns "" if the region has
// no cities yet.
func (a *App) nearestCityName(lat, lon float64) string {
	cities := a.st.CitiesByRegion(a.currentRegion.ID)
	best := ""
	bestDist := math.MaxFloat64
	for _, c := range cities {
		if d := geo.DistanceKm(lat, lon, c.Lat, c.Lon); d < bestDist {
			bestDist = d
			best = c.Name
		}
	}
	return best
}

// onCalloutContextMenu is called on right-click over a pinned callout card
// (created by dragging off a marker — see MapWidget.MouseDown/Dragged): it
// offers to remove that card and its leader line, leaving the underlying
// incident untouched.
func (a *App) onCalloutContextMenu(absPos fyne.Position, incidentID string) {
	menu := fyne.NewMenu("",
		fyne.NewMenuItem("Удалить", func() {
			a.mapWidget.RemoveCallout(incidentID)
		}),
	)
	widget.ShowPopUpMenuAtPosition(menu, a.win.Canvas(), absPos)
}

// onCalloutChanged persists a callout's placement (or removal) so it's
// restored the next time the app starts (see store.SetIncidentCallout).
func (a *App) onCalloutChanged(incidentID string, hasCallout bool, dx, dy float32) {
	_ = a.st.SetIncidentCallout(incidentID, hasCallout, float64(dx), float64(dy))
}

func (a *App) onMarkerTap(id string) {
	for _, in := range a.st.IncidentsByRegion(a.currentRegion.ID) {
		if in.ID == id {
			a.openIncidentViewDialog(in, nil)
			return
		}
	}
}

// onCalloutTap is called on double-click over a pinned callout card (see
// MapWidget.SetOnCalloutTap) — like tapping a marker, it opens the
// read-only view first rather than jumping straight to editing.
func (a *App) onCalloutTap(incidentID string) {
	for _, in := range a.st.IncidentsByRegion(a.currentRegion.ID) {
		if in.ID == incidentID {
			a.openIncidentViewDialog(in, nil)
			return
		}
	}
}

// onProviderSelected switches the active map tile source. Each source has
// its own on-disk cache (by ID), and the bundled offline Crimea overview
// only applies to OpenStreetMap, so it's cleared for the others.
func (a *App) onProviderSelected(name string) {
	for _, s := range maptiles.TileSources {
		if s.Name != name {
			continue
		}
		a.provider.SetSource(s)
		if s.ID == maptiles.DefaultSource {
			a.provider.SetBundleRoot(a.bundleRootCrimea)
		} else {
			a.provider.SetBundleRoot("")
		}
		a.mapWidget.SetProvider(a.provider)
		return
	}
}

func (a *App) onBuildReport() {
	a.showReportDialog(a.rangeStart, a.rangeEnd)
}

func (a *App) onZoomChanged(zoom int) {
	if a.zoomLabel != nil {
		a.zoomLabel.SetText(fmt.Sprint(zoom))
	}
}
