package i18n

// The commit count tooltip: how far a commit list has loaded and how to see more.
var commitCountTooltipTranslations = map[string]map[string]string{
	"zh-Hans": {
		"%d commits shown.": "已显示 %d 个提交。",
		"%d commits loaded so far. Scroll down to load more, or use the filter to search the commit history.": "已加载 %d 个提交。向下滚动可加载更多，或使用筛选搜索提交历史。",
	},
	"zh-Hant": {
		"%d commits shown.": "已顯示 %d 個提交。",
		"%d commits loaded so far. Scroll down to load more, or use the filter to search the commit history.": "已載入 %d 個提交。向下捲動可載入更多，或使用篩選搜尋提交歷史。",
	},
	"ja": {
		"%d commits shown.": "%d 件のコミットを表示しています。",
		"%d commits loaded so far. Scroll down to load more, or use the filter to search the commit history.": "%d 件のコミットを読み込み済みです。下にスクロールするとさらに読み込めます。フィルターでコミット履歴を検索することもできます。",
	},
	"es": {
		"%d commits shown.": "%d commits mostrados.",
		"%d commits loaded so far. Scroll down to load more, or use the filter to search the commit history.": "%d commits cargados hasta ahora. Desplázate hacia abajo para cargar más o usa el filtro para buscar en el historial.",
	},
	"fr": {
		"%d commits shown.": "%d commits affichés.",
		"%d commits loaded so far. Scroll down to load more, or use the filter to search the commit history.": "%d commits chargés pour l’instant. Faites défiler vers le bas pour en charger d’autres, ou utilisez le filtre pour chercher dans l’historique.",
	},
}

func init() {
	for language, additions := range commitCountTooltipTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
