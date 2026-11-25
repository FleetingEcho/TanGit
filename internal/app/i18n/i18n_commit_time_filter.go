package i18n

var commitTimeFilterTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Time range":   "时间范围",
		"Last 30 days": "最近 30 天",
		"Last 90 days": "最近 90 天",
	},
	"zh-Hant": {
		"Time range":   "時間範圍",
		"Last 30 days": "最近 30 天",
		"Last 90 days": "最近 90 天",
	},
	"ja": {
		"Time range":   "期間",
		"Last 30 days": "過去30日",
		"Last 90 days": "過去90日",
	},
	"es": {
		"Time range":   "Intervalo de tiempo",
		"Last 30 days": "Últimos 30 días",
		"Last 90 days": "Últimos 90 días",
	},
	"fr": {
		"Time range":   "Période",
		"Last 30 days": "30 derniers jours",
		"Last 90 days": "90 derniers jours",
	},
}

func init() {
	for language, additions := range commitTimeFilterTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
