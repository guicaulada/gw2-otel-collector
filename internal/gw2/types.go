package gw2

// Account is the subset of /v2/account fields the collector uses.
// Field shapes verified empirically — see docs/api-empirical-findings.md.
type Account struct {
	Name         string   `json:"name"`
	Age          int64    `json:"age"` // total account age in seconds (monotonic)
	World        int      `json:"world"`
	Created      string   `json:"created"`
	Access       []string `json:"access"`
	Commander    bool     `json:"commander"`
	FractalLevel int      `json:"fractal_level"`
	DailyAP      int      `json:"daily_ap"`
	MonthlyAP    int      `json:"monthly_ap"`
	WvW          struct {
		TeamID int `json:"team_id"`
		Rank   int `json:"rank"`
	} `json:"wvw"`
	LastModified string `json:"last_modified"`
}

// CurrencyAmount is one entry of /v2/account/wallet.
type CurrencyAmount struct {
	ID    int   `json:"id"`
	Value int64 `json:"value"`
}

// Currency is one entry of the static /v2/currencies reference endpoint.
type Currency struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Character is the subset of a /v2/characters?ids=all overview object the
// collector uses. The bulk overview embeds far more (equipment, tabs, recipes,
// ...) — later slices will read those from the same response.
type Character struct {
	Name       string `json:"name"`
	Race       string `json:"race"`
	Gender     string `json:"gender"`
	Profession string `json:"profession"`
	Level      int    `json:"level"`
	Age        int64  `json:"age"`    // playtime in seconds (monotonic)
	Deaths     int64  `json:"deaths"` // lifetime deaths (monotonic)
	Created    string `json:"created"`
}
