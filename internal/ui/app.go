package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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

	providerSelect *widget.Select
	searchEntry    *widget.Entry
	cityList       *widget.List
	incidentList   *widget.List
	periodLabel    *widget.Label
	zoomLabel      *widget.Label
	coordLabel     *widget.Label

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
	sidebar := a.buildSidebar()
	top := a.buildTopBar()

	mapArea := container.NewStack(a.mapWidget, a.buildZoomOverlay(), a.buildCoordOverlay(), a.buildProviderOverlay())

	split := container.NewHSplit(sidebar, mapArea)
	split.Offset = 0.26

	return container.NewBorder(top, nil, nil, nil, split)
}

// buildZoomOverlay is a small floating +/- / current-zoom control pinned to
// the top-left corner of the map (just off the sidebar divider), as an
// independent widget layered on top via container.NewStack — kept separate
// from the map's own renderer so it is never affected by how many
// tiles/markers the map is drawing.
func (a *App) buildZoomOverlay() fyne.CanvasObject {
	a.zoomLabel = widget.NewLabelWithStyle(fmt.Sprint(a.mapWidget.Zoom()), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	zoomInBtn := widget.NewButton("+", a.mapWidget.ZoomIn)
	zoomOutBtn := widget.NewButton("−", a.mapWidget.ZoomOut)

	cluster := container.NewVBox(zoomInBtn, a.zoomLabel, zoomOutBtn)
	card := container.NewStack(
		canvas.NewRectangle(colOverlayBg),
		container.NewPadded(cluster),
	)
	fixedCard := container.New(layout.NewGridWrapLayout(fyne.NewSize(56, 140)), card)

	// A trailing spacer keeps the fixed-size card pinned to the top of the
	// map instead of being stretched to fill the full height by the Stack.
	return container.NewVBox(container.NewPadded(fixedCard), layout.NewSpacer())
}

// buildCoordOverlay shows the coordinates under the cursor (updated on
// hover) just below the zoom cluster, in a few common formats. It is blank
// until the mouse first moves over the map.
func (a *App) buildCoordOverlay() fyne.CanvasObject {
	a.coordLabel = widget.NewLabel("")
	a.coordLabel.Wrapping = fyne.TextWrapOff

	card := container.NewStack(
		canvas.NewRectangle(colOverlayBg),
		container.NewPadded(a.coordLabel),
	)

	topOffset := canvas.NewRectangle(color.Transparent)
	topOffset.SetMinSize(fyne.NewSize(0, 160))

	return container.NewVBox(topOffset, container.NewPadded(card), layout.NewSpacer())
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
		a.coordLabel.SetText("")
	}
}

// buildProviderOverlay is the map-source picker, stacked below the
// coordinate readout using the same top-left-pinned fixed-size card
// pattern as the zoom cluster (Border's edge slots do not reliably keep
// content on-screen once the row grows past a certain width, so this
// avoids that layout entirely).
func (a *App) buildProviderOverlay() fyne.CanvasObject {
	names := make([]string, len(maptiles.TileSources))
	for i, s := range maptiles.TileSources {
		names[i] = s.Name
	}
	a.providerSelect = widget.NewSelect(names, a.onProviderSelected)
	a.providerSelect.SetSelected(maptiles.TileSources[0].Name)

	label := widget.NewLabelWithStyle("Карта", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	cluster := container.NewVBox(label, a.providerSelect)
	card := container.NewStack(
		canvas.NewRectangle(colOverlayBg),
		container.NewPadded(cluster),
	)
	fixedCard := container.New(layout.NewGridWrapLayout(fyne.NewSize(190, 74)), card)

	topOffset := canvas.NewRectangle(color.Transparent)
	topOffset.SetMinSize(fyne.NewSize(0, 260))

	return container.NewVBox(topOffset, container.NewPadded(fixedCard), layout.NewSpacer())
}

func (a *App) buildTopBar() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("Карта поражений", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	hint := widget.NewLabelWithStyle("ПКМ по карте — добавить происшествие", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})

	titleBox := container.NewVBox(title, hint)
	return container.NewBorder(nil, widget.NewSeparator(), nil, nil, container.NewPadded(titleBox))
}

func (a *App) buildSidebar() fyne.CanvasObject {
	a.searchEntry = widget.NewEntry()
	a.searchEntry.SetPlaceHolder("Поиск по городу...")
	a.searchEntry.OnChanged = a.onSearchChanged

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
		a.searchEntry.SetText(c.Name)
		a.refreshIncidentList()
	}

	a.incidentList = widget.NewList(
		func() int { return len(a.filteredIncidents) },
		func() fyne.CanvasObject { return newIncidentRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			bindIncidentRow(o, a.filteredIncidents[i])
		},
	)
	a.incidentList.OnSelected = func(i widget.ListItemID) {
		a.jumpToIncident(a.filteredIncidents[i])
	}

	listTab := container.NewBorder(
		container.NewVBox(a.searchEntry, widget.NewSeparator()),
		nil, nil, nil,
		container.NewHSplit(a.cityList, a.incidentList),
	)

	a.periodLabel = widget.NewLabel("")
	a.updatePeriodLabel()
	reportBtn := widget.NewButtonWithIcon("Составить отчёт", theme.DocumentIcon(), a.onBuildReport)
	reportBtn.Importance = widget.HighImportance
	calTab := container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), a.periodLabel, reportBtn),
		nil, nil,
		a.calendarWidget,
	)

	a.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Список", theme.ListIcon(), listTab),
		container.NewTabItemWithIcon("Календарь", theme.HistoryIcon(), calTab),
	)

	a.refreshCities()
	a.refreshIncidentList()
	return a.tabs
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

func (a *App) refreshIncidentList() {
	q := strings.ToLower(strings.TrimSpace(a.searchEntry.Text))
	all := a.st.IncidentsByRegion(a.currentRegion.ID)
	a.filteredIncidents = a.filteredIncidents[:0]
	for _, in := range all {
		if !a.inSelectedRange(in.Date) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(in.City), q) && !strings.Contains(strings.ToLower(in.ObjectName), q) {
			continue
		}
		a.filteredIncidents = append(a.filteredIncidents, in)
	}
	if a.incidentList != nil {
		a.incidentList.Refresh()
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
	a.refreshIncidentList()
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
				Lat:      lat,
				Lon:      lon,
				Date:     time.Now().Format("2006-01-02"),
			})
		}),
	)
	widget.ShowPopUpMenuAtPosition(menu, a.win.Canvas(), absPos)
}

func (a *App) onMarkerTap(id string) {
	for _, in := range a.st.IncidentsByRegion(a.currentRegion.ID) {
		if in.ID == id {
			cp := in
			a.openIncidentDialog(&cp)
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
		if s.Note != "" {
			dialog.ShowInformation("Источник карты", s.Note, a.win)
		}
		return
	}
}

// jumpToIncident centers the map on an incident's location at a comfortable
// zoom level and opens it for viewing/editing.
func (a *App) jumpToIncident(in store.Incident) {
	a.mapWidget.SetView(in.Lat, in.Lon, 16)
	a.openIncidentDialog(&in)
}

func (a *App) onBuildReport() {
	a.showReportDialog(a.rangeStart, a.rangeEnd)
}

func (a *App) onZoomChanged(zoom int) {
	if a.zoomLabel != nil {
		a.zoomLabel.SetText(fmt.Sprint(zoom))
	}
}
