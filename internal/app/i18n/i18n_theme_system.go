package i18n

// The theme menu's System entry and the file history's copy tooltip.
var themeSystemTranslations = map[string]map[string]string{
	"zh-Hans": {
		"System":             "跟随系统",
		"Copy the file path": "复制文件路径",
	},
	"zh-Hant": {
		"System":             "跟隨系統",
		"Copy the file path": "複製檔案路徑",
	},
	"ja": {
		"System":             "システム",
		"Copy the file path": "ファイルパスをコピー",
	},
	"es": {
		"System":             "Sistema",
		"Copy the file path": "Copiar la ruta del archivo",
	},
	"fr": {
		"System":             "Système",
		"Copy the file path": "Copier le chemin du fichier",
	},
}

func init() {
	for language, additions := range themeSystemTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
