package i18n

var reflogPagingTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Loading older reflog entries":         "正在加载更早的引用日志条目",
		"Loading older reflog entries…":        "正在加载更早的引用日志条目…",
		"Could not load older reflog entries.": "无法加载更早的引用日志条目。",
		"Beginning of reflog":                  "已到引用日志的开头",
	},
	"zh-Hant": {
		"Loading older reflog entries":         "正在載入更早的參考日誌條目",
		"Loading older reflog entries…":        "正在載入更早的參考日誌條目…",
		"Could not load older reflog entries.": "無法載入更早的參考日誌條目。",
		"Beginning of reflog":                  "已到參考日誌的開頭",
	},
	"ja": {
		"Loading older reflog entries":         "古いリフログを読み込み中",
		"Loading older reflog entries…":        "古いリフログを読み込み中…",
		"Could not load older reflog entries.": "古いリフログを読み込めませんでした。",
		"Beginning of reflog":                  "リフログの最初です",
	},
	"es": {
		"Loading older reflog entries":         "Cargando entradas anteriores del reflog",
		"Loading older reflog entries…":        "Cargando entradas anteriores del reflog…",
		"Could not load older reflog entries.": "No se pudieron cargar entradas anteriores del reflog.",
		"Beginning of reflog":                  "Principio del reflog",
	},
	"fr": {
		"Loading older reflog entries":         "Chargement d’anciennes entrées du reflog",
		"Loading older reflog entries…":        "Chargement d’anciennes entrées du reflog…",
		"Could not load older reflog entries.": "Impossible de charger les anciennes entrées du reflog.",
		"Beginning of reflog":                  "Début du reflog",
	},
}

func init() {
	for language, additions := range reflogPagingTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
