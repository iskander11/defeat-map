package geo

// Settlement is a built-in gazetteer entry used to seed city search and to
// place a sensible default marker position when the user only picks a city
// by name instead of clicking an exact point on the map.
type Settlement struct {
	Name string
	Lat  float64
	Lon  float64
}

// CrimeaSettlements lists the major cities and towns of Crimea with their
// approximate centre coordinates, used to pre-populate city search.
var CrimeaSettlements = []Settlement{
	{"Симферополь", 44.9521, 34.1024},
	{"Севастополь", 44.6166, 33.5254},
	{"Керчь", 45.3467, 36.4700},
	{"Евпатория", 45.1908, 33.3661},
	{"Ялта", 44.4952, 34.1667},
	{"Феодосия", 45.0333, 35.3833},
	{"Джанкой", 45.7086, 34.3856},
	{"Саки", 45.1333, 33.5833},
	{"Судак", 44.8478, 34.9714},
	{"Алушта", 44.6767, 34.4053},
	{"Армянск", 46.1069, 33.6858},
	{"Красноперекопск", 45.9539, 33.7864},
	{"Бахчисарай", 44.7522, 33.8656},
	{"Белогорск", 45.1225, 34.6011},
	{"Старый Крым", 45.0064, 35.0928},
	{"Щёлкино", 45.4133, 35.8256},
	{"Гвардейское", 45.0342, 33.9822},
	{"Октябрьское", 45.3722, 34.0431},
	{"Нижнегорский", 45.4547, 34.6975},
	{"Раздольное", 45.5450, 33.4839},
	{"Черноморское", 45.5083, 32.7217},
	{"Первомайское", 45.5719, 33.7369},
	{"Красногвардейское", 45.3467, 34.3236},
	{"Кировское", 45.1567, 35.1969},
	{"Ленино", 45.2925, 35.7739},
	{"Советский", 45.2864, 34.8975},
	{"Приморский (Феодосия)", 45.0392, 35.3486},
	{"Гвардейское (Симферополь)", 45.0342, 33.9822},
	{"Новофёдоровка", 45.0794, 33.4283},
	{"Форос", 44.3928, 33.7908},
	{"Гурзуф", 44.5464, 34.2789},
	{"Массандра", 44.5028, 34.1994},
	{"Николаевка", 45.0672, 33.5311},
}

// CrimeaBBox is a padded bounding box covering the whole peninsula, used for
// bundling the low-zoom offline overview and for detecting when the user
// pans outside the region.
var CrimeaBBox = BBox{MinLat: 44.30, MaxLat: 46.22, MinLon: 32.40, MaxLon: 36.75}
