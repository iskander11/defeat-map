package ui

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// modernTheme is a compact, dark, high-contrast theme so the app looks
// current instead of using Fyne's default light widget skin.
type modernTheme struct{}

var _ fyne.Theme = (*modernTheme)(nil)

var (
	colBackground = color.NRGBA{R: 0x12, G: 0x16, B: 0x1c, A: 0xff}
	colForeground = color.NRGBA{R: 0xe8, G: 0xec, B: 0xf1, A: 0xff}
	colButton     = color.NRGBA{R: 0x1c, G: 0x22, B: 0x2b, A: 0xff}
	colDisabled   = color.NRGBA{R: 0x55, G: 0x5c, B: 0x66, A: 0xff}
	colPrimary    = color.NRGBA{R: 0xe4, G: 0x4a, B: 0x3a, A: 0xff} // "поражение" accent red
	colHover      = color.NRGBA{R: 0x24, G: 0x2c, B: 0x38, A: 0xff}
	colInputBg    = color.NRGBA{R: 0x1a, G: 0x1f, B: 0x27, A: 0xff}
	colSeparator  = color.NRGBA{R: 0x2a, G: 0x31, B: 0x3b, A: 0xff}
	colSuccess    = color.NRGBA{R: 0x3d, G: 0xa3, B: 0x5f, A: 0xff}
	// colOverlayBg is used for dialogs, dropdown/menu popups and list
	// headers — without an explicit dark value here Fyne falls back to its
	// built-in light-mode white, which combined with our always-light
	// foreground text makes popup text unreadable (white on white).
	colOverlayBg = color.NRGBA{R: 0x1e, G: 0x25, B: 0x2f, A: 0xff}
	colShadow    = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x99}
	// colMapOverlayBg is the translucent panel background for HUD-style
	// widgets drawn on top of the map (zoom controls, coordinate readout) —
	// unlike colOverlayBg it's intentionally see-through so the map stays
	// visible underneath.
	colMapOverlayBg = color.NRGBA{R: 0x1e, G: 0x25, B: 0x2f, A: 0xb0}
	// colLayerPoint marks imported KML/KMZ reference points — a cool blue,
	// distinct from the "поражение" accent red used for the user's own
	// incidents, so the two are never visually confused on the map.
	colLayerPoint = color.NRGBA{R: 0x3a, G: 0x8e, B: 0xe4, A: 0xff}
)

func (m *modernTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colBackground
	case theme.ColorNameForeground:
		return colForeground
	case theme.ColorNameButton:
		return colButton
	case theme.ColorNameDisabled:
		return colDisabled
	case theme.ColorNamePrimary:
		return colPrimary
	case theme.ColorNameHover, theme.ColorNameFocus:
		return colHover
	case theme.ColorNameInputBackground:
		return colInputBg
	case theme.ColorNameSeparator:
		return colSeparator
	case theme.ColorNameSuccess:
		return colSuccess
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0xe4, G: 0x4a, B: 0x3a, A: 0x55}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0x8a, G: 0x92, B: 0x9d, A: 0xff}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0x3a, G: 0x42, B: 0x4e, A: 0xaa}
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground, theme.ColorNameHeaderBackground:
		return colOverlayBg
	case theme.ColorNameShadow:
		return colShadow
	case theme.ColorNameDisabledButton:
		return colButton
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (m *modernTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (m *modernTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (m *modernTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInlineIcon:
		return 20
	case theme.SizeNameScrollBar:
		return 12
	case theme.SizeNameScrollBarSmall:
		return 6
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNameInputBorder:
		return 1.5
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	}
	return theme.DefaultTheme().Size(name)
}

// NewTheme returns the app's custom modern dark theme.
func NewTheme() fyne.Theme { return &modernTheme{} }

// incidentColors is the preset palette users pick a marker/card accent
// color from (see the color swatches in openIncidentDialog).
var incidentColors = []string{
	"#e44a3a", // red (default/"поражение" accent)
	"#e0972a", // orange
	"#dbc22e", // yellow
	"#3da35f", // green
	"#3a8ee4", // blue
	"#9159e0", // purple
	"#9aa1ab", // grey
}

// parseHexColor parses a "#rrggbb" string into a color.Color, falling back
// to colPrimary for anything empty or malformed — in particular, incidents
// saved before per-incident colors existed.
func parseHexColor(hex string) color.Color {
	if len(hex) == 7 && hex[0] == '#' {
		r, err1 := strconv.ParseUint(hex[1:3], 16, 8)
		g, err2 := strconv.ParseUint(hex[3:5], 16, 8)
		b, err3 := strconv.ParseUint(hex[5:7], 16, 8)
		if err1 == nil && err2 == nil && err3 == nil {
			return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
		}
	}
	return colPrimary
}

// calloutFillColor mixes an incident's accent color into the callout card's
// dark base so the card clearly reads as "this incident's color" inside,
// not just on the border, while staying dark enough for the light text
// drawn on top of it to stay legible.
func calloutFillColor(accent color.Color) color.Color {
	r, g, b, _ := accent.RGBA()
	base := colOverlayBg
	mix := func(a uint32, base uint8) uint8 {
		return uint8((a>>8)*3/5 + uint32(base)*2/5)
	}
	return color.NRGBA{R: mix(r, base.R), G: mix(g, base.G), B: mix(b, base.B), A: 0xff}
}
