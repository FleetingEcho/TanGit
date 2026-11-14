package app

// Plain-folder opening shares the Home launcher and local Editor wording.
var editorOnlyTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Open a folder": "打开文件夹",
		"Browse, search, edit files, and use a terminal — no Git repository required.": "浏览、搜索、编辑文件并使用终端，无需 Git 仓库。",
		"Open mode":                       "打开模式",
		"Git repository":                  "Git 仓库",
		"Folder":                          "文件夹",
		"Recent folders":                  "最近使用的文件夹",
		"Search recent folders":           "搜索最近使用的文件夹",
		"No folders match your search":    "没有符合搜索条件的文件夹",
		"Open this folder in your editor": "在编辑器中打开此文件夹",
		"Open a folder to begin":          "打开文件夹开始使用",
		"Browse, search, and edit files or use a terminal without initializing Git.": "浏览、搜索和编辑文件或使用终端，无需初始化 Git。",
		"Workspace folder":         "工作区文件夹",
		"This folder has no files": "此文件夹没有文件",
		"Terminal: open a Git repository or folder path directly in TanGit": "终端：直接在 TanGit 中打开 Git 仓库或文件夹路径",
	},
	"zh-Hant": {
		"Open a folder": "開啟資料夾",
		"Browse, search, edit files, and use a terminal — no Git repository required.": "瀏覽、搜尋、編輯檔案並使用終端機，無需 Git 儲存庫。",
		"Open mode":                       "開啟模式",
		"Git repository":                  "Git 儲存庫",
		"Folder":                          "資料夾",
		"Recent folders":                  "最近使用的資料夾",
		"Search recent folders":           "搜尋最近使用的資料夾",
		"No folders match your search":    "沒有符合搜尋條件的資料夾",
		"Open this folder in your editor": "在編輯器中開啟此資料夾",
		"Open a folder to begin":          "開啟資料夾開始使用",
		"Browse, search, and edit files or use a terminal without initializing Git.": "瀏覽、搜尋和編輯檔案或使用終端機，無需初始化 Git。",
		"Workspace folder":         "工作區資料夾",
		"This folder has no files": "此資料夾沒有檔案",
		"Terminal: open a Git repository or folder path directly in TanGit": "終端機：直接在 TanGit 中開啟 Git 儲存庫或資料夾路徑",
	},
	"ja": {
		"Open a folder": "フォルダーを開く",
		"Browse, search, edit files, and use a terminal — no Git repository required.": "ファイルの参照、検索、編集とターミナルを Git リポジトリなしで利用できます。",
		"Open mode":                       "オープンモード",
		"Git repository":                  "Git リポジトリ",
		"Folder":                          "フォルダー",
		"Recent folders":                  "最近使用したフォルダー",
		"Search recent folders":           "最近使用したフォルダーを検索",
		"No folders match your search":    "検索に一致するフォルダーがありません",
		"Open this folder in your editor": "このフォルダーをエディターで開く",
		"Open a folder to begin":          "フォルダーを開いて開始",
		"Browse, search, and edit files or use a terminal without initializing Git.": "Git を初期化せずに、ファイルの参照、検索、編集やターミナルを利用できます。",
		"Workspace folder":         "ワークスペースフォルダー",
		"This folder has no files": "このフォルダーにはファイルがありません",
		"Terminal: open a Git repository or folder path directly in TanGit": "ターミナル：Git リポジトリまたはフォルダーのパスを TanGit で直接開く",
	},
	"es": {
		"Open a folder": "Abrir una carpeta",
		"Browse, search, edit files, and use a terminal — no Git repository required.": "Explora, busca y edita archivos y usa una terminal sin necesidad de un repositorio Git.",
		"Open mode":                       "Modo de apertura",
		"Git repository":                  "Repositorio Git",
		"Folder":                          "Carpeta",
		"Recent folders":                  "Carpetas recientes",
		"Search recent folders":           "Buscar en carpetas recientes",
		"No folders match your search":    "Ninguna carpeta coincide con la búsqueda",
		"Open this folder in your editor": "Abrir esta carpeta en el editor",
		"Open a folder to begin":          "Abrir una carpeta para empezar",
		"Browse, search, and edit files or use a terminal without initializing Git.": "Explora, busca y edita archivos o usa una terminal sin inicializar Git.",
		"Workspace folder":         "Carpeta de trabajo",
		"This folder has no files": "Esta carpeta no contiene archivos",
		"Terminal: open a Git repository or folder path directly in TanGit": "Terminal: abre directamente una ruta de repositorio Git o carpeta en TanGit",
	},
	"fr": {
		"Open a folder": "Ouvrir un dossier",
		"Browse, search, edit files, and use a terminal — no Git repository required.": "Parcourez, recherchez et modifiez des fichiers et utilisez un terminal, sans dépôt Git.",
		"Open mode":                       "Mode d’ouverture",
		"Git repository":                  "Dépôt Git",
		"Folder":                          "Dossier",
		"Recent folders":                  "Dossiers récents",
		"Search recent folders":           "Rechercher dans les dossiers récents",
		"No folders match your search":    "Aucun dossier ne correspond à votre recherche",
		"Open this folder in your editor": "Ouvrir ce dossier dans votre éditeur",
		"Open a folder to begin":          "Ouvrir un dossier pour commencer",
		"Browse, search, and edit files or use a terminal without initializing Git.": "Parcourez, recherchez et modifiez des fichiers ou utilisez un terminal sans initialiser Git.",
		"Workspace folder":         "Dossier de travail",
		"This folder has no files": "Ce dossier ne contient aucun fichier",
		"Terminal: open a Git repository or folder path directly in TanGit": "Terminal : ouvrez directement un chemin de dépôt Git ou de dossier dans TanGit",
	},
}

func init() {
	for language, additions := range editorOnlyTranslations {
		catalog := translations[language]
		for english, translated := range additions {
			catalog[english] = translated
		}
	}
}
