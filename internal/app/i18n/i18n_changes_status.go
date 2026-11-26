package i18n

// Status-filter names follow lazygit's compact menu vocabulary.
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

var changesStatusTranslations = map[string]map[string]string{
	"zh-Hant": {
		"Show only staged files":                "僅顯示已暫存檔案",
		"Show only unstaged files":              "僅顯示有未暫存變更的檔案",
		"Show only tracked files":               "僅顯示已追蹤檔案",
		"Show only untracked files":             "僅顯示未追蹤檔案",
		"No filter":                             "不篩選狀態",
		"No files match the current filters":    "沒有符合目前篩選條件的檔案",
		"No files match your search or filters": "沒有符合搜尋或篩選條件的檔案",
	},
	"ja": {
		"Show only staged files":                "ステージ済みファイルのみ表示",
		"Show only unstaged files":              "未ステージの変更があるファイルのみ表示",
		"Show only tracked files":               "追跡対象ファイルのみ表示",
		"Show only untracked files":             "未追跡ファイルのみ表示",
		"No filter":                             "フィルターなし",
		"No files match the current filters":    "現在のフィルターに一致するファイルはありません",
		"No files match your search or filters": "検索条件またはフィルターに一致するファイルはありません",
	},
	"es": {
		"Show only staged files":                "Mostrar solo archivos preparados",
		"Show only unstaged files":              "Mostrar solo archivos con cambios sin preparar",
		"Show only tracked files":               "Mostrar solo archivos con seguimiento",
		"Show only untracked files":             "Mostrar solo archivos sin seguimiento",
		"No filter":                             "Sin filtro de estado",
		"No files match the current filters":    "Ningún archivo coincide con los filtros actuales",
		"No files match your search or filters": "Ningún archivo coincide con la búsqueda o los filtros",
	},
	"fr": {
		"Show only staged files":                "Afficher uniquement les fichiers indexés",
		"Show only unstaged files":              "Afficher uniquement les fichiers avec des modifications non indexées",
		"Show only tracked files":               "Afficher uniquement les fichiers suivis",
		"Show only untracked files":             "Afficher uniquement les fichiers non suivis",
		"No filter":                             "Aucun filtre d’état",
		"No files match the current filters":    "Aucun fichier ne correspond aux filtres actuels",
		"No files match your search or filters": "Aucun fichier ne correspond à la recherche ou aux filtres",
	},
}

func init() {
	catalogs := map[string]map[string]string{"zh-Hans": ZhHans, "zh-Hant": zhHant, "ja": japanese, "es": spanish, "fr": french}
	for _, key := range changesStatusTranslationKeys {
		for _, catalog := range catalogs {
			if _, exists := catalog[key]; !exists {
				catalog[key] = key
			}
		}
	}
	for key, translated := range changesStatusZhHans {
		if existing, exists := ZhHans[key]; !exists || existing == key {
			ZhHans[key] = translated
		}
	}
	for language, translations := range changesStatusTranslations {
		for key, translated := range translations {
			catalogs[language][key] = translated
		}
	}
}
