package i18n

// Clone speed options: partial, shallow and sparse clones.
var cloneSpeedTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Clone mode": "克隆方式", "Full clone": "完整克隆",
		"Blobless: full history, files on demand":                                  "无 Blob：完整历史，文件按需下载",
		"Treeless: commits only, fastest":                                          "无 Tree：仅提交，最快",
		"Shallow: recent commits only":                                             "浅克隆：仅最近的提交",
		"Downloads every commit and file. Best for small and medium repositories.": "下载所有提交和文件。适合中小型仓库。",
		"Recommended for large repositories. History, blame and log stay complete; file contents download when Git first needs them.": "大型仓库推荐。历史、blame 和日志保持完整；文件内容在 Git 首次需要时下载。",
		"For builds and one-off checkouts. Browsing old history downloads trees on demand and can be slow.":                           "适合构建和一次性检出。浏览旧历史时会按需下载目录树，可能较慢。",
		"Downloads only the newest commits. Older history, blame and merge bases are unavailable until you fetch more.":               "只下载最新的提交。在获取更多历史之前，旧历史、blame 和合并基础不可用。",
		"Commits to fetch": "获取的提交数", "Enter a positive number of commits.": "请输入正整数的提交数。",
		"Fetch only the cloned branch":                             "只获取克隆的分支",
		"Skips other branches and their history (--single-branch)": "跳过其他分支及其历史 (--single-branch)",
		"Sparse checkout folders (optional)":                       "稀疏检出文件夹（可选）",
		"app, docs/api — empty checks out everything":              "app, docs/api — 留空则检出全部",
		"Sparse checkout was not applied":                          "未能应用稀疏检出",
	},
	"zh-Hant": {
		"Clone mode": "複製方式", "Full clone": "完整複製",
		"Blobless: full history, files on demand":                                  "無 Blob：完整歷史，檔案依需下載",
		"Treeless: commits only, fastest":                                          "無 Tree：僅提交，最快",
		"Shallow: recent commits only":                                             "淺複製：僅最近的提交",
		"Downloads every commit and file. Best for small and medium repositories.": "下載所有提交和檔案。適合中小型儲存庫。",
		"Recommended for large repositories. History, blame and log stay complete; file contents download when Git first needs them.": "大型儲存庫推薦。歷史、blame 和日誌保持完整；檔案內容在 Git 首次需要時下載。",
		"For builds and one-off checkouts. Browsing old history downloads trees on demand and can be slow.":                           "適合建置和一次性簽出。瀏覽舊歷史時會依需下載目錄樹，可能較慢。",
		"Downloads only the newest commits. Older history, blame and merge bases are unavailable until you fetch more.":               "只下載最新的提交。在擷取更多歷史之前，舊歷史、blame 和合併基礎無法使用。",
		"Commits to fetch": "擷取的提交數", "Enter a positive number of commits.": "請輸入正整數的提交數。",
		"Fetch only the cloned branch":                             "只擷取複製的分支",
		"Skips other branches and their history (--single-branch)": "略過其他分支及其歷史 (--single-branch)",
		"Sparse checkout folders (optional)":                       "稀疏簽出資料夾（選填）",
		"app, docs/api — empty checks out everything":              "app, docs/api — 留空則簽出全部",
		"Sparse checkout was not applied":                          "未能套用稀疏簽出",
	},
	"ja": {
		"Clone mode": "クローン方式", "Full clone": "完全クローン",
		"Blobless: full history, files on demand":                                  "Blob なし：履歴は完全、ファイルは必要時に取得",
		"Treeless: commits only, fastest":                                          "Tree なし：コミットのみ、最速",
		"Shallow: recent commits only":                                             "シャロー：最近のコミットのみ",
		"Downloads every commit and file. Best for small and medium repositories.": "すべてのコミットとファイルをダウンロードします。小〜中規模のリポジトリ向けです。",
		"Recommended for large repositories. History, blame and log stay complete; file contents download when Git first needs them.": "大規模リポジトリにおすすめ。履歴、blame、ログは完全なままで、ファイル内容は Git が初めて必要としたときにダウンロードされます。",
		"For builds and one-off checkouts. Browsing old history downloads trees on demand and can be slow.":                           "ビルドや一時的なチェックアウト向け。古い履歴を見るとツリーを都度ダウンロードするため遅くなることがあります。",
		"Downloads only the newest commits. Older history, blame and merge bases are unavailable until you fetch more.":               "最新のコミットのみをダウンロードします。追加でフェッチするまで、古い履歴、blame、マージベースは使えません。",
		"Commits to fetch": "取得するコミット数", "Enter a positive number of commits.": "正のコミット数を入力してください。",
		"Fetch only the cloned branch":                             "クローンするブランチのみ取得",
		"Skips other branches and their history (--single-branch)": "他のブランチとその履歴を取得しません (--single-branch)",
		"Sparse checkout folders (optional)":                       "スパースチェックアウトするフォルダー（任意）",
		"app, docs/api — empty checks out everything":              "app, docs/api — 空欄ですべてをチェックアウト",
		"Sparse checkout was not applied":                          "スパースチェックアウトを適用できませんでした",
	},
	"es": {
		"Clone mode": "Modo de clonación", "Full clone": "Clonación completa",
		"Blobless: full history, files on demand":                                  "Sin blobs: historial completo, archivos bajo demanda",
		"Treeless: commits only, fastest":                                          "Sin árboles: solo commits, lo más rápido",
		"Shallow: recent commits only":                                             "Superficial: solo commits recientes",
		"Downloads every commit and file. Best for small and medium repositories.": "Descarga todos los commits y archivos. Ideal para repositorios pequeños y medianos.",
		"Recommended for large repositories. History, blame and log stay complete; file contents download when Git first needs them.": "Recomendado para repositorios grandes. El historial, blame y log siguen completos; el contenido de los archivos se descarga cuando Git lo necesita por primera vez.",
		"For builds and one-off checkouts. Browsing old history downloads trees on demand and can be slow.":                           "Para compilaciones y checkouts puntuales. Explorar el historial antiguo descarga árboles bajo demanda y puede ser lento.",
		"Downloads only the newest commits. Older history, blame and merge bases are unavailable until you fetch more.":               "Descarga solo los commits más recientes. El historial antiguo, blame y las bases de fusión no están disponibles hasta que obtengas más.",
		"Commits to fetch": "Commits a obtener", "Enter a positive number of commits.": "Introduce un número positivo de commits.",
		"Fetch only the cloned branch":                             "Obtener solo la rama clonada",
		"Skips other branches and their history (--single-branch)": "Omite las demás ramas y su historial (--single-branch)",
		"Sparse checkout folders (optional)":                       "Carpetas de checkout disperso (opcional)",
		"app, docs/api — empty checks out everything":              "app, docs/api — vacío extrae todo",
		"Sparse checkout was not applied":                          "No se aplicó el checkout disperso",
	},
	"fr": {
		"Clone mode": "Mode de clonage", "Full clone": "Clone complet",
		"Blobless: full history, files on demand":                                  "Sans blobs : historique complet, fichiers à la demande",
		"Treeless: commits only, fastest":                                          "Sans arbres : commits seulement, le plus rapide",
		"Shallow: recent commits only":                                             "Superficiel : commits récents seulement",
		"Downloads every commit and file. Best for small and medium repositories.": "Télécharge tous les commits et fichiers. Idéal pour les dépôts petits et moyens.",
		"Recommended for large repositories. History, blame and log stay complete; file contents download when Git first needs them.": "Recommandé pour les grands dépôts. L’historique, le blame et le log restent complets ; le contenu des fichiers est téléchargé quand Git en a besoin.",
		"For builds and one-off checkouts. Browsing old history downloads trees on demand and can be slow.":                           "Pour les builds et les extractions ponctuelles. Parcourir l’ancien historique télécharge les arbres à la demande et peut être lent.",
		"Downloads only the newest commits. Older history, blame and merge bases are unavailable until you fetch more.":               "Télécharge seulement les commits les plus récents. L’ancien historique, le blame et les bases de fusion sont indisponibles tant que vous n’en récupérez pas davantage.",
		"Commits to fetch": "Commits à récupérer", "Enter a positive number of commits.": "Saisissez un nombre positif de commits.",
		"Fetch only the cloned branch":                             "Récupérer uniquement la branche clonée",
		"Skips other branches and their history (--single-branch)": "Ignore les autres branches et leur historique (--single-branch)",
		"Sparse checkout folders (optional)":                       "Dossiers d’extraction partielle (facultatif)",
		"app, docs/api — empty checks out everything":              "app, docs/api — vide pour tout extraire",
		"Sparse checkout was not applied":                          "L’extraction partielle n’a pas été appliquée",
	},
}

func init() {
	for language, additions := range cloneSpeedTranslations {
		catalog := Translations[language]
		for english, translated := range additions {
			catalog[english] = translated
		}
	}
}
