package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

type CommodityMeta struct {
	Symbol       string
	Name         string
	ShortName    string
	Unit         string
	Exchange     string
	Description  string
	Keywords     string
	About        string
	PriceFactors []string
	TradingHours string
}

var commodities = map[string]CommodityMeta{
	"WTI": {
		Symbol:    "WTI",
		Name:      "WTI Crude Oil",
		ShortName: "WTI",
		Unit:      "USD/barrel",
		Exchange:  "NYMEX",
		Description: "Track the live WTI crude oil price per barrel with real-time NYMEX data, " +
			"interactive candlestick charts, historical trends, and breaking energy market news. " +
			"West Texas Intermediate is the primary U.S. oil benchmark.",
		Keywords: "WTI crude oil price, WTI price today, West Texas Intermediate, crude oil price per barrel, " +
			"NYMEX oil price, WTI crude oil chart, US oil benchmark, oil futures price",
		About: "West Texas Intermediate (WTI) crude oil is a light, sweet crude oil that serves as the primary benchmark for oil prices in the United States. Traded on the New York Mercantile Exchange (NYMEX) under ticker CL, WTI futures are among the most actively traded commodity contracts in the world. The crude is extracted from oil fields across Texas, Louisiana, and North Dakota, and is delivered at the Cushing, Oklahoma storage hub — the most important oil storage facility in North America. WTI has an API gravity of about 39.6 degrees and a sulfur content of approximately 0.24%, making it ideal for refining into gasoline and other high-value petroleum products. As the U.S. benchmark, WTI prices directly influence fuel costs, inflation expectations, and monetary policy decisions. Major factors driving WTI prices include OPEC+ production decisions, U.S. shale output, Strategic Petroleum Reserve releases, refinery utilization rates, and weekly EIA inventory reports.",
		PriceFactors: []string{
			"OPEC+ production quotas and compliance levels",
			"U.S. crude oil inventory levels (EIA weekly report)",
			"Cushing, Oklahoma storage hub capacity",
			"U.S. shale production growth (Permian Basin output)",
			"Federal Reserve interest rate policy and USD strength",
			"Geopolitical tensions in oil-producing regions",
			"Seasonal refinery maintenance and driving demand",
		},
		TradingHours: "Sun–Fri 6:00 PM – 5:00 PM ET (23-hour session)",
	},
	"BRENT": {
		Symbol:    "BRENT",
		Name:      "Brent Crude Oil",
		ShortName: "Brent",
		Unit:      "USD/barrel",
		Exchange:  "ICE",
		Description: "Track the live Brent crude oil price per barrel with real-time ICE data, " +
			"interactive candlestick charts, historical trends, and breaking energy market news. " +
			"Brent crude is the global oil price benchmark.",
		Keywords: "Brent crude oil price, Brent price today, Brent crude chart, ICE Brent, " +
			"international oil price, Brent oil futures, global oil benchmark, North Sea oil price",
		About: "Brent crude oil is the world's most widely used benchmark for international oil pricing, referenced in approximately two-thirds of all global crude oil contracts. Traded on the Intercontinental Exchange (ICE) under ticker BZ, Brent is a light, sweet crude originally sourced from the North Sea's Brent oil field between Scotland and Norway. Today, the Brent benchmark is based on a basket of North Sea crudes including Brent, Forties, Oseberg, Ekofisk, and Troll (collectively known as BFOET). With an API gravity around 38 degrees and sulfur content of about 0.37%, Brent is slightly heavier and more sulfurous than WTI but remains highly desirable for refining. The spread between Brent and WTI — known as the Brent-WTI spread — is closely watched by traders as an indicator of global supply-demand dynamics versus U.S. domestic conditions.",
		PriceFactors: []string{
			"OPEC+ output decisions and geopolitical risk premiums",
			"North Sea production levels and field maintenance",
			"Asian and European refinery demand",
			"Brent-WTI spread dynamics",
			"Global tanker freight rates and shipping disruptions",
			"Chinese and Indian crude import volumes",
			"Sanctions and trade restrictions on oil-producing nations",
		},
		TradingHours: "Sun–Fri 8:00 PM – 6:00 PM ET (22-hour session)",
	},
	"NATGAS": {
		Symbol:    "NATGAS",
		Name:      "Natural Gas",
		ShortName: "Natural Gas",
		Unit:      "USD/MMBtu",
		Exchange:  "NYMEX",
		Description: "Track the live natural gas price with real-time NYMEX Henry Hub data, " +
			"interactive charts, historical trends, and breaking energy market news. " +
			"Natural gas is a key energy commodity for power generation and heating.",
		Keywords: "natural gas price, natural gas price today, Henry Hub natural gas, NYMEX natural gas, " +
			"natural gas chart, natural gas futures, gas price per MMBtu, energy prices",
		About: "Natural gas is a fossil fuel composed primarily of methane (CH₄) and is one of the most important energy commodities globally. Traded on the NYMEX under ticker NG, U.S. natural gas futures are priced in dollars per million British thermal units (MMBtu) and are delivered at the Henry Hub in Erath, Louisiana — the most important natural gas pricing point in North America. Natural gas accounts for roughly 40% of U.S. electricity generation and is widely used for residential heating, industrial processes, and as a petrochemical feedstock. The U.S. has become the world's largest natural gas producer thanks to the shale revolution, and is now a major LNG exporter. Natural gas prices are highly seasonal, with demand peaking in winter (heating) and summer (cooling/power generation), and are significantly more volatile than crude oil prices.",
		PriceFactors: []string{
			"Weather forecasts (heating degree days and cooling degree days)",
			"EIA weekly natural gas storage report",
			"U.S. LNG export terminal feed gas volumes",
			"Shale gas production (Marcellus, Haynesville, Permian associated gas)",
			"Power generation fuel switching (gas vs. coal vs. renewables)",
			"Pipeline infrastructure constraints and regional basis differentials",
			"Hurricane season impacts on Gulf of Mexico production",
		},
		TradingHours: "Sun–Fri 6:00 PM – 5:00 PM ET (23-hour session)",
	},
	"HEATING": {
		Symbol:    "HEATING",
		Name:      "Heating Oil",
		ShortName: "Heating Oil",
		Unit:      "USD/gallon",
		Exchange:  "NYMEX",
		Description: "Track the live heating oil price with real-time NYMEX data, " +
			"interactive candlestick charts, historical trends, and breaking energy market news. " +
			"Heating oil futures are a key indicator for distillate demand.",
		Keywords: "heating oil price, heating oil price today, NYMEX heating oil, heating oil futures, " +
			"heating oil chart, No. 2 fuel oil price, home heating oil price, distillate price",
		About: "Heating oil (No. 2 fuel oil) is a refined petroleum product derived from crude oil distillation, closely related to diesel fuel. Traded on the NYMEX under ticker HO, heating oil futures are priced in U.S. dollars per gallon and serve as a benchmark for distillate fuel pricing worldwide. Approximately 5.5 million U.S. households rely on heating oil as their primary heating fuel, with the heaviest concentration in the Northeast. Heating oil prices are strongly seasonal, typically rising in the fall as distributors build winter inventories and peaking during cold snaps. The crack spread — the difference between heating oil prices and crude oil — is a key metric for refinery profitability. Heating oil prices also serve as a proxy for diesel fuel costs, making them an important indicator for transportation and logistics industries.",
		PriceFactors: []string{
			"Northeast U.S. winter weather severity",
			"Distillate fuel inventory levels (PADD 1 stocks)",
			"Refinery utilization rates and planned maintenance",
			"Crude oil input costs (crack spread dynamics)",
			"Diesel demand from trucking and transportation",
			"International distillate trade flows",
			"Renewable diesel and biodiesel blending mandates",
		},
		TradingHours: "Sun–Fri 6:00 PM – 5:00 PM ET (23-hour session)",
	},
	"RBOB": {
		Symbol:    "RBOB",
		Name:      "RBOB Gasoline",
		ShortName: "RBOB",
		Unit:      "USD/gallon",
		Exchange:  "NYMEX",
		Description: "Track the live RBOB gasoline futures price with real-time NYMEX data, " +
			"interactive candlestick charts, historical trends, and breaking energy market news. " +
			"RBOB is the benchmark for U.S. gasoline prices.",
		Keywords: "RBOB gasoline price, RBOB price today, gasoline futures, NYMEX gasoline, " +
			"RBOB gasoline chart, reformulated gasoline, gas futures price, fuel price today",
		About: "RBOB (Reformulated Blendstock for Oxygenate Blending) gasoline is the primary futures contract used to price gasoline in the United States. Traded on the NYMEX under ticker RB, RBOB represents unfinished gasoline that requires the addition of ethanol before retail sale. RBOB replaced the older reformulated gasoline contract in 2006 to reflect the industry's shift from MTBE to ethanol as an oxygenate additive. U.S. consumers use approximately 9 million barrels of gasoline per day, making it one of the most consumed petroleum products globally. Gasoline prices at the pump are directly influenced by RBOB futures, along with taxes, distribution costs, and retail margins. Prices follow strong seasonal patterns — rising in spring as refineries switch to costlier summer-blend formulations and peaking around Memorial Day as driving season begins.",
		PriceFactors: []string{
			"Seasonal driving demand (summer vs. winter blend specifications)",
			"U.S. refinery utilization and gasoline output",
			"EIA weekly gasoline inventory data",
			"Crude oil feedstock costs",
			"RVP (Reid Vapor Pressure) seasonal blend switchovers",
			"Ethanol blending requirements (RFS mandates)",
			"Electric vehicle adoption trends and long-term demand outlook",
		},
		TradingHours: "Sun–Fri 6:00 PM – 5:00 PM ET (23-hour session)",
	},
	"OPEC": {
		Symbol:    "OPEC",
		Name:      "OPEC Basket",
		ShortName: "OPEC Basket",
		Unit:      "USD/barrel",
		Exchange:  "OPEC",
		Description: "Track the live OPEC Reference Basket price with daily data, " +
			"interactive charts, historical trends, and breaking OPEC news. " +
			"The OPEC Basket is a weighted average of oil prices from OPEC member nations.",
		Keywords: "OPEC basket price, OPEC oil price, OPEC reference basket, OPEC price today, " +
			"OPEC crude oil price, OPEC basket chart, OPEC production, oil cartel price",
		About: "The OPEC Reference Basket (ORB) is a weighted average of oil prices from crude streams produced by the 13 OPEC member nations. Introduced in 2005, the basket replaced earlier single-crude benchmarks to better reflect the diverse quality of OPEC output. Current basket components include crudes such as Arab Light (Saudi Arabia), Bonny Light (Nigeria), Girassol (Angola), and Iran Heavy, among others. OPEC — the Organization of the Petroleum Exporting Countries — was founded in 1960 and collectively controls approximately 30% of global oil production and holds about 80% of the world's proven reserves. The expanded OPEC+ alliance, which includes Russia and other non-OPEC producers, coordinates production quotas that directly impact global supply and prices. OPEC ministerial meetings, typically held every six months, are among the most market-moving events in the energy sector.",
		PriceFactors: []string{
			"OPEC+ production quota decisions and compliance rates",
			"Spare production capacity (primarily Saudi Arabia)",
			"Geopolitical stability in member nations",
			"Global oil demand growth forecasts (IEA, EIA, OPEC monthly reports)",
			"Non-OPEC supply competition (U.S. shale, Brazil, Guyana)",
			"Voluntary production cuts by key members",
			"OPEC+ meeting outcomes and policy signaling",
		},
		TradingHours: "Published daily by OPEC Secretariat (Vienna)",
	},
	"DUBAI": {
		Symbol:    "DUBAI",
		Name:      "Dubai Crude",
		ShortName: "Dubai",
		Unit:      "USD/barrel",
		Exchange:  "DME",
		Description: "Track the live Dubai crude oil price with real-time data, " +
			"interactive charts, historical trends, and breaking energy market news. " +
			"Dubai crude is the key pricing benchmark for Middle Eastern oil exports to Asia.",
		Keywords: "Dubai crude oil price, Dubai crude price today, Dubai crude chart, DME crude, " +
			"Middle East oil price, Asian oil benchmark, Dubai Mercantile Exchange, sour crude price",
		About: "Dubai crude oil is a medium-sour crude that serves as the primary benchmark for pricing Persian Gulf oil exports to the Asia-Pacific region. Traded on the Dubai Mercantile Exchange (DME), Dubai crude has an API gravity of about 31 degrees and a sulfur content of around 2%, making it heavier and more sulfurous than WTI or Brent. Dubai crude, along with Oman crude, is used to price the majority of Middle Eastern oil sold to Asian refiners — a trade flow that represents the largest crude oil market in the world. Saudi Aramco, the world's largest oil company, uses Dubai/Oman as the benchmark for setting its official selling prices (OSPs) to Asian buyers. The Dubai-Brent spread (known as the EFS, or Exchange of Futures for Swaps) is a key indicator of the relative value of sour versus sweet crude in global markets.",
		PriceFactors: []string{
			"Middle East geopolitical risk and shipping route security (Strait of Hormuz)",
			"Asian refinery demand (China, India, Japan, South Korea)",
			"Saudi Aramco official selling price (OSP) adjustments",
			"Dubai-Brent spread (EFS) dynamics",
			"OPEC+ production policy for Gulf producers",
			"Sour crude supply-demand balance",
			"Asian strategic petroleum reserve policies",
		},
		TradingHours: "Mon–Fri 4:30 AM – 4:15 PM Dubai Time (GST)",
	},
	"MURBAN": {
		Symbol:    "MURBAN",
		Name:      "Murban Crude",
		ShortName: "Murban",
		Unit:      "USD/barrel",
		Exchange:  "ICE",
		Description: "Track the live Murban crude oil price with real-time ICE Futures Abu Dhabi data, " +
			"interactive charts, historical trends, and breaking energy market news. " +
			"Murban is Abu Dhabi's flagship crude grade and a growing Asian benchmark.",
		Keywords: "Murban crude price, Murban oil price today, ICE Murban futures, Abu Dhabi crude, " +
			"Murban crude chart, IFAD Murban, UAE oil price, Middle East crude benchmark",
		About: "Murban crude oil is Abu Dhabi's flagship light crude grade and one of the newest exchange-traded oil benchmarks in the world. Launched on ICE Futures Abu Dhabi (IFAD) in March 2021, the Murban futures contract represents Abu Dhabi National Oil Company's (ADNOC) ambition to establish a transparent, market-driven benchmark for Middle Eastern crude. With an API gravity of approximately 40 degrees and sulfur content of about 0.8%, Murban is a high-quality crude well-suited for refining into gasoline and naphtha. ADNOC produces roughly 2 million barrels per day of Murban, making it one of the most liquid physical crude streams in the Middle East. The transition from retroactive OSP-based pricing to exchange-traded futures marked a significant shift in how Middle Eastern crude is priced, offering greater transparency and hedging opportunities for international refiners.",
		PriceFactors: []string{
			"ADNOC production levels and export allocations",
			"UAE OPEC+ quota compliance",
			"Asian refinery intake and seasonal demand",
			"IFAD contract liquidity and open interest growth",
			"Murban-Brent and Murban-Dubai differentials",
			"Middle East refinery capacity additions",
			"Geopolitical stability in the UAE and broader Gulf region",
		},
		TradingHours: "Sun–Fri various sessions (ICE Futures Abu Dhabi)",
	},
	"WCS": {
		Symbol:    "WCS",
		Name:      "Western Canadian Select",
		ShortName: "WCS",
		Unit:      "USD/barrel",
		Exchange:  "CME",
		Description: "Track the live Western Canadian Select price with real-time data, " +
			"interactive charts, historical trends, and breaking energy market news. " +
			"WCS is the benchmark for Canadian heavy crude oil.",
		Keywords: "Western Canadian Select price, WCS price today, Canadian crude oil price, WCS crude chart, " +
			"heavy crude oil price, Alberta oil price, Canadian oil sands, WCS differential",
		About: "Western Canadian Select (WCS) is the benchmark price for Canadian heavy crude oil, a blend of heavy conventional and oil sands bitumen with a diluent of sweet synthetic and condensate. With an API gravity of about 20.5 degrees and sulfur content around 3.5%, WCS is classified as heavy-sour crude, requiring specialized (complex) refineries with coking capacity to process. Canada is the world's fourth-largest oil producer, with the majority of its output coming from the Athabasca oil sands in Alberta. WCS typically trades at a significant discount to WTI, known as the WCS differential, which reflects quality differences and transportation costs. Pipeline capacity constraints — particularly before the completion of the Trans Mountain Expansion — have historically caused the WCS discount to widen dramatically. The completion of major pipeline projects and growing U.S. Gulf Coast refinery demand for heavy crude have been key factors in narrowing this differential.",
		PriceFactors: []string{
			"WCS-WTI differential (quality and transportation discount)",
			"Pipeline capacity (Trans Mountain, Keystone, Enbridge Mainline)",
			"Alberta oil sands production levels and planned maintenance",
			"U.S. Gulf Coast heavy crude refinery demand",
			"Canadian government production curtailment orders",
			"Rail-by-crude economics as pipeline alternative",
			"Competition from other heavy crudes (Mexico Maya, Venezuela)",
		},
		TradingHours: "Mon–Fri (CME Globex, various sessions)",
	},
	"GASOIL": {
		Symbol:    "GASOIL",
		Name:      "ICE Gasoil",
		ShortName: "Gasoil",
		Unit:      "USD/tonne",
		Exchange:  "ICE",
		Description: "Track the live ICE Gasoil futures price with real-time data, " +
			"interactive charts, historical trends, and breaking energy market news. " +
			"ICE Gasoil is the European benchmark for diesel and middle distillate prices.",
		Keywords: "ICE Gasoil price, gasoil price today, gasoil futures, ICE gasoil chart, " +
			"European diesel price, middle distillate price, diesel futures, gasoil Rotterdam",
		About: "ICE Gasoil (formerly known as IPE Gasoil) is the primary benchmark for middle distillate pricing in Europe and is traded on the Intercontinental Exchange (ICE) in London. Priced in U.S. dollars per metric tonne, the contract is physically deliverable in the Amsterdam-Rotterdam-Antwerp (ARA) hub, the largest petroleum storage and trading hub in Europe. Gasoil is a middle distillate product that encompasses diesel fuel, heating oil, and jet fuel — making it one of the most economically significant refined products globally. European diesel demand is driven by the continent's large diesel vehicle fleet, industrial activity, and agricultural machinery. The gasoil crack spread (gasoil price minus crude oil cost) is the key measure of European refining profitability. Russia's invasion of Ukraine in 2022 and subsequent EU sanctions on Russian petroleum products fundamentally restructured European distillate trade flows and significantly tightened the market.",
		PriceFactors: []string{
			"European diesel demand (transportation, industrial, heating)",
			"ARA (Amsterdam-Rotterdam-Antwerp) distillate inventory levels",
			"EU sanctions on Russian petroleum product imports",
			"Refinery utilization rates across Europe",
			"Gasoil crack spread and refining margins",
			"Middle East and Asian distillate export flows to Europe",
			"IMO shipping fuel regulations (low-sulfur marine gasoil demand)",
		},
		TradingHours: "Mon–Fri 1:00 AM – 6:30 PM London Time (GMT/BST)",
	},
}

type commodityPageData struct {
	Meta         CommodityMeta
	PageTitle    string
	OGTitle      string
	Canonical    string
	Price        string
	Change       string
	ChangePct    string
	High         string
	Low          string
	Volume       string
	Contract     string
	IsPositive   bool
	Sign         string
	HasFactors   bool
}

var commodityTmpl *template.Template

func InitCommodityTemplate(path string) error {
	var err error
	commodityTmpl, err = template.ParseFiles(path)
	return err
}

func (a *API) ServeCommodityPage(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	meta, ok := commodities[symbol]
	if !ok {
		http.NotFound(w, r)
		return
	}

	data := commodityPageData{
		Meta:       meta,
		PageTitle:  fmt.Sprintf("%s Price Today — Live Chart & Real-Time Data | Live Oil Prices", meta.Name),
		OGTitle:    fmt.Sprintf("%s Price Today — Live Chart & Market Data", meta.Name),
		Canonical:  fmt.Sprintf("https://liveoilprices.com/commodity/%s", meta.Symbol),
		HasFactors: len(meta.PriceFactors) > 0,
	}

	prices := a.market.GetPrices()
	for _, p := range prices {
		if p.Symbol == symbol {
			data.Price = fmt.Sprintf("%.2f", p.Price)
			data.Change = fmt.Sprintf("%.2f", p.Change)
			data.ChangePct = fmt.Sprintf("%.2f", p.ChangePct)
			data.High = fmt.Sprintf("%.2f", p.High)
			data.Low = fmt.Sprintf("%.2f", p.Low)
			data.Contract = p.Contract
			data.IsPositive = p.Change >= 0
			if data.IsPositive {
				data.Sign = "+"
			}

			if p.Volume >= 1_000_000 {
				data.Volume = fmt.Sprintf("%.1fM", float64(p.Volume)/1_000_000)
			} else if p.Volume >= 1_000 {
				data.Volume = fmt.Sprintf("%dK", p.Volume/1_000)
			} else {
				data.Volume = fmt.Sprintf("%d", p.Volume)
			}

			data.PageTitle = fmt.Sprintf("%s Price Today $%s (%s%s%%) — Live Oil Prices",
				meta.Name, data.Price, data.Sign, data.ChangePct)
			break
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := commodityTmpl.Execute(w, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
