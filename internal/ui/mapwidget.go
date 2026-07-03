package ui

import (
	"fmt"
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"defeatmap/internal/geo"
	"defeatmap/internal/maptiles"
	"defeatmap/internal/store"
)

const tilePx float32 = 256

type tileKey struct{ z, x, y int }

// MapWidget is a custom interactive slippy map: drag to pan, scroll to zoom
// (anchored under the cursor), tap to drop a pin, colored markers for
// logged incidents, and an OSM attribution badge as required by ODbL.
//
// The renderer keeps the number of canvas objects it emits as small as
// possible (tight tile buffer, cached/reused tile images) — Fyne's canvas
// silently stops drawing objects past a certain count in one widget's
// object list, and this map can accumulate many incident markers over the
// lifetime of the app, so object count needs to stay well under that.
type MapWidget struct {
	widget.BaseWidget

	mu        sync.Mutex
	provider  *maptiles.Provider
	zoom      int
	minZoom   int
	maxZoom   int
	centerLat float64
	centerLon float64
	size      fyne.Size

	onContextMenu        func(absPos fyne.Position, lat, lon float64)
	onMarkerTap          func(id string)
	onZoomChanged        func(zoom int)
	onHover              func(lat, lon float64)
	onHoverEnd           func()
	onCalloutContextMenu func(absPos fyne.Position, incidentID string)
	onCalloutChanged     func(incidentID string, hasCallout bool, dx, dy float32)
	onCalloutTap         func(incidentID string)
	onLayerPointTap      func(name, description string)

	incidents []store.Incident
	// layerPoints are read-only reference points imported from KML/KMZ
	// files (see SetLayerPoints) — rendered distinctly from incidents and
	// not editable/draggable, just tappable for a quick look at their
	// name/description.
	layerPoints []store.LayerPoint

	hasPending             bool
	pendingLat, pendingLon float64

	// callouts holds, per incident ID, a pinned info card connected to its
	// marker by a leader line — created by press-and-drag on the marker (see
	// MouseDown/Dragged) and removed via RemoveCallout (right-click on the
	// card). The offset is a fixed screen-space vector from the marker, so
	// the card moves together with its marker when the map pans.
	callouts map[string]calloutOffset
	// calloutRects is each currently-shown callout card's last-rendered
	// on-screen bounds, written by mapRenderer.rebuild() (the card's size is
	// content-dependent, computed there) and read by hitTestCallout — this
	// keeps hit-testing exact even though cards aren't a fixed size.
	calloutRects map[string]calloutRect
	// dragCandidateID is the marker or callout card under the cursor at
	// MouseDown; it is promoted to draggingID only once the pointer
	// actually moves, so a plain click still just opens the incident (via
	// Tapped/onMarkerTap) instead of starting a drag.
	dragCandidateID string
	draggingID      string

	imgCache map[tileKey]*canvas.Image
	loading  map[tileKey]bool
}

// calloutOffset is a pinned callout card's position, as a fixed pixel
// offset from its incident marker's current on-screen position.
type calloutOffset struct {
	dx, dy float32
}

// calloutRect is a callout card's last-rendered on-screen bounds, used for
// hit-testing (see MapWidget.calloutRects).
type calloutRect struct {
	pos  fyne.Position
	size fyne.Size
}

func NewMapWidget(provider *maptiles.Provider) *MapWidget {
	m := &MapWidget{
		provider:     provider,
		zoom:         11,
		minZoom:      5,
		maxZoom:      19,
		centerLat:    45.05,
		centerLon:    34.2,
		imgCache:     map[tileKey]*canvas.Image{},
		loading:      map[tileKey]bool{},
		callouts:     map[string]calloutOffset{},
		calloutRects: map[string]calloutRect{},
	}
	m.ExtendBaseWidget(m)
	return m
}

func (m *MapWidget) CreateRenderer() fyne.WidgetRenderer {
	r := &mapRenderer{widget: m}
	r.rebuild()
	return r
}

// ---- public API ----

func (m *MapWidget) SetView(lat, lon float64, zoom int) {
	m.mu.Lock()
	m.centerLat, m.centerLon, m.zoom = lat, lon, clampInt(zoom, m.minZoom, m.maxZoom)
	z := m.zoom
	m.mu.Unlock()
	m.Refresh()
	if m.onZoomChanged != nil {
		m.onZoomChanged(z)
	}
}

func (m *MapWidget) SetProvider(p *maptiles.Provider) {
	m.mu.Lock()
	m.provider = p
	m.imgCache = map[tileKey]*canvas.Image{}
	m.loading = map[tileKey]bool{}
	m.mu.Unlock()
	m.Refresh()
}

// SetOnContextMenu is called on right-click with the click's absolute
// (window-relative) position — for showing a context menu there — plus the
// geographic coordinate under the cursor.
func (m *MapWidget) SetOnContextMenu(cb func(absPos fyne.Position, lat, lon float64)) {
	m.onContextMenu = cb
}
func (m *MapWidget) SetOnMarkerTap(cb func(id string))  { m.onMarkerTap = cb }
func (m *MapWidget) SetOnZoomChanged(cb func(zoom int)) { m.onZoomChanged = cb }

// SetOnCalloutContextMenu is called on right-click over a pinned callout
// card, with the click's absolute position (for showing a menu there) and
// the ID of the incident the card belongs to.
func (m *MapWidget) SetOnCalloutContextMenu(cb func(absPos fyne.Position, incidentID string)) {
	m.onCalloutContextMenu = cb
}

// SetOnCalloutChanged is called whenever a callout is placed/moved (with
// its final position, once the drag ends) or removed, so the caller can
// persist it — placed callouts otherwise only live in memory and would be
// lost the next time the app starts.
func (m *MapWidget) SetOnCalloutChanged(cb func(incidentID string, hasCallout bool, dx, dy float32)) {
	m.onCalloutChanged = cb
}

// SetOnCalloutTap is called on double-click over a pinned callout card,
// with the ID of the incident it belongs to (see DoubleTapped).
func (m *MapWidget) SetOnCalloutTap(cb func(incidentID string)) {
	m.onCalloutTap = cb
}

// RemoveCallout un-pins the given incident's callout card and leader line,
// if one is currently shown.
func (m *MapWidget) RemoveCallout(incidentID string) {
	m.mu.Lock()
	delete(m.callouts, incidentID)
	m.mu.Unlock()
	m.Refresh()
	if m.onCalloutChanged != nil {
		m.onCalloutChanged(incidentID, false, 0, 0)
	}
}

// SetOnHover is called continuously while the mouse moves over the map with
// the coordinate under the cursor, and SetOnHoverEnd once when it leaves.
func (m *MapWidget) SetOnHover(cb func(lat, lon float64)) { m.onHover = cb }
func (m *MapWidget) SetOnHoverEnd(cb func())              { m.onHoverEnd = cb }

// SetIncidents also restores any previously-placed callouts (in.HasCallout)
// that this widget doesn't already have in memory, so cards placed before
// an app restart come back where they were left.
func (m *MapWidget) SetIncidents(list []store.Incident) {
	m.mu.Lock()
	m.incidents = list
	for _, in := range list {
		if !in.HasCallout {
			continue
		}
		if _, exists := m.callouts[in.ID]; !exists {
			m.callouts[in.ID] = calloutOffset{dx: float32(in.CalloutDX), dy: float32(in.CalloutDY)}
		}
	}
	m.mu.Unlock()
	m.Refresh()
}

// SetLayerPoints replaces the currently-shown set of imported layer
// reference points (see MapWidget.layerPoints) — the caller is
// responsible for filtering to just the currently-visible layers.
func (m *MapWidget) SetLayerPoints(points []store.LayerPoint) {
	m.mu.Lock()
	m.layerPoints = points
	m.mu.Unlock()
	m.Refresh()
}

// SetOnLayerPointTap is called when the user taps an imported layer point,
// with its name/description.
func (m *MapWidget) SetOnLayerPointTap(cb func(name, description string)) {
	m.onLayerPointTap = cb
}

// SetPendingPoint shows a persistent marker at (lat, lon) — used while the
// user is filling in the "add incident" form, so the chosen spot stays
// visible on the map instead of disappearing the moment the context menu
// closes. ClearPendingPoint removes it (called when that dialog closes).
func (m *MapWidget) SetPendingPoint(lat, lon float64) {
	m.mu.Lock()
	m.hasPending = true
	m.pendingLat, m.pendingLon = lat, lon
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) ClearPendingPoint() {
	m.mu.Lock()
	m.hasPending = false
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) Zoom() int { return m.zoom }

func (m *MapWidget) ZoomIn()  { m.zoomAt(fyne.NewPos(m.size.Width/2, m.size.Height/2), 1) }
func (m *MapWidget) ZoomOut() { m.zoomAt(fyne.NewPos(m.size.Width/2, m.size.Height/2), -1) }

// ---- geometry helpers ----

func (m *MapWidget) pixelToLonLat(pos fyne.Position) (lon, lat float64) {
	cx, cy := geo.LonLatToTile(m.centerLon, m.centerLat, m.zoom)
	px := cx + float64(pos.X-m.size.Width/2)/float64(tilePx)
	py := cy + float64(pos.Y-m.size.Height/2)/float64(tilePx)
	return geo.TileToLonLat(px, py, m.zoom)
}

func (m *MapWidget) lonLatToPixel(lon, lat float64) fyne.Position {
	cx, cy := geo.LonLatToTile(m.centerLon, m.centerLat, m.zoom)
	tx, ty := geo.LonLatToTile(lon, lat, m.zoom)
	x := float32(tx-cx)*tilePx + m.size.Width/2
	y := float32(ty-cy)*tilePx + m.size.Height/2
	return fyne.NewPos(x, y)
}

func (m *MapWidget) zoomAt(anchor fyne.Position, delta int) {
	m.mu.Lock()
	newZoom := clampInt(m.zoom+delta, m.minZoom, m.maxZoom)
	if newZoom == m.zoom {
		m.mu.Unlock()
		return
	}
	lon, lat := m.pixelToLonLat(anchor)
	nx, ny := geo.LonLatToTile(lon, lat, newZoom)
	ctx := nx - float64(anchor.X-m.size.Width/2)/float64(tilePx)
	cty := ny - float64(anchor.Y-m.size.Height/2)/float64(tilePx)
	m.zoom = newZoom
	m.centerLon, m.centerLat = geo.TileToLonLat(ctx, cty, newZoom)
	m.mu.Unlock()
	m.Refresh()
	if m.onZoomChanged != nil {
		m.onZoomChanged(newZoom)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- input handling ----

func (m *MapWidget) Dragged(ev *fyne.DragEvent) {
	m.mu.Lock()
	if m.draggingID == "" && m.dragCandidateID != "" {
		// First movement since a MouseDown that landed on a marker: this is
		// a callout drag, not a map pan.
		m.draggingID = m.dragCandidateID
		m.dragCandidateID = ""
		if _, exists := m.callouts[m.draggingID]; !exists {
			// Seed a small, already-visible offset so the leader line snaps
			// into view immediately on the first pixel of movement, instead
			// of growing from zero length (imperceptible) up to whatever
			// the real cursor delta happens to be.
			m.callouts[m.draggingID] = calloutOffset{dx: 32, dy: -32}
		}
	}
	if m.draggingID != "" {
		off := m.callouts[m.draggingID]
		off.dx += ev.Dragged.DX
		off.dy += ev.Dragged.DY
		m.callouts[m.draggingID] = off
		m.mu.Unlock()
		m.Refresh()
		return
	}
	x0, y0 := geo.LonLatToTile(m.centerLon, m.centerLat, m.zoom)
	x0 -= float64(ev.Dragged.DX) / float64(tilePx)
	y0 -= float64(ev.Dragged.DY) / float64(tilePx)
	m.centerLon, m.centerLat = geo.TileToLonLat(x0, y0, m.zoom)
	m.mu.Unlock()
	m.Refresh()
}

// DragEnd satisfies fyne.Draggable. For a plain map pan there's no
// follow-up action — tiles for wherever the user panned to are simply
// fetched and cached on demand. For a callout drag, the offset was already
// written to m.callouts live, in Dragged, so this just persists that final
// position (once, on release, rather than on every drag frame) and clears
// the in-progress state.
func (m *MapWidget) DragEnd() {
	m.mu.Lock()
	id := m.draggingID
	off, hadCallout := m.callouts[id]
	m.draggingID = ""
	m.dragCandidateID = ""
	m.mu.Unlock()

	if id != "" && hadCallout && m.onCalloutChanged != nil {
		m.onCalloutChanged(id, true, off.dx, off.dy)
	}
}

func (m *MapWidget) Scrolled(ev *fyne.ScrollEvent) {
	if ev.Scrolled.DY > 0 {
		m.zoomAt(ev.Position, 1)
	} else if ev.Scrolled.DY < 0 {
		m.zoomAt(ev.Position, -1)
	}
}

// markerHitPx is the click/drag tolerance radius, in pixels, for hitting an
// incident marker.
const markerHitPx float32 = 10

// hitTestMarker returns the ID of the incident marker closest to pos, if
// any lies within markerHitPx of it.
func (m *MapWidget) hitTestMarker(pos fyne.Position) (string, bool) {
	var bestID string
	var bestDist float32 = markerHitPx + 1
	for _, in := range m.incidents {
		p := m.lonLatToPixel(in.Lon, in.Lat)
		dx := p.X - pos.X
		dy := p.Y - pos.Y
		d := float32(math.Hypot(float64(dx), float64(dy)))
		if d < bestDist {
			bestDist = d
			bestID = in.ID
		}
	}
	if bestDist <= markerHitPx {
		return bestID, true
	}
	return "", false
}

// hitTestCallout returns the ID of the pinned callout card under pos, if
// any, using each card's last-rendered bounds (calloutRects).
func (m *MapWidget) hitTestCallout(pos fyne.Position) (string, bool) {
	for id := range m.callouts {
		rect, ok := m.calloutRects[id]
		if !ok {
			continue
		}
		if pos.X >= rect.pos.X && pos.X <= rect.pos.X+rect.size.Width &&
			pos.Y >= rect.pos.Y && pos.Y <= rect.pos.Y+rect.size.Height {
			return id, true
		}
	}
	return "", false
}

// hitTestLayerPoint returns the imported layer point closest to pos, if
// any lies within markerHitPx of it.
func (m *MapWidget) hitTestLayerPoint(pos fyne.Position) (store.LayerPoint, bool) {
	var best store.LayerPoint
	var bestDist float32 = markerHitPx + 1
	found := false
	for _, p := range m.layerPoints {
		sp := m.lonLatToPixel(p.Lon, p.Lat)
		dx := sp.X - pos.X
		dy := sp.Y - pos.Y
		d := float32(math.Hypot(float64(dx), float64(dy)))
		if d < bestDist {
			bestDist = d
			best = p
			found = true
		}
	}
	if found && bestDist <= markerHitPx {
		return best, true
	}
	return store.LayerPoint{}, false
}

func (m *MapWidget) Tapped(ev *fyne.PointEvent) {
	if id, ok := m.hitTestMarker(ev.Position); ok && m.onMarkerTap != nil {
		m.onMarkerTap(id)
		return
	}
	if p, ok := m.hitTestLayerPoint(ev.Position); ok && m.onLayerPointTap != nil {
		m.onLayerPointTap(p.Name, p.Description)
	}
}

// DoubleTapped opens a pinned callout card's full details on double-click
// (see SetOnCalloutTap). Implementing fyne.DoubleTappable is also what
// makes Fyne wait to disambiguate single vs. double clicks anywhere on the
// map (including on markers) — a small, standard trade-off for supporting
// double-click at all on the same widget.
func (m *MapWidget) DoubleTapped(ev *fyne.PointEvent) {
	if id, ok := m.hitTestCallout(ev.Position); ok && m.onCalloutTap != nil {
		m.onCalloutTap(id)
	}
}

// TappedSecondary (right-click) shows the "delete" menu when a callout card
// is under the cursor, or otherwise is how the user adds a new incident: it
// hands the click's absolute position and the geographic coordinate under
// it to the app, which shows a small context menu there.
func (m *MapWidget) TappedSecondary(ev *fyne.PointEvent) {
	if id, ok := m.hitTestCallout(ev.Position); ok {
		if m.onCalloutContextMenu != nil {
			m.onCalloutContextMenu(ev.AbsolutePosition, id)
		}
		return
	}
	if m.onContextMenu == nil {
		return
	}
	lon, lat := m.pixelToLonLat(ev.Position)
	m.onContextMenu(ev.AbsolutePosition, lat, lon)
}

// ---- desktop.Mouseable: press-and-drag on a marker starts a pinned
// callout instead of panning the map (see Dragged) ----

func (m *MapWidget) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	m.mu.Lock()
	// Check the callout card first: once a card is pinned it's drawn on top
	// of its marker/line and is often the only thing left clickable there,
	// so grabbing the card itself must also be able to reposition it.
	if id, ok := m.hitTestCallout(ev.Position); ok {
		m.dragCandidateID = id
	} else if id, ok := m.hitTestMarker(ev.Position); ok {
		m.dragCandidateID = id
	} else {
		m.dragCandidateID = ""
	}
	m.mu.Unlock()
}

func (m *MapWidget) MouseUp(*desktop.MouseEvent) {
	m.mu.Lock()
	m.dragCandidateID = ""
	m.mu.Unlock()
}

// ---- desktop.Hoverable: live coordinate readout under the cursor ----

func (m *MapWidget) MouseIn(ev *desktop.MouseEvent)    { m.reportHover(ev.Position) }
func (m *MapWidget) MouseMoved(ev *desktop.MouseEvent) { m.reportHover(ev.Position) }
func (m *MapWidget) MouseOut() {
	if m.onHoverEnd != nil {
		m.onHoverEnd()
	}
}

func (m *MapWidget) reportHover(pos fyne.Position) {
	if m.onHover == nil {
		return
	}
	lon, lat := m.pixelToLonLat(pos)
	m.onHover(lat, lon)
}

// ---- renderer ----

type mapRenderer struct {
	widget  *MapWidget
	objects []fyne.CanvasObject

	// markerPool is reused across rebuild() calls so the object count stays
	// bounded to len(incidents) instead of growing with every redraw.
	markerPool  []*canvas.Circle
	pendingMark *canvas.Circle
	pendingHalo *canvas.Circle

	// layerPointPool mirrors markerPool but for imported layer points (see
	// MapWidget.layerPoints) — square markers so they read as visually
	// distinct from the round incident markers.
	layerPointPool []*canvas.Rectangle

	// calloutPool holds one line+card per incident that currently has a
	// pinned callout, reused across rebuild() calls and pruned once the
	// callout is removed (see RemoveCallout).
	calloutPool map[string]*calloutWidgets
}

// calloutWidgets is a pinned callout's leader line plus its info card,
// bound to the incident's live data on every rebuild.
type calloutWidgets struct {
	line *canvas.Line
	bg   *canvas.Rectangle
	card fyne.CanvasObject
	refs *calloutRowRefs
}

// calloutRowRefs holds the label refs for a callout card's content. Every
// field is capped to a single line (truncated with "…" if it doesn't fit)
// so the whole card is always exactly 3 lines — header, object, and
// description — never more, regardless of how long any of them are.
type calloutRowRefs struct {
	date *widget.Label
	city *widget.Label
	obj  *widget.Label
	desc *widget.Label
}

func newCalloutContent() (fyne.CanvasObject, *calloutRowRefs) {
	date := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	city := widget.NewLabel("")
	obj := widget.NewLabel("")
	obj.Truncation = fyne.TextTruncateEllipsis
	desc := widget.NewLabel("")
	desc.Truncation = fyne.TextTruncateEllipsis
	desc.TextStyle = fyne.TextStyle{Italic: true}
	refs := &calloutRowRefs{date: date, city: city, obj: obj, desc: desc}
	content := container.New(layout.NewCustomPaddedVBoxLayout(1),
		container.New(layout.NewCustomPaddedHBoxLayout(4), date, city),
		obj,
		desc,
	)
	return content, refs
}

func bindCalloutContent(r *calloutRowRefs, in store.Incident) {
	r.date.SetText(formatDisplayDate(in.Date))
	r.city.SetText(in.City)
	r.obj.SetText(in.ObjectName)
	r.desc.SetText(in.Description)
	if in.Description == "" {
		r.desc.Hide()
	} else {
		r.desc.Show()
	}
}

// calloutCardW is the card's fixed width; combined with every field being
// truncated to one line (see newCalloutContent), this gives the card a
// fixed, predictable size (3 lines: header, object, description) no matter
// how long any field's text is — nothing stretches the card wider or
// taller, and nothing spills into whatever is drawn behind it.
const calloutCardW float32 = 240

// calloutMinZoom is the lowest zoom level at which callout cards are still
// drawn (see mapRenderer.rebuild); below it they're hidden to avoid
// overlapping clutter, since their on-screen size stays fixed regardless
// of zoom while the real-world distance between markers shrinks.
const calloutMinZoom = 11

// sizeCalloutCard returns card's natural height at the fixed calloutCardW,
// resizing it (and, through normal container layout, its children) to
// match. Two passes because Fyne's box layouts compute each child's height
// from that child's *current* MinSize() before applying a new width, so a
// label's reported size only reflects calloutCardW after it has already
// been resized to that width once — hence resize-measure-resize rather
// than a single MinSize() call.
func sizeCalloutCard(card fyne.CanvasObject) fyne.Size {
	card.Resize(fyne.NewSize(calloutCardW, card.MinSize().Height))
	h := card.MinSize().Height
	size := fyne.NewSize(calloutCardW, h)
	card.Resize(size)
	return size
}

func newCalloutCard() *calloutWidgets {
	bg := canvas.NewRectangle(colOverlayBg)
	bg.StrokeColor = colPrimary
	bg.StrokeWidth = 1.5
	bg.CornerRadius = 8
	content, refs := newCalloutContent()
	card := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(4, 4, 6, 6), content))

	line := canvas.NewLine(colPrimary)
	line.StrokeWidth = 2
	return &calloutWidgets{line: line, bg: bg, card: card, refs: refs}
}

func (r *mapRenderer) Layout(size fyne.Size) {
	r.widget.mu.Lock()
	r.widget.size = size
	r.widget.mu.Unlock()
	r.rebuild()
}

func (r *mapRenderer) MinSize() fyne.Size           { return fyne.NewSize(320, 240) }
func (r *mapRenderer) Refresh()                     { r.rebuild() }
func (r *mapRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *mapRenderer) Destroy()                     {}

func (r *mapRenderer) rebuild() {
	w := r.widget
	w.mu.Lock()
	size := w.size
	zoom := w.zoom
	centerLon, centerLat := w.centerLon, w.centerLat
	provider := w.provider
	incidents := w.incidents
	layerPoints := w.layerPoints
	hasPending := w.hasPending
	pendingLat, pendingLon := w.pendingLat, w.pendingLon
	callouts := make(map[string]calloutOffset, len(w.callouts))
	for id, off := range w.callouts {
		callouts[id] = off
	}
	w.mu.Unlock()

	if size.Width <= 0 || size.Height <= 0 || provider == nil {
		r.objects = nil
		return
	}

	bg := canvas.NewRectangle(colBackground)
	bg.Resize(size)
	objects := []fyne.CanvasObject{bg}

	cx, cy := geo.LonLatToTile(centerLon, centerLat, zoom)
	n := int(math.Exp2(float64(zoom)))

	// Only cover the visible area plus a single tile of buffer on each
	// side — over-fetching tiles was blowing past the object budget below.
	tilesAcross := int(math.Ceil(float64(size.Width)/float64(tilePx))) + 1
	tilesDown := int(math.Ceil(float64(size.Height)/float64(tilePx))) + 1
	startTX := int(math.Floor(cx)) - tilesAcross/2
	startTY := int(math.Floor(cy)) - tilesDown/2

	for dx := 0; dx <= tilesAcross; dx++ {
		for dy := 0; dy <= tilesDown; dy++ {
			tx := startTX + dx
			ty := startTY + dy
			if ty < 0 || ty >= n {
				continue
			}
			wrappedX := ((tx % n) + n) % n

			screenX := (float32(tx)-float32(cx))*tilePx + size.Width/2
			screenY := (float32(ty)-float32(cy))*tilePx + size.Height/2
			if screenX < -tilePx || screenX > size.Width || screenY < -tilePx || screenY > size.Height {
				continue
			}

			obj := r.tileObject(zoom, wrappedX, ty)
			obj.Resize(fyne.NewSize(tilePx+1, tilePx+1))
			obj.Move(fyne.NewPos(screenX, screenY))
			objects = append(objects, obj)
		}
	}

	// markers — reuse a pool of *canvas.Circle across redraws so the
	// object count only grows with the number of incidents, never with
	// how many times the map has been panned/zoomed/refreshed.
	for len(r.markerPool) < len(incidents) {
		c := canvas.NewCircle(colPrimary)
		c.StrokeColor = colForeground
		c.StrokeWidth = 1.5
		r.markerPool = append(r.markerPool, c)
	}
	const markerR = 7
	for i, in := range incidents {
		pin := r.markerPool[i]
		pin.FillColor = parseHexColor(in.Color)
		tx, ty := geo.LonLatToTile(in.Lon, in.Lat, zoom)
		screenX := (float32(tx)-float32(cx))*tilePx + size.Width/2
		screenY := (float32(ty)-float32(cy))*tilePx + size.Height/2
		pin.Resize(fyne.NewSize(markerR*2, markerR*2))
		pin.Move(fyne.NewPos(screenX-markerR, screenY-markerR))
		pin.Hidden = screenX < -20 || screenX > size.Width+20 || screenY < -20 || screenY > size.Height+20
		objects = append(objects, pin)
	}

	// imported layer points — square markers, visually distinct from the
	// round incident markers, read-only reference data from KML/KMZ files.
	for len(r.layerPointPool) < len(layerPoints) {
		sq := canvas.NewRectangle(colLayerPoint)
		sq.StrokeColor = colForeground
		sq.StrokeWidth = 1
		sq.CornerRadius = 2
		r.layerPointPool = append(r.layerPointPool, sq)
	}
	const layerPointR = 6
	for i, p := range layerPoints {
		sq := r.layerPointPool[i]
		tx, ty := geo.LonLatToTile(p.Lon, p.Lat, zoom)
		screenX := (float32(tx)-float32(cx))*tilePx + size.Width/2
		screenY := (float32(ty)-float32(cy))*tilePx + size.Height/2
		sq.Resize(fyne.NewSize(layerPointR*2, layerPointR*2))
		sq.Move(fyne.NewPos(screenX-layerPointR, screenY-layerPointR))
		sq.Hidden = screenX < -20 || screenX > size.Width+20 || screenY < -20 || screenY > size.Height+20
		objects = append(objects, sq)
	}

	// pending point — where the user right-clicked to add a new incident,
	// kept visible (a distinct color + halo ring) until that dialog closes.
	if hasPending {
		if r.pendingMark == nil {
			r.pendingMark = canvas.NewCircle(colSuccess)
			r.pendingMark.StrokeColor = colForeground
			r.pendingMark.StrokeWidth = 1.5
		}
		if r.pendingHalo == nil {
			r.pendingHalo = canvas.NewCircle(color.Transparent)
			r.pendingHalo.StrokeColor = colSuccess
			r.pendingHalo.StrokeWidth = 2
		}
		tx, ty := geo.LonLatToTile(pendingLon, pendingLat, zoom)
		screenX := (float32(tx)-float32(cx))*tilePx + size.Width/2
		screenY := (float32(ty)-float32(cy))*tilePx + size.Height/2

		const haloR = 13
		r.pendingHalo.Resize(fyne.NewSize(haloR*2, haloR*2))
		r.pendingHalo.Move(fyne.NewPos(screenX-haloR, screenY-haloR))
		objects = append(objects, r.pendingHalo)

		const pendR = 8
		r.pendingMark.Resize(fyne.NewSize(pendR*2, pendR*2))
		r.pendingMark.Move(fyne.NewPos(screenX-pendR, screenY-pendR))
		objects = append(objects, r.pendingMark)
	}

	// callout cards — a pinned info card + leader line per incident that
	// currently has one (see MapWidget.callouts). Pool entries are created
	// lazily and pruned once their callout is removed or the incident is
	// gone. Each card is sized to its own content's natural MinSize (no
	// wrapping/truncation in the callout labels), so it's exactly as small
	// as it can be while still showing every field in full.
	for id := range r.calloutPool {
		if _, ok := callouts[id]; !ok {
			delete(r.calloutPool, id)
		}
	}
	newRects := make(map[string]calloutRect, len(callouts))
	// Below calloutMinZoom, a fixed on-screen card size covers a much
	// bigger geographic area, so cards for nearby incidents start
	// overlapping into an unreadable mess. Rather than shrinking them
	// (illegible at any real size) or leaving them piled on top of each
	// other, just hide cards+lines until the user zooms back in — the
	// underlying callouts map (and each card's position within it) is
	// untouched, so they reappear exactly where they were left.
	if zoom >= calloutMinZoom {
		for _, in := range incidents {
			off, ok := callouts[in.ID]
			if !ok {
				continue
			}
			if r.calloutPool == nil {
				r.calloutPool = map[string]*calloutWidgets{}
			}
			cw, ok := r.calloutPool[in.ID]
			if !ok {
				cw = newCalloutCard()
				r.calloutPool[in.ID] = cw
			}
			bindCalloutContent(cw.refs, in)
			col := parseHexColor(in.Color)
			cw.bg.StrokeColor = col
			cw.bg.FillColor = calloutFillColor(col)
			cw.line.StrokeColor = col

			tx, ty := geo.LonLatToTile(in.Lon, in.Lat, zoom)
			markerX := (float32(tx)-float32(cx))*tilePx + size.Width/2
			markerY := (float32(ty)-float32(cy))*tilePx + size.Height/2
			centerX, centerY := markerX+off.dx, markerY+off.dy

			cardSize := sizeCalloutCard(cw.card)
			cardPos := fyne.NewPos(centerX-cardSize.Width/2, centerY-cardSize.Height/2)
			cw.card.Move(cardPos)
			newRects[in.ID] = calloutRect{pos: cardPos, size: cardSize}

			cw.line.Position1 = fyne.NewPos(markerX, markerY)
			cw.line.Position2 = fyne.NewPos(centerX, centerY)
			objects = append(objects, cw.line)
			objects = append(objects, cw.card)
		}
	}
	w.mu.Lock()
	w.calloutRects = newRects
	w.mu.Unlock()

	// attribution (required by OpenStreetMap / ODbL)
	attrBg := canvas.NewRectangle(colButton)
	attrBg.Resize(fyne.NewSize(190, 18))
	attrBg.Move(fyne.NewPos(4, size.Height-22))
	attrTxt := canvas.NewText("© OpenStreetMap contributors", colForeground)
	attrTxt.TextSize = 10
	attrTxt.Move(fyne.NewPos(8, size.Height-21))
	objects = append(objects, attrBg, attrTxt)

	r.objects = objects
}

// tileObject returns the cached tile image if already decoded, otherwise a
// placeholder rectangle while a background fetch is kicked off (at most
// once per tile key).
func (r *mapRenderer) tileObject(z, x, y int) fyne.CanvasObject {
	w := r.widget
	key := tileKey{z, x, y}

	w.mu.Lock()
	if img, ok := w.imgCache[key]; ok {
		w.mu.Unlock()
		return img
	}
	w.mu.Unlock()

	if data, ok := w.provider.GetCached(z, x, y); ok {
		img := canvas.NewImageFromResource(fyne.NewStaticResource(fmt.Sprintf("tile-%d-%d-%d.png", z, x, y), data))
		img.FillMode = canvas.ImageFillStretch
		img.ScaleMode = canvas.ImageScaleFastest
		w.mu.Lock()
		w.imgCache[key] = img
		w.mu.Unlock()
		return img
	}

	w.mu.Lock()
	alreadyLoading := w.loading[key]
	w.loading[key] = true
	w.mu.Unlock()

	if !alreadyLoading {
		provider := w.provider
		provider.RequestAsync(z, x, y, func(data []byte, err error) {
			fyne.Do(func() {
				w.mu.Lock()
				delete(w.loading, key)
				if err == nil {
					img := canvas.NewImageFromResource(fyne.NewStaticResource(fmt.Sprintf("tile-%d-%d-%d.png", z, x, y), data))
					img.FillMode = canvas.ImageFillStretch
					img.ScaleMode = canvas.ImageScaleFastest
					w.imgCache[key] = img
				}
				w.mu.Unlock()
				if err == nil {
					w.Refresh()
				}
			})
		})
	}

	placeholder := canvas.NewRectangle(colButton)
	placeholder.StrokeColor = colSeparator
	placeholder.StrokeWidth = 0.5
	return placeholder
}
