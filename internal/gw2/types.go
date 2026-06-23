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
	Guilds       []string `json:"guilds"`
	GuildLeader  []string `json:"guild_leader"`
	LastModified string   `json:"last_modified"`
}

// Guild is the subset of /v2/guild/:id the collector tracks. The operational
// fields (level, influence, ...) are populated only for guilds the key has
// permission to see; otherwise they are zero.
type Guild struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Tag            string `json:"tag"`
	Level          int    `json:"level"`
	Influence      int64  `json:"influence"`
	Aetherium      int64  `json:"aetherium"`
	Resonance      int64  `json:"resonance"`
	Favor          int64  `json:"favor"`
	MemberCount    int    `json:"member_count"`
	MemberCapacity int    `json:"member_capacity"`
}

// GuildTreasuryEntry is one entry of /v2/guild/:id/treasury.
type GuildTreasuryEntry struct {
	ItemID int   `json:"item_id"`
	Count  int64 `json:"count"`
}

// GuildStashSection is one vault section of /v2/guild/:id/stash.
type GuildStashSection struct {
	UpgradeID int     `json:"upgrade_id"`
	Size      int64   `json:"size"`
	Coins     int64   `json:"coins"`
	Inventory []*Slot `json:"inventory"`
}

// GuildStorageEntry is one entry of /v2/guild/:id/storage.
type GuildStorageEntry struct {
	ID    int   `json:"id"`
	Count int64 `json:"count"`
}

// GuildLogEntry is one entry of /v2/guild/:id/log (fields vary by type).
type GuildLogEntry struct {
	ID        int64  `json:"id"`
	Time      string `json:"time"`
	Type      string `json:"type"`
	User      string `json:"user"`
	Operation string `json:"operation"`
	ItemID    int    `json:"item_id"`
	Count     int64  `json:"count"`
	Coins     int64  `json:"coins"`
	Motd      string `json:"motd"`
	UpgradeID int    `json:"upgrade_id"`
	Action    string `json:"action"`
	NewRank   string `json:"new_rank"`
	OldRank   string `json:"old_rank"`
}

// WvWMatch is the subset of /v2/wvw/matches the collector tracks. The score-like
// objects are keyed by team color ("red"/"blue"/"green").
type WvWMatch struct {
	ID            string           `json:"id"`
	Scores        map[string]int64 `json:"scores"`
	VictoryPoints map[string]int64 `json:"victory_points"`
	Kills         map[string]int64 `json:"kills"`
	Deaths        map[string]int64 `json:"deaths"`
	Worlds        map[string]int   `json:"worlds"`
	AllWorlds     map[string][]int `json:"all_worlds"`
	Maps          []struct {
		Type       string `json:"type"`
		Objectives []struct {
			Type          string `json:"type"`
			Owner         string `json:"owner"` // Red/Blue/Green/Neutral
			PointsTick    int64  `json:"points_tick"`
			YaksDelivered int64  `json:"yaks_delivered"`
		} `json:"objectives"`
	} `json:"maps"`
}

// WinLoss is the win/loss record shape used across pvp/stats.
type WinLoss struct {
	Wins       int64 `json:"wins"`
	Losses     int64 `json:"losses"`
	Desertions int64 `json:"desertions"`
	Byes       int64 `json:"byes"`
	Forfeits   int64 `json:"forfeits"`
}

// PvPStats is the subset of /v2/pvp/stats the collector tracks.
type PvPStats struct {
	PvPRank       int                `json:"pvp_rank"`
	PvPRankPoints int                `json:"pvp_rank_points"`
	Aggregate     WinLoss            `json:"aggregate"`
	Professions   map[string]WinLoss `json:"professions"`
	Ladders       map[string]WinLoss `json:"ladders"`
}

// PvPStanding is one entry of /v2/pvp/standings (empty when no active season).
type PvPStanding struct {
	SeasonID string `json:"season_id"`
	Current  struct {
		Rating      int64 `json:"rating"`
		Division    int64 `json:"division"`
		Tier        int64 `json:"tier"`
		Points      int64 `json:"points"`
		TotalPoints int64 `json:"total_points"`
	} `json:"current"`
	Best struct {
		Division    int64 `json:"division"`
		Tier        int64 `json:"tier"`
		TotalPoints int64 `json:"total_points"`
	} `json:"best"`
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

// Transaction is one entry of /v2/commerce/transactions/history/{buys,sells}.
type Transaction struct {
	ID        int64  `json:"id"`
	ItemID    int    `json:"item_id"`
	Price     int64  `json:"price"`
	Quantity  int64  `json:"quantity"`
	Created   string `json:"created"`
	Purchased string `json:"purchased"`
}

// ItemPrice is one entry of /v2/commerce/prices: aggregated best bid/ask.
type ItemPrice struct {
	ID   int `json:"id"`
	Buys struct {
		Quantity  int64 `json:"quantity"`
		UnitPrice int64 `json:"unit_price"`
	} `json:"buys"`
	Sells struct {
		Quantity  int64 `json:"quantity"`
		UnitPrice int64 `json:"unit_price"`
	} `json:"sells"`
}

// Item is the subset of /v2/items the collector uses (for id→name on tracked items).
type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Exchange is the response of /v2/commerce/exchange/{coins,gems}.
type Exchange struct {
	CoinsPerGem int64 `json:"coins_per_gem"`
	Quantity    int64 `json:"quantity"`
}

// Delivery is the response of /v2/commerce/delivery (the trading-post pickup box).
type Delivery struct {
	Coins int64 `json:"coins"`
	Items []struct {
		ID    int   `json:"id"`
		Count int64 `json:"count"`
	} `json:"items"`
}

// Quest is the subset of /v2/quests used for story-completion mapping.
type Quest struct {
	ID    int `json:"id"`
	Story int `json:"story"`
}

// StoryDef is the subset of /v2/stories: a story belongs to a season (GUID).
type StoryDef struct {
	ID     int    `json:"id"`
	Season string `json:"season"`
}

// StorySeason is /v2/stories/seasons: a season id (GUID) and its name.
type StorySeason struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Collection pairs an account unlock endpoint (the unlocked set) with its
// reference index endpoint (the full set), so the collector can report both an
// unlocked count and a completion total per collection.
type Collection struct {
	Name        string // metric label, e.g. "skins"
	AccountPath string // e.g. "account/skins"
	RefPath     string // e.g. "skins"
}

// Collections is the set of unlock collections the collector tracks.
var Collections = []Collection{
	{"skins", "account/skins", "skins"},
	{"dyes", "account/dyes", "colors"},
	{"minis", "account/minis", "minis"},
	{"outfits", "account/outfits", "outfits"},
	{"gliders", "account/gliders", "gliders"},
	{"mount_skins", "account/mounts/skins", "mounts/skins"},
	{"mount_types", "account/mounts/types", "mounts/types"},
	{"finishers", "account/finishers", "finishers"},
	{"novelties", "account/novelties", "novelties"},
	{"titles", "account/titles", "titles"},
	{"mailcarriers", "account/mailcarriers", "mailcarriers"},
	{"recipes", "account/recipes", "recipes"},
	{"jadebots", "account/jadebots", "jadebots"},
	{"skiffs", "account/skiffs", "skiffs"},
}

// WizardsVault is /v2/account/wizardsvault/{daily,weekly,special}. The meta_*
// fields are present for daily/weekly and absent (zero) for special.
type WizardsVault struct {
	MetaProgressCurrent  int64 `json:"meta_progress_current"`
	MetaProgressComplete int64 `json:"meta_progress_complete"`
	MetaRewardClaimed    bool  `json:"meta_reward_claimed"`
	Objectives           []struct {
		ID               int   `json:"id"`
		Acclaim          int64 `json:"acclaim"`
		ProgressCurrent  int64 `json:"progress_current"`
		ProgressComplete int64 `json:"progress_complete"`
		Claimed          bool  `json:"claimed"`
	} `json:"objectives"`
}

// Mastery is one entry of /v2/account/masteries.
type Mastery struct {
	ID    int `json:"id"`
	Level int `json:"level"`
}

// MasteryPoints is /v2/account/mastery/points: earned/spent points per region.
type MasteryPoints struct {
	Totals []struct {
		Region string `json:"region"`
		Spent  int64  `json:"spent"`
		Earned int64  `json:"earned"`
	} `json:"totals"`
	Unlocked []int `json:"unlocked"`
}

// LuckAmount is one entry of /v2/account/luck (the id is the string "luck").
type LuckAmount struct {
	ID    string `json:"id"`
	Value int64  `json:"value"`
}

// MaterialAmount is one entry of /v2/account/materials.
type MaterialAmount struct {
	ID       int   `json:"id"`
	Category int   `json:"category"`
	Count    int64 `json:"count"`
}

// Slot is a generic bank/inventory slot. Empty slots in the API are null and
// decode to a nil *Slot, so counting non-nil entries gives slots used.
type Slot struct {
	ID    int   `json:"id"`
	Count int64 `json:"count"`
}

// Character is the subset of a /v2/characters?ids=all overview object the
// collector uses. The bulk overview embeds far more (equipment, tabs, recipes,
// ...) — later slices will read those from the same response.
type Character struct {
	Name       string               `json:"name"`
	Race       string               `json:"race"`
	Gender     string               `json:"gender"`
	Profession string               `json:"profession"`
	Level      int                  `json:"level"`
	Age        int64                `json:"age"`    // playtime in seconds (monotonic)
	Deaths     int64                `json:"deaths"` // lifetime deaths (monotonic)
	Created    string               `json:"created"`
	Bags       []*CharacterBag      `json:"bags"`     // present in the ?ids=all overview
	Crafting   []CraftingDiscipline `json:"crafting"` // present in the ?ids=all overview
}

// CharacterBag is one equipped bag with its slots (null slots = empty).
type CharacterBag struct {
	ID        int     `json:"id"`
	Size      int     `json:"size"`
	Inventory []*Slot `json:"inventory"`
}

// CraftingDiscipline is one entry of a character's crafting list.
type CraftingDiscipline struct {
	Discipline string `json:"discipline"`
	Rating     int    `json:"rating"`
	Active     bool   `json:"active"`
}

// AccountAchievement is one entry of /v2/account/achievements.
type AccountAchievement struct {
	ID       int   `json:"id"`
	Current  int   `json:"current"`
	Max      int   `json:"max"`
	Done     bool  `json:"done"`
	Repeated int64 `json:"repeated"`
}

// AchievementDef is the subset of /v2/achievements used to compute AP.
type AchievementDef struct {
	ID    int      `json:"id"`
	Flags []string `json:"flags"`
	Tiers []struct {
		Count  int   `json:"count"`
		Points int64 `json:"points"`
	} `json:"tiers"`
	PointCap int64 `json:"point_cap"`
}

// ProgressionEntry is one entry of /v2/account/progression (luck + fractal augments).
type ProgressionEntry struct {
	ID    string `json:"id"`
	Value int64  `json:"value"`
}

// LegendaryArmoryEntry is one entry of /v2/account/legendaryarmory.
type LegendaryArmoryEntry struct {
	ID    int   `json:"id"`
	Count int64 `json:"count"`
}

// LegendaryArmoryDef is one entry of /v2/legendaryarmory (max copies per item).
type LegendaryArmoryDef struct {
	ID       int   `json:"id"`
	MaxCount int64 `json:"max_count"`
}
