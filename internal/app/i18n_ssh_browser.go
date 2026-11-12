package app

func init() {
	additions := map[string][]string{
		"No folder loaded": {"尚未加载文件夹", "尚未載入資料夾", "フォルダー未読み込み", "Ninguna carpeta cargada", "Aucun dossier chargé"},
		"Choose a Git-managed folder or enable repository creation, then click Open.": {"请选择由 Git 管理的文件夹，或允许创建仓库后点击打开。", "請選擇由 Git 管理的資料夾，或允許建立儲存庫後按下開啟。", "Git 管理下のフォルダーを選ぶか、リポジトリ作成を許可してから開いてください。", "Elige una carpeta gestionada por Git o permite crear un repositorio y pulsa Abrir.", "Choisissez un dossier géré par Git ou autorisez la création d’un dépôt, puis cliquez sur Ouvrir."},
		"Remote folder does not exist.":                                               {"远程文件夹不存在。", "遠端資料夾不存在。", "リモートフォルダーが存在しません。", "La carpeta remota no existe.", "Le dossier distant n’existe pas."},
		"Remote folder contents":                                                      {"远程文件夹内容", "遠端資料夾內容", "リモートフォルダーの内容", "Contenido de la carpeta remota", "Contenu du dossier distant"},
		"Cancel creating remote entry":                                                {"取消创建远程项目", "取消建立遠端項目", "リモート項目の作成をキャンセル", "Cancelar creación remota", "Annuler la création distante"},
		"Create remote entry":                                                         {"创建远程项目", "建立遠端項目", "リモート項目を作成", "Crear elemento remoto", "Créer un élément distant"},
		"Open creates a Git repository here if needed.":                               {"点击打开时，如有需要将在此处创建 Git 仓库。", "按下開啟時，如有需要會在此處建立 Git 儲存庫。", "必要に応じて、開くときにここに Git リポジトリを作成します。", "Abrir crea un repositorio Git aquí si es necesario.", "Ouvrir crée ici un dépôt Git si nécessaire."},
		"Browse remote folder":                                                        {"浏览远程文件夹", "瀏覽遠端資料夾", "リモートフォルダーを参照", "Explorar carpeta remota", "Parcourir le dossier distant"},
		"Parent folder":                                                               {"上级文件夹", "上層資料夾", "親フォルダー", "Carpeta superior", "Dossier parent"},
		"Allow creating a Git repository":                                             {"允许创建 Git 仓库", "允許建立 Git 儲存庫", "Git リポジトリの作成を許可", "Permitir crear un repositorio Git", "Autoriser la création d’un dépôt Git"},
		"This folder is empty":                                                        {"此文件夹为空", "此資料夾為空", "このフォルダーは空です", "Esta carpeta está vacía", "Ce dossier est vide"},
		"This folder is managed by Git":                                               {"此文件夹由 Git 管理", "此資料夾由 Git 管理", "このフォルダーは Git で管理されています", "Esta carpeta está gestionada por Git", "Ce dossier est géré par Git"},
		"Loading remote folder…":                                                      {"正在加载远程文件夹…", "正在載入遠端資料夾…", "リモートフォルダーを読み込み中…", "Cargando carpeta remota…", "Chargement du dossier distant…"},
		"Browse remote folder %s":                                                     {"浏览远程文件夹 %s", "瀏覽遠端資料夾 %s", "リモートフォルダー %s を参照", "Explorar carpeta remota %s", "Parcourir le dossier distant %s"},
	}
	catalogs := []map[string]string{zhHans, zhHant, japanese, spanish, french}
	for english, values := range additions {
		for i, catalog := range catalogs {
			catalog[english] = values[i]
		}
	}
}
