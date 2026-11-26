package i18n

// The reflog count tooltip: how far the list has loaded and how to see more.
var reflogCountTooltipTranslations = map[string]map[string]string{
	"zh-Hans": {
		"%d reflog entries loaded so far. Scroll down to load more, or use the filter to search by operation, hash or author.": "已加载 %d 条引用日志。向下滚动可加载更多，或使用筛选按操作、哈希或作者搜索。",
		"%d reflog entries shown. This is the whole reflog.":                                                                   "已显示 %d 条引用日志，这就是全部。",
	},
	"zh-Hant": {
		"%d reflog entries loaded so far. Scroll down to load more, or use the filter to search by operation, hash or author.": "已載入 %d 筆參照日誌。向下捲動可載入更多，或使用篩選依操作、雜湊或作者搜尋。",
		"%d reflog entries shown. This is the whole reflog.":                                                                   "已顯示 %d 筆參照日誌，這就是全部。",
	},
	"ja": {
		"%d reflog entries loaded so far. Scroll down to load more, or use the filter to search by operation, hash or author.": "%d 件のリフログを読み込み済みです。下にスクロールするとさらに読み込めます。フィルターで操作・ハッシュ・作成者から検索することもできます。",
		"%d reflog entries shown. This is the whole reflog.":                                                                   "%d 件のリフログを表示しています。これで全部です。",
	},
	"es": {
		"%d reflog entries loaded so far. Scroll down to load more, or use the filter to search by operation, hash or author.": "%d entradas del reflog cargadas hasta ahora. Desplázate hacia abajo para cargar más o usa el filtro para buscar por operación, hash o autor.",
		"%d reflog entries shown. This is the whole reflog.":                                                                   "%d entradas del reflog mostradas. Este es todo el reflog.",
	},
	"fr": {
		"%d reflog entries loaded so far. Scroll down to load more, or use the filter to search by operation, hash or author.": "%d entrées du reflog chargées pour l’instant. Faites défiler vers le bas pour en charger d’autres, ou utilisez le filtre pour chercher par opération, hash ou auteur.",
		"%d reflog entries shown. This is the whole reflog.":                                                                   "%d entrées du reflog affichées. C’est tout le reflog.",
	},
}

func init() {
	for language, additions := range reflogCountTooltipTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			if _, exists := catalog[english]; !exists {
				catalog[english] = translated
			}
		}
	}
}
