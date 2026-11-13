package app

// Sync recovery copy turns a rejected push or diverged pull into an actionable next step.
var syncTranslations = map[string]map[string]string{
	"zh-Hans": {
		"Pull first, then push": "先拉取，再推送",
		"The remote branch has changes you do not have locally. Pull and resolve any conflicts, then push again.": "远程分支包含本地没有的更改。请先拉取并解决所有冲突，然后再次推送。",
		"Pull now":           "立即拉取",
		"Choose how to pull": "选择拉取方式",
		"Your local and remote branches have diverged. Merge preserves both histories; rebase replays your local commits on top of the remote tip.": "本地分支和远程分支已分叉。合并会保留双方历史；变基会将本地提交重放到远程分支顶端。",
		"Pull with merge":  "通过合并拉取",
		"Pull with rebase": "通过变基拉取",
		"Fetch the upstream branch and merge it into the current branch. This creates a merge commit when needed, and conflicts may require resolution. It does not change your Git configuration.":       "获取上游分支并将其合并到当前分支。必要时会创建合并提交，并可能需要解决冲突。此操作不会更改你的 Git 配置。",
		"Fetch the upstream branch and replay your local commits onto its tip. This rewrites your local commit history, and conflicts may require resolution. It does not change your Git configuration.": "获取上游分支，并将本地提交重放到其顶端。这会重写本地提交历史，并可能需要解决冲突。此操作不会更改你的 Git 配置。",
	},
	"zh-Hant": {
		"Pull first, then push": "先拉取，再推送",
		"The remote branch has changes you do not have locally. Pull and resolve any conflicts, then push again.": "遠端分支包含本機沒有的更改。請先拉取並解決所有衝突，然後再次推送。",
		"Pull now":           "立即拉取",
		"Choose how to pull": "選擇拉取方式",
		"Your local and remote branches have diverged. Merge preserves both histories; rebase replays your local commits on top of the remote tip.": "本機分支和遠端分支已分叉。合併會保留雙方歷史；變基會將本機提交重放到遠端分支頂端。",
		"Pull with merge":  "透過合併拉取",
		"Pull with rebase": "透過變基拉取",
		"Fetch the upstream branch and merge it into the current branch. This creates a merge commit when needed, and conflicts may require resolution. It does not change your Git configuration.":       "取得上游分支並將其合併到目前分支。必要時會建立合併提交，並可能需要解決衝突。此操作不會更改你的 Git 設定。",
		"Fetch the upstream branch and replay your local commits onto its tip. This rewrites your local commit history, and conflicts may require resolution. It does not change your Git configuration.": "取得上游分支，並將本機提交重放到其頂端。這會重寫本機提交歷史，並可能需要解決衝突。此操作不會更改你的 Git 設定。",
	},
	"ja": {
		"Pull first, then push": "プルしてからプッシュしてください",
		"The remote branch has changes you do not have locally. Pull and resolve any conflicts, then push again.": "リモートブランチにローカルにない変更があります。プルして競合を解決してから、もう一度プッシュしてください。",
		"Pull now":           "今すぐプル",
		"Choose how to pull": "プル方法を選択",
		"Your local and remote branches have diverged. Merge preserves both histories; rebase replays your local commits on top of the remote tip.": "ローカルブランチとリモートブランチが分岐しています。マージは両方の履歴を保持し、リベースはローカルコミットをリモートの先端に再生します。",
		"Pull with merge":  "マージでプル",
		"Pull with rebase": "リベースでプル",
		"Fetch the upstream branch and merge it into the current branch. This creates a merge commit when needed, and conflicts may require resolution. It does not change your Git configuration.":       "上流ブランチを取得して現在のブランチにマージします。必要に応じてマージコミットを作成し、競合の解決が必要な場合があります。Git 設定は変更しません。",
		"Fetch the upstream branch and replay your local commits onto its tip. This rewrites your local commit history, and conflicts may require resolution. It does not change your Git configuration.": "上流ブランチを取得し、その先端にローカルコミットを再生します。ローカルのコミット履歴が書き換わり、競合の解決が必要な場合があります。Git 設定は変更しません。",
	},
	"es": {
		"Pull first, then push": "Haz pull antes de volver a hacer push",
		"The remote branch has changes you do not have locally. Pull and resolve any conflicts, then push again.": "La rama remota tiene cambios que no están en tu copia local. Haz pull, resuelve los conflictos y vuelve a hacer push.",
		"Pull now":           "Hacer pull ahora",
		"Choose how to pull": "Elige cómo hacer pull",
		"Your local and remote branches have diverged. Merge preserves both histories; rebase replays your local commits on top of the remote tip.": "Tus ramas local y remota han divergido. La fusión conserva ambos historiales; rebase reproduce tus commits locales sobre la punta remota.",
		"Pull with merge":  "Hacer pull con fusión",
		"Pull with rebase": "Hacer pull con rebase",
		"Fetch the upstream branch and merge it into the current branch. This creates a merge commit when needed, and conflicts may require resolution. It does not change your Git configuration.":       "Obtén la rama ascendente y fusiónala en la rama actual. Esto crea un commit de fusión cuando es necesario y puede requerir resolver conflictos. No cambia tu configuración de Git.",
		"Fetch the upstream branch and replay your local commits onto its tip. This rewrites your local commit history, and conflicts may require resolution. It does not change your Git configuration.": "Obtén la rama ascendente y reproduce tus commits locales sobre su punta. Esto reescribe tu historial de commits local y puede requerir resolver conflictos. No cambia tu configuración de Git.",
	},
	"fr": {
		"Pull first, then push": "Tirez d’abord, puis poussez",
		"The remote branch has changes you do not have locally. Pull and resolve any conflicts, then push again.": "La branche distante contient des changements absents de votre copie locale. Tirez-la, résolvez les conflits, puis poussez à nouveau.",
		"Pull now":           "Tirer maintenant",
		"Choose how to pull": "Choisissez comment tirer",
		"Your local and remote branches have diverged. Merge preserves both histories; rebase replays your local commits on top of the remote tip.": "Vos branches locale et distante ont divergé. La fusion conserve les deux historiques ; le rebasage rejoue vos commits locaux au sommet distant.",
		"Pull with merge":  "Tirer avec fusion",
		"Pull with rebase": "Tirer avec rebasage",
		"Fetch the upstream branch and merge it into the current branch. This creates a merge commit when needed, and conflicts may require resolution. It does not change your Git configuration.":       "Récupère la branche amont et la fusionne dans la branche actuelle. Cela crée un commit de fusion si nécessaire et peut demander une résolution de conflits. Votre configuration Git n’est pas modifiée.",
		"Fetch the upstream branch and replay your local commits onto its tip. This rewrites your local commit history, and conflicts may require resolution. It does not change your Git configuration.": "Récupère la branche amont et rejoue vos commits locaux à son sommet. Cela réécrit votre historique de commits local et peut demander une résolution de conflits. Votre configuration Git n’est pas modifiée.",
	},
}

func init() {
	catalogs := map[string]map[string]string{"zh-Hans": zhHans, "zh-Hant": zhHant, "ja": japanese, "es": spanish, "fr": french}
	for language, additions := range syncTranslations {
		for english, translated := range additions {
			catalogs[language][english] = translated
		}
	}
}
