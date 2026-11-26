package i18n

// The blame pane title for a file HEAD does not have yet.
var blameNewFileTranslations = map[string]map[string]string{
	"zh-Hans": {"Blame · new file, not committed yet": "追溯 · 新文件，尚未提交"},
	"zh-Hant": {"Blame · new file, not committed yet": "追溯 · 新檔案，尚未提交"},
	"ja":      {"Blame · new file, not committed yet": "Blame · 新規ファイル（未コミット）"},
	"es":      {"Blame · new file, not committed yet": "Blame · archivo nuevo, aún sin confirmar"},
	"fr":      {"Blame · new file, not committed yet": "Blame · nouveau fichier, pas encore commité"},
}

func init() {
	for language, additions := range blameNewFileTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
