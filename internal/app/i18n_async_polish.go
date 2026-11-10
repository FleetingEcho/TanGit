package app

var asyncCommandPolishTranslations = map[string]map[string]string{
	"zh-Hans": {
		"The command did not stop in time; shutdown reached its wait limit.": "命令未能及时停止，关闭操作已达到等待上限。",
		"A command is already running":                                       "已有命令正在运行",
		"Wait for the current operation to finish, then try again.":          "请等待当前操作完成后再试。",
	},
	"zh-Hant": {
		"The command did not stop in time; shutdown reached its wait limit.": "命令未能及時停止，關閉操作已達到等待上限。",
		"A command is already running":                                       "已有命令正在執行",
		"Wait for the current operation to finish, then try again.":          "請等待目前操作完成後再試。",
	},
	"ja": {
		"The command did not stop in time; shutdown reached its wait limit.": "コマンドが時間内に停止せず、シャットダウンの待機上限に達しました。",
		"A command is already running":                                       "コマンドはすでに実行中です",
		"Wait for the current operation to finish, then try again.":          "現在の操作が完了してから、もう一度試してください。",
	},
	"es": {
		"The command did not stop in time; shutdown reached its wait limit.": "El comando no se detuvo a tiempo; se alcanzó el límite de espera del cierre.",
		"A command is already running":                                       "Ya hay un comando en ejecución",
		"Wait for the current operation to finish, then try again.":          "Espera a que termine la operación actual y vuelve a intentarlo.",
	},
	"fr": {
		"The command did not stop in time; shutdown reached its wait limit.": "La commande ne s’est pas arrêtée à temps ; la durée d’attente maximale de fermeture a été atteinte.",
		"A command is already running":                                       "Une commande est déjà en cours d’exécution",
		"Wait for the current operation to finish, then try again.":          "Attendez la fin de l’opération actuelle, puis réessayez.",
	},
}

func init() {
	for language, entries := range asyncCommandPolishTranslations {
		for english, translated := range entries {
			translations[language][english] = translated
		}
	}
}
