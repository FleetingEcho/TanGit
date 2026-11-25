package i18n

var settingsSearchTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Search settings":                       "搜索设置",
		"Find an option by name or description": "按名称或说明查找选项",
		"Clear settings search":                 "清除设置搜索",
		"No settings match your search":         "没有匹配的设置",
	},
	"zh-Hant": {
		"Search settings":                       "搜尋設定",
		"Find an option by name or description": "按名稱或說明尋找選項",
		"Clear settings search":                 "清除設定搜尋",
		"No settings match your search":         "沒有符合的設定",
	},
	"ja": {
		"Search settings":                       "設定を検索",
		"Find an option by name or description": "名前または説明でオプションを検索",
		"Clear settings search":                 "設定の検索をクリア",
		"No settings match your search":         "一致する設定がありません",
	},
	"es": {
		"Search settings":                       "Buscar ajustes",
		"Find an option by name or description": "Buscar una opción por nombre o descripción",
		"Clear settings search":                 "Borrar búsqueda de ajustes",
		"No settings match your search":         "No hay ajustes que coincidan con tu búsqueda",
	},
	"fr": {
		"Search settings":                       "Rechercher dans les paramètres",
		"Find an option by name or description": "Rechercher une option par nom ou description",
		"Clear settings search":                 "Effacer la recherche dans les paramètres",
		"No settings match your search":         "Aucun paramètre ne correspond à votre recherche",
	},
}

func init() {
	for language, additions := range settingsSearchTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
