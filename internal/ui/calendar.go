package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var ruMonths = []string{"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
	"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"}
var ruWeekdays = []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}

// CalendarWidget shows a month grid and highlights days that have at least
// one logged incident. Click a day to select it; click a second, later day
// to extend the selection into a range. OnRangeSelected fires after each
// click with the current (start, end) — both equal for a single day.
type CalendarWidget struct {
	widget.BaseWidget

	year, month int            // month: 1-12
	counts      map[string]int // "YYYY-MM-DD" -> incident count

	rangeStart, rangeEnd string
	awaitingEnd          bool

	OnRangeSelected func(start, end string)
	OnMonthChanged  func(year, month int)

	title *widget.Label
	grid  *fyne.Container
	root  *fyne.Container
}

func NewCalendarWidget() *CalendarWidget {
	c := &CalendarWidget{counts: map[string]int{}}
	now := time.Now()
	c.year, c.month = now.Year(), int(now.Month())
	c.ExtendBaseWidget(c)
	c.build()
	return c
}

func (c *CalendarWidget) build() {
	c.title = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { c.shiftMonth(-1) })
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { c.shiftMonth(1) })
	header := container.NewBorder(nil, nil, prev, next, c.title)

	c.grid = container.NewGridWithColumns(7)
	c.root = container.NewVBox(header, weekdayHeader(), c.grid)
	c.refreshGrid()
}

func weekdayHeader() *fyne.Container {
	row := container.NewGridWithColumns(7)
	for _, wd := range ruWeekdays {
		l := widget.NewLabelWithStyle(wd, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
		row.Add(l)
	}
	return row
}

func (c *CalendarWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.root)
}

// SetCounts sets how many incidents fall on each date ("YYYY-MM-DD" keys).
func (c *CalendarWidget) SetCounts(counts map[string]int) {
	c.counts = counts
	c.refreshGrid()
}

// SetRange sets the selected period without firing OnRangeSelected (used to
// reflect a range chosen or cleared elsewhere in the app).
func (c *CalendarWidget) SetRange(start, end string) {
	c.rangeStart, c.rangeEnd = start, end
	c.awaitingEnd = false
	c.refreshGrid()
}

// pickDate implements the two-click range picker: the first click starts a
// new single-day selection, the second click extends it into a range
// (swapping start/end if the user picked an earlier day second).
func (c *CalendarWidget) pickDate(date string) {
	if !c.awaitingEnd {
		c.rangeStart, c.rangeEnd = date, date
		c.awaitingEnd = true
	} else {
		if date < c.rangeStart {
			c.rangeStart, c.rangeEnd = date, c.rangeStart
		} else {
			c.rangeEnd = date
		}
		c.awaitingEnd = false
	}
	c.refreshGrid()
	if c.OnRangeSelected != nil {
		c.OnRangeSelected(c.rangeStart, c.rangeEnd)
	}
}

func (c *CalendarWidget) shiftMonth(delta int) {
	c.month += delta
	for c.month > 12 {
		c.month -= 12
		c.year++
	}
	for c.month < 1 {
		c.month += 12
		c.year--
	}
	c.refreshGrid()
	if c.OnMonthChanged != nil {
		c.OnMonthChanged(c.year, c.month)
	}
}

func (c *CalendarWidget) refreshGrid() {
	c.title.SetText(fmt.Sprintf("%s %d", ruMonths[c.month-1], c.year))

	c.grid.RemoveAll()
	first := time.Date(c.year, time.Month(c.month), 1, 0, 0, 0, 0, time.UTC)
	// Monday-first offset
	offset := (int(first.Weekday()) + 6) % 7
	daysInMonth := time.Date(c.year, time.Month(c.month)+1, 0, 0, 0, 0, 0, time.UTC).Day()

	for i := 0; i < offset; i++ {
		c.grid.Add(widget.NewLabel(""))
	}

	today := time.Now().Format("2006-01-02")

	for d := 1; d <= daysInMonth; d++ {
		date := fmt.Sprintf("%04d-%02d-%02d", c.year, c.month, d)
		count := c.counts[date]
		c.grid.Add(c.dayCell(d, date, count, date == today))
	}

	c.root.Refresh()
}

func (c *CalendarWidget) dayCell(day int, date string, count int, isToday bool) fyne.CanvasObject {
	isEndpoint := c.rangeStart != "" && (date == c.rangeStart || date == c.rangeEnd)
	inRange := c.rangeStart != "" && date > c.rangeStart && date < c.rangeEnd

	var bgColor color.Color = colButton
	switch {
	case isEndpoint:
		bgColor = colPrimary
	case inRange:
		bgColor = color.NRGBA{R: 0xe4, G: 0x4a, B: 0x3a, A: 0x40}
	case count > 0:
		bgColor = incidentHeatColor(count)
	}

	bg := canvas.NewRectangle(bgColor)
	bg.CornerRadius = 6
	if isToday {
		bg.StrokeColor = colForeground
		bg.StrokeWidth = 1.5
	}

	label := widget.NewLabelWithStyle(fmt.Sprint(day), fyne.TextAlignCenter, fyne.TextStyle{Bold: count > 0 || isEndpoint})

	stack := container.NewStack(bg, container.NewPadded(label))
	btn := widget.NewButton("", func() { c.pickDate(date) })
	btn.Importance = widget.LowImportance

	cell := container.NewStack(stack, btn)
	return container.New(layout.NewGridWrapLayout(fyne.NewSize(34, 30)), cell)
}

// incidentHeatColor gives a slightly deeper red the more incidents a day has.
func incidentHeatColor(count int) color.Color {
	intensity := uint8(140)
	if count > 1 {
		intensity = 170
	}
	if count > 3 {
		intensity = 210
	}
	return color.NRGBA{R: intensity, G: 0x33, B: 0x2a, A: 0xff}
}
