package ui

import (
	"fmt"
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
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

	onContextMenu func(absPos fyne.Position, lat, lon float64)
	onMarkerTap   func(id string)
	onZoomChanged func(zoom int)
	onHover       func(lat, lon float64)
	onHoverEnd    func()

	incidents []store.Incident

	hasPending             bool
	pendingLat, pendingLon float64

	imgCache map[tileKey]*canvas.Image
	loading  map[tileKey]bool
}

func NewMapWidget(provider *maptiles.Provider) *MapWidget {
	m := &MapWidget{
		provider:  provider,
		zoom:      11,
		minZoom:   5,
		maxZoom:   19,
		centerLat: 45.05,
		centerLon: 34.2,
		imgCache:  map[tileKey]*canvas.Image{},
		loading:   map[tileKey]bool{},
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

// SetOnHover is called continuously while the mouse moves over the map with
// the coordinate under the cursor, and SetOnHoverEnd once when it leaves.
func (m *MapWidget) SetOnHover(cb func(lat, lon float64)) { m.onHover = cb }
func (m *MapWidget) SetOnHoverEnd(cb func())              { m.onHoverEnd = cb }

func (m *MapWidget) SetIncidents(list []store.Incident) {
	m.mu.Lock()
	m.incidents = list
	m.mu.Unlock()
	m.Refresh()
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
	x0, y0 := geo.LonLatToTile(m.centerLon, m.centerLat, m.zoom)
	x0 -= float64(ev.Dragged.DX) / float64(tilePx)
	y0 -= float64(ev.Dragged.DY) / float64(tilePx)
	m.centerLon, m.centerLat = geo.TileToLonLat(x0, y0, m.zoom)
	m.mu.Unlock()
	m.Refresh()
}

// DragEnd satisfies fyne.Draggable; panning freely never needs a follow-up
// action here — tiles for wherever the user pans are simply fetched and
// cached on demand by the map renderer.
func (m *MapWidget) DragEnd() {}

func (m *MapWidget) Scrolled(ev *fyne.ScrollEvent) {
	if ev.Scrolled.DY > 0 {
		m.zoomAt(ev.Position, 1)
	} else if ev.Scrolled.DY < 0 {
		m.zoomAt(ev.Position, -1)
	}
}

func (m *MapWidget) Tapped(ev *fyne.PointEvent) {
	// hit-test markers (closest within 10px)
	const hit = 10
	var bestID string
	var bestDist float32 = hit + 1
	for _, in := range m.incidents {
		p := m.lonLatToPixel(in.Lon, in.Lat)
		dx := p.X - ev.Position.X
		dy := p.Y - ev.Position.Y
		d := float32(math.Hypot(float64(dx), float64(dy)))
		if d < bestDist {
			bestDist = d
			bestID = in.ID
		}
	}
	if bestID != "" && bestDist <= hit && m.onMarkerTap != nil {
		m.onMarkerTap(bestID)
	}
}

// TappedSecondary (right-click) is how the user adds a new incident: it
// hands the click's absolute position and the geographic coordinate under
// it to the app, which shows a small context menu there.
func (m *MapWidget) TappedSecondary(ev *fyne.PointEvent) {
	if m.onContextMenu == nil {
		return
	}
	lon, lat := m.pixelToLonLat(ev.Position)
	m.onContextMenu(ev.AbsolutePosition, lat, lon)
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
	hasPending := w.hasPending
	pendingLat, pendingLon := w.pendingLat, w.pendingLon
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
		tx, ty := geo.LonLatToTile(in.Lon, in.Lat, zoom)
		screenX := (float32(tx)-float32(cx))*tilePx + size.Width/2
		screenY := (float32(ty)-float32(cy))*tilePx + size.Height/2
		pin.Resize(fyne.NewSize(markerR*2, markerR*2))
		pin.Move(fyne.NewPos(screenX-markerR, screenY-markerR))
		pin.Hidden = screenX < -20 || screenX > size.Width+20 || screenY < -20 || screenY > size.Height+20
		objects = append(objects, pin)
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
