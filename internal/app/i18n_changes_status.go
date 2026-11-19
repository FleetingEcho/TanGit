package app

// Status-filter names follow lazygit's compact menu vocabulary. Other
// locales retain the normal English fallback until native translations land.
var changesStatusTranslationKeys = []string{
	"Show only staged files",
	"Show only unstaged files",
	"Show only tracked files",
	"Show only untracked files",
	"No filter",
	"No files match the current filters",
	"No files match your search or filters",
}

var changesStatusZhHans = map[string]string{
	"Show only staged files":                "仅显示已暂存文件",
	"Show only unstaged files":              "仅显示未暂存更改的文件",
	"Show only tracked files":               "仅显示已跟踪文件",
	"Show only untracked files":             "仅显示未跟踪文件",
	"No filter":                             "不筛选状态",
	"No files match the current filters":    "没有符合当前筛选条件的文件",
	"No files match your search or filters": "没有符合搜索或筛选条件的文件",
}

func init() {
	catalogs := map[string]map[string]string{"zh-Hans": zhHans, "zh-Hant": zhHant, "ja": japanese, "es": spanish, "fr": french}
	for _, key := range changesStatusTranslationKeys {
		for _, catalog := range catalogs {
			if _, exists := catalog[key]; !exists {
				catalog[key] = key
			}
		}
	}
	for key, translated := range changesStatusZhHans {
		if existing, exists := zhHans[key]; !exists || existing == key {
			zhHans[key] = translated
		}
	}
}
