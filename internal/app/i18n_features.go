package app

// Feature translations are organized in separate catalogs so additions for
// the git-hosting, history and command workflows remain maintainable. Each
// file is deliberately a plain English-keyed map: the source coverage tests
// verify that every statically referenced UI string reaches every language.
func init() {
	featureCatalogs := map[string]map[string]string{
		"zh-Hans": featureTranslationsZhHans,
		"zh-Hant": featureTranslationsZhHant,
		"ja":      featureTranslationsJa,
		"es":      featureTranslationsEs,
		"fr":      featureTranslationsFr,
	}
	for language, entries := range featureCatalogs {
		for english, translated := range entries {
			translations[language][english] = translated
		}
	}
}
