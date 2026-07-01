package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
	win      fyne.Window
	st       *store.Store
	provider *maptiles.Provider
	bundleRootCrimea string

	regions     []store.Region
	currentRegion store.Region

	mapWidget      *MapWidget
	calendarWidget *CalendarWidget

	regionSelect *widget.Select
	addBtn       *widget.Button
	searchEntry  *widget.Entry
	cityList     *widget.List
	incidentList *widget.List

	filteredCities    []store.City
	filteredIncidents []store.Incident
	dayFilter         string

	tabs *container.AppTabs
}

func NewApp(win fyne.Window, st *store.Store, bundleRootCrimea string) *App {
	a := &App{win: win, st: st, bundleRootCrimea: bundleRootCrimea}
	a.ensureCrimeaRegion()
	a.regions = st.Regions()
	a.currentRegion = a.regions[0]

	cache := maptiles.NewDiskCache(mustTileCacheDir())
	fetcher := maptiles.NewFetcher(maptiles.DefaultUserAgent)
	a.provider = maptiles.NewProvider(cache, fetcher, bundleRootCrimea, maptiles.DefaultSource, maptiles.DefaultTileURL)

	a.mapWidget = NewMapWidget(a.provider)
	a.mapWidget.SetOnPick(a.onMapPick)
	a.mapWidget.SetOnMarkerTap(a.onMarkerTap)
	a.mapWidget.SetOnOutOfBounds(a.onOutOfBounds)

	a.calendarWidget = NewCalendarWidget()
	a.calendarWidget.OnDaySelected = a.onDaySelected

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

	split := container.NewHSplit(sidebar, a.mapWidget)
	split.Offset = 0.26

	return container.NewBorder(top, nil, nil, nil, split)
}

func (a *App) buildTopBar() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("Карта поражений", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	a.regionSelect = widget.NewSelect(a.regionNames(), a.onRegionSelected)
	a.regionSelect.SetSelected(a.currentRegion.Name)

	newRegionBtn := widget.NewButtonWithIcon("Добавить карту", theme.ContentAddIcon(), a.onAddRegionClicked)

	a.addBtn = widget.NewButtonWithIcon("Добавить происшествие", theme.ContentAddIcon(), a.onToggleAddIncident)
	a.addBtn.Importance = widget.HighImportance

	right := container.NewHBox(a.regionSelect, newRegionBtn, a.addBtn)
	return container.NewBorder(nil, widget.NewSeparator(), nil, right, container.NewPadded(title))
}

func (a *App) regionNames() []string {
	names := make([]string, len(a.regions))
	for i, r := range a.regions {
		names[i] = r.Name
	}
	return names
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
		a.dayFilter = ""
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
		in := a.filteredIncidents[i]
		a.mapWidget.SetView(in.Lat, in.Lon, 15)
		a.openIncidentDialog(&in)
	}

	listTab := container.NewBorder(
		container.NewVBox(a.searchEntry, widget.NewSeparator()),
		nil, nil, nil,
		container.NewHSplit(a.cityList, a.incidentList),
	)

	calTab := container.NewVBox(a.calendarWidget)

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
		if a.dayFilter != "" && in.Date != a.dayFilter {
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

func (a *App) onDaySelected(date string) {
	a.dayFilter = date
	a.refreshIncidentList()
	if a.tabs != nil {
		a.tabs.SelectIndex(0)
	}
}

func (a *App) onRegionSelected(name string) {
	for _, r := range a.regions {
		if r.Name == name {
			a.currentRegion = r
			a.applyRegionToMap(r)
			a.searchEntry.SetText("")
			a.dayFilter = ""
			a.refreshCities()
			a.refreshIncidentList()
			return
		}
	}
}

func (a *App) applyRegionToMap(r store.Region) {
	bundle := ""
	if r.BundleDir == "crimea" {
		bundle = a.bundleRootCrimea
	}
	a.provider.SetBundleRoot(bundle)
	a.mapWidget.SetProvider(a.provider)
	a.mapWidget.SetView(r.CenterLat, r.CenterLon, r.DefaultZoom)
	b := geo.BBox{MinLat: r.MinLat, MaxLat: r.MaxLat, MinLon: r.MinLon, MaxLon: r.MaxLon}
	a.mapWidget.SetBounds(&b)
}

func (a *App) onToggleAddIncident() {
	newState := !a.mapWidget.AddPinMode()
	a.mapWidget.SetAddPinMode(newState)
	if newState {
		a.addBtn.SetText("Кликните на карте...")
		dialog.ShowInformation("Добавление происшествия", "Кликните по карте в нужном месте, чтобы указать точку происшествия.", a.win)
	} else {
		a.addBtn.SetText("Добавить происшествие")
	}
}

func (a *App) onMapPick(lat, lon float64) {
	a.mapWidget.SetAddPinMode(false)
	a.addBtn.SetText("Добавить происшествие")
	a.openIncidentDialog(&store.Incident{
		RegionID: a.currentRegion.ID,
		Lat:      lat,
		Lon:      lon,
		Date:     time.Now().Format("2006-01-02"),
	})
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

func (a *App) onAddRegionClicked() {
	a.showAddRegionDialog(a.mapWidget.centerLat, a.mapWidget.centerLon)
}

func (a *App) onOutOfBounds(lat, lon float64) {
	msg := fmt.Sprintf("Вы вышли за пределы карты «%s». Добавить новую карту для этой области?", a.currentRegion.Name)
	dialog.ShowConfirm("Новая область", msg, func(ok bool) {
		if ok {
			a.showAddRegionDialog(lat, lon)
		}
	}, a.win)
}

func (a *App) switchToRegion(id string) {
	a.regions = a.st.Regions()
	sort.Slice(a.regions, func(i, j int) bool { return a.regions[i].Name < a.regions[j].Name })
	a.regionSelect.SetOptions(a.regionNames())
	for _, r := range a.regions {
		if r.ID == id {
			a.currentRegion = r
			a.regionSelect.SetSelected(r.Name)
			a.applyRegionToMap(r)
			a.refreshCities()
			a.refreshIncidentList()
			return
		}
	}
}
