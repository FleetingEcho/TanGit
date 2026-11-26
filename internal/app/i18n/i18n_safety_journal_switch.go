package i18n

// The safety journal switch and the redesigned Recovery & checkpoints dialog.
var safetyJournalSwitchTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Recovery sections":     "恢复分区",
		"Safety journal":        "安全日志",
		"Safety journal is on":  "安全日志已开启",
		"Safety journal is off": "安全日志已关闭",
		"Save a recovery point before actions so they can be undone": "在执行操作前保存恢复点，以便撤销",
		"Turn the safety journal off":                                "关闭安全日志",
		"Turn the safety journal on":                                 "开启安全日志",
		"Actions run faster while it is off, but stash, discard, checkout, merge and similar actions are not saved and cannot be undone. Rebase, reset and patch edits still keep a recovery point.": "关闭后操作会更快，但暂存、丢弃、检出、合并等操作不会被保存，也无法撤销。变基、重置和补丁编辑仍会保留恢复点。",
		"The safety journal is off. New actions are not backed up and cannot be undone.":                                                                                                             "安全日志已关闭。新的操作不会被备份，也无法撤销。",
		"Turn on": "开启",
		"Recovery points and undo for your actions":            "操作的恢复点与撤销",
		"Nothing recorded yet":                                 "暂无记录",
		"Nothing is recorded while the safety journal is off.": "安全日志关闭期间不会记录任何内容。",
		"Recovery points appear here":                          "恢复点会显示在这里",
	},
	"zh-Hant": {
		"Recovery sections":     "復原分頁",
		"Safety journal":        "安全日誌",
		"Safety journal is on":  "安全日誌已開啟",
		"Safety journal is off": "安全日誌已關閉",
		"Save a recovery point before actions so they can be undone": "在執行操作前儲存復原點，以便復原",
		"Turn the safety journal off":                                "關閉安全日誌",
		"Turn the safety journal on":                                 "開啟安全日誌",
		"Actions run faster while it is off, but stash, discard, checkout, merge and similar actions are not saved and cannot be undone. Rebase, reset and patch edits still keep a recovery point.": "關閉後操作會更快，但擱置、捨棄、簽出、合併等操作不會被儲存，也無法復原。重定基底、重設和修補編輯仍會保留復原點。",
		"The safety journal is off. New actions are not backed up and cannot be undone.":                                                                                                             "安全日誌已關閉。新的操作不會被備份，也無法復原。",
		"Turn on": "開啟",
		"Recovery points and undo for your actions":            "操作的復原點與復原",
		"Nothing recorded yet":                                 "尚無記錄",
		"Nothing is recorded while the safety journal is off.": "安全日誌關閉期間不會記錄任何內容。",
		"Recovery points appear here":                          "復原點會顯示在這裡",
	},
	"ja": {
		"Recovery sections":     "復元のセクション",
		"Safety journal":        "セーフティジャーナル",
		"Safety journal is on":  "セーフティジャーナルはオンです",
		"Safety journal is off": "セーフティジャーナルはオフです",
		"Save a recovery point before actions so they can be undone": "操作の前に復元ポイントを保存し、元に戻せるようにします",
		"Turn the safety journal off":                                "セーフティジャーナルをオフにする",
		"Turn the safety journal on":                                 "セーフティジャーナルをオンにする",
		"Actions run faster while it is off, but stash, discard, checkout, merge and similar actions are not saved and cannot be undone. Rebase, reset and patch edits still keep a recovery point.": "オフにすると操作は速くなりますが、スタッシュ、破棄、チェックアウト、マージなどは保存されず、元に戻せません。リベース、リセット、パッチ編集では引き続き復元ポイントが残ります。",
		"The safety journal is off. New actions are not backed up and cannot be undone.":                                                                                                             "セーフティジャーナルはオフです。新しい操作はバックアップされず、元に戻せません。",
		"Turn on": "オンにする",
		"Recovery points and undo for your actions":            "操作の復元ポイントと取り消し",
		"Nothing recorded yet":                                 "まだ記録はありません",
		"Nothing is recorded while the safety journal is off.": "セーフティジャーナルがオフの間は何も記録されません。",
		"Recovery points appear here":                          "復元ポイントはここに表示されます",
	},
	"es": {
		"Recovery sections":     "Secciones de recuperación",
		"Safety journal":        "Diario de seguridad",
		"Safety journal is on":  "El diario de seguridad está activado",
		"Safety journal is off": "El diario de seguridad está desactivado",
		"Save a recovery point before actions so they can be undone": "Guarda un punto de recuperación antes de las acciones para poder deshacerlas",
		"Turn the safety journal off":                                "Desactivar el diario de seguridad",
		"Turn the safety journal on":                                 "Activar el diario de seguridad",
		"Actions run faster while it is off, but stash, discard, checkout, merge and similar actions are not saved and cannot be undone. Rebase, reset and patch edits still keep a recovery point.": "Las acciones son más rápidas con el diario desactivado, pero guardar en el stash, descartar, cambiar de rama, fusionar y similares no se guardan y no se pueden deshacer. Rebase, reset y ediciones de parches siguen conservando un punto de recuperación.",
		"The safety journal is off. New actions are not backed up and cannot be undone.":                                                                                                             "El diario de seguridad está desactivado. Las acciones nuevas no tienen copia y no se pueden deshacer.",
		"Turn on": "Activar",
		"Recovery points and undo for your actions":            "Puntos de recuperación y deshacer para tus acciones",
		"Nothing recorded yet":                                 "Aún no hay nada registrado",
		"Nothing is recorded while the safety journal is off.": "No se registra nada mientras el diario de seguridad está desactivado.",
		"Recovery points appear here":                          "Los puntos de recuperación aparecen aquí",
	},
	"fr": {
		"Recovery sections":     "Sections de récupération",
		"Safety journal":        "Journal de sécurité",
		"Safety journal is on":  "Le journal de sécurité est activé",
		"Safety journal is off": "Le journal de sécurité est désactivé",
		"Save a recovery point before actions so they can be undone": "Enregistre un point de récupération avant les actions pour pouvoir les annuler",
		"Turn the safety journal off":                                "Désactiver le journal de sécurité",
		"Turn the safety journal on":                                 "Activer le journal de sécurité",
		"Actions run faster while it is off, but stash, discard, checkout, merge and similar actions are not saved and cannot be undone. Rebase, reset and patch edits still keep a recovery point.": "Les actions sont plus rapides quand il est désactivé, mais le remisage, l’abandon, le changement de branche, la fusion et les actions similaires ne sont pas sauvegardés et ne peuvent pas être annulés. Le rebase, la réinitialisation et les modifications de patch gardent un point de récupération.",
		"The safety journal is off. New actions are not backed up and cannot be undone.":                                                                                                             "Le journal de sécurité est désactivé. Les nouvelles actions ne sont pas sauvegardées et ne peuvent pas être annulées.",
		"Turn on": "Activer",
		"Recovery points and undo for your actions":            "Points de récupération et annulation de vos actions",
		"Nothing recorded yet":                                 "Rien d’enregistré pour l’instant",
		"Nothing is recorded while the safety journal is off.": "Rien n’est enregistré tant que le journal de sécurité est désactivé.",
		"Recovery points appear here":                          "Les points de récupération apparaissent ici",
	},
}

func init() {
	for language, additions := range safetyJournalSwitchTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
