package i18n

func init() {
	labels := [...]string{
		"Find in history",
		"Pause recording",
		"No history entries",
		"Clear all command palette history?",
		"Clear history",
		"Delete selected",
		"Newest first",
		"Select history entries",
		"Confirm clear history",
		"Open the original workspace to run this again",
	}
	translations := map[string][len(labels)]string{
		"zh-Hans": {"搜索历史记录", "暂停记录", "暂无历史记录", "清空命令面板的所有历史记录？", "清空历史", "删除所选", "最近使用优先", "多选历史记录", "确认清空历史", "请打开原工作区后再次运行"},
		"zh-Hant": {"搜尋歷史記錄", "暫停記錄", "沒有歷史記錄", "清除命令面板的所有歷史記錄？", "清除歷史", "刪除所選", "最近使用優先", "多選歷史記錄", "確認清除歷史", "請開啟原工作區以再次執行"},
		"ja":      {"履歴を検索", "記録を一時停止", "履歴はありません", "コマンドパレットの履歴をすべて消去しますか？", "履歴を消去", "選択した項目を削除", "新しい順", "履歴項目を複数選択", "履歴の消去を確認", "元のワークスペースを開いて再実行してください"},
		"es":      {"Buscar en el historial", "Pausar el registro", "No hay entradas en el historial", "¿Borrar todo el historial de la paleta de comandos?", "Borrar historial", "Eliminar selección", "Más recientes primero", "Seleccionar varias entradas", "Confirmar borrado del historial", "Abre el espacio de trabajo original para volver a ejecutar"},
		"fr":      {"Rechercher dans l’historique", "Suspendre l’enregistrement", "Aucune entrée dans l’historique", "Effacer tout l’historique de la palette de commandes ?", "Effacer l’historique", "Supprimer la sélection", "Les plus récentes en premier", "Sélectionner des entrées", "Confirmer l’effacement de l’historique", "Ouvrez l’espace de travail d’origine pour relancer"},
	}
	for language, entries := range translations {
		for i, label := range labels {
			if _, exists := Translations[language][label]; !exists {
				Translations[language][label] = entries[i]
			}
		}
	}
}
