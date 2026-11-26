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
		"Back to Command Palette",
		"Exit selection mode",
		"Search mode",
		"Resume code change search",
		"%s and %d more files",
		"Tag",
		"Record history",
		"Recording",
		"Not recording",
		"Keep latest %d",
		"Entries to keep",
		"Entries saved in Recent",
		"New searches and commands are saved to Recent",
		"New searches and commands are not saved to Recent",
	}
	translations := map[string][len(labels)]string{
		"zh-Hans": {"搜索历史记录", "暂停记录", "暂无历史记录", "清空命令面板的所有历史记录？", "清空历史", "删除所选", "最近使用优先", "多选历史记录", "确认清空历史", "请打开原工作区后再次运行", "返回命令面板", "退出多选", "搜索方式", "继续上次的代码更改搜索", "%s 及另外 %d 个文件", "标签", "记录历史", "正在记录", "未记录", "保留最近 %d 条", "保留的条数", "“最近”中已保存的条目", "新的搜索和命令会保存到“最近”", "新的搜索和命令不会保存到“最近”"},
		"zh-Hant": {"搜尋歷史記錄", "暫停記錄", "沒有歷史記錄", "清除命令面板的所有歷史記錄？", "清除歷史", "刪除所選", "最近使用優先", "多選歷史記錄", "確認清除歷史", "請開啟原工作區以再次執行", "返回命令面板", "結束多選", "搜尋方式", "繼續上次的程式碼變更搜尋", "%s 及另外 %d 個檔案", "標籤", "記錄歷史", "正在記錄", "未記錄", "保留最近 %d 筆", "保留的筆數", "「最近」中已儲存的項目", "新的搜尋和命令會儲存到「最近」", "新的搜尋和命令不會儲存到「最近」"},
		"ja":      {"履歴を検索", "記録を一時停止", "履歴はありません", "コマンドパレットの履歴をすべて消去しますか？", "履歴を消去", "選択した項目を削除", "新しい順", "履歴項目を複数選択", "履歴の消去を確認", "元のワークスペースを開いて再実行してください", "コマンドパレットに戻る", "複数選択を終了", "検索モード", "前回のコード変更検索を再開", "%s ほか %d 件のファイル", "タグ", "履歴を記録", "記録中", "記録していません", "最新 %d 件を保持", "保持する件数", "「最近」に保存された項目", "新しい検索とコマンドは「最近」に保存されます", "新しい検索とコマンドは「最近」に保存されません"},
		"es":      {"Buscar en el historial", "Pausar el registro", "No hay entradas en el historial", "¿Borrar todo el historial de la paleta de comandos?", "Borrar historial", "Eliminar selección", "Más recientes primero", "Seleccionar varias entradas", "Confirmar borrado del historial", "Abre el espacio de trabajo original para volver a ejecutar", "Volver a la paleta de comandos", "Salir de la selección múltiple", "Modo de búsqueda", "Reanudar la búsqueda de cambios de código", "%s y %d archivos más", "Etiqueta", "Registrar historial", "Registrando", "Sin registrar", "Conservar las últimas %d", "Entradas que conservar", "Entradas guardadas en Recientes", "Las nuevas búsquedas y comandos se guardan en Recientes", "Las nuevas búsquedas y comandos no se guardan en Recientes"},
		"fr":      {"Rechercher dans l’historique", "Suspendre l’enregistrement", "Aucune entrée dans l’historique", "Effacer tout l’historique de la palette de commandes ?", "Effacer l’historique", "Supprimer la sélection", "Les plus récentes en premier", "Sélectionner des entrées", "Confirmer l’effacement de l’historique", "Ouvrez l’espace de travail d’origine pour relancer", "Retour à la palette de commandes", "Quitter la sélection multiple", "Mode de recherche", "Reprendre la recherche de modifications de code", "%s et %d autres fichiers", "Étiquette", "Enregistrer l’historique", "Enregistrement", "Pas d’enregistrement", "Garder les %d dernières", "Entrées à garder", "Entrées enregistrées dans Récentes", "Les nouvelles recherches et commandes sont enregistrées dans Récentes", "Les nouvelles recherches et commandes ne sont pas enregistrées dans Récentes"},
	}
	for language, entries := range translations {
		for i, label := range labels {
			if _, exists := Translations[language][label]; !exists {
				Translations[language][label] = entries[i]
			}
		}
	}
}
