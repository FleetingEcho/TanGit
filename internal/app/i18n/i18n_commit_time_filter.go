package i18n

var commitTimeFilterTranslations = map[string]map[string]string{
	"zh-Hans": {
		"From":                   "从",
		"To":                     "到",
		"Any time":               "不限时间",
		"Choose a date and time": "选择日期和时间",
		"Leave this end open":    "不限制这一端",
		"Since %s":               "自 %s",
		"Until %s":               "至 %s",
		"Set %s time":            "设置%s时间",
		"%s date":                "%s日期",
		"%s time":                "%s时间",
		"Clear %s time":          "清除%s时间",
		"Time range":             "时间范围",
		"Last 30 days":           "最近 30 天",
		"Last 90 days":           "最近 90 天",
	},
	"zh-Hant": {
		"From":                   "從",
		"To":                     "到",
		"Any time":               "不限時間",
		"Choose a date and time": "選擇日期和時間",
		"Leave this end open":    "不限制這一端",
		"Since %s":               "自 %s",
		"Until %s":               "至 %s",
		"Set %s time":            "設定%s時間",
		"%s date":                "%s日期",
		"%s time":                "%s時間",
		"Clear %s time":          "清除%s時間",
		"Time range":             "時間範圍",
		"Last 30 days":           "最近 30 天",
		"Last 90 days":           "最近 90 天",
	},
	"ja": {
		"From":                   "開始",
		"To":                     "終了",
		"Any time":               "指定なし",
		"Choose a date and time": "日付と時刻を選択",
		"Leave this end open":    "この端を指定しない",
		"Since %s":               "%s 以降",
		"Until %s":               "%s まで",
		"Set %s time":            "%sの時刻を設定",
		"%s date":                "%sの日付",
		"%s time":                "%sの時刻",
		"Clear %s time":          "%sの時刻をクリア",
		"Time range":             "期間",
		"Last 30 days":           "過去30日",
		"Last 90 days":           "過去90日",
	},
	"es": {
		"From":                   "Desde",
		"To":                     "Hasta",
		"Any time":               "Cualquier momento",
		"Choose a date and time": "Elegir fecha y hora",
		"Leave this end open":    "Dejar este extremo abierto",
		"Since %s":               "Desde %s",
		"Until %s":               "Hasta %s",
		"Set %s time":            "Fijar hora: %s",
		"%s date":                "Fecha: %s",
		"%s time":                "Hora: %s",
		"Clear %s time":          "Borrar hora: %s",
		"Time range":             "Intervalo de tiempo",
		"Last 30 days":           "Últimos 30 días",
		"Last 90 days":           "Últimos 90 días",
	},
	"fr": {
		"From":                   "Du",
		"To":                     "Au",
		"Any time":               "À tout moment",
		"Choose a date and time": "Choisir une date et une heure",
		"Leave this end open":    "Laisser cette borne ouverte",
		"Since %s":               "Depuis %s",
		"Until %s":               "Jusqu’à %s",
		"Set %s time":            "Définir l’heure : %s",
		"%s date":                "Date : %s",
		"%s time":                "Heure : %s",
		"Clear %s time":          "Effacer l’heure : %s",
		"Time range":             "Période",
		"Last 30 days":           "30 derniers jours",
		"Last 90 days":           "90 derniers jours",
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
