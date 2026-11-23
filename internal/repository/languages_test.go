package repository

import "testing"

func TestSummarizeLanguagesCountsSourceBySize(t *testing.T) {
	output := "100644 blob aaa     300\tmain.go\x00" +
		"100644 blob bbb     100\tweb/app.ts\x00" +
		"100644 blob ccc     900\tnode_modules/x/index.js\x00" +
		"100644 blob ddd     50\tlogo.png\x00" +
		"100644 blob eee     200\tinternal/util.go\x00" +
		"160000 commit fff       -\tsub\x00"
	stats := summarizeLanguages(output)
	if len(stats.Languages) != 2 || stats.Languages[0].Name != "Go" || stats.Languages[0].Bytes != 500 || stats.Languages[0].Files != 2 {
		t.Fatalf("languages = %+v", stats.Languages)
	}
	if stats.Languages[1].Name != "TypeScript" || stats.TotalBytes != 600 || stats.TotalFiles != 3 {
		t.Fatalf("stats = %+v", stats)
	}
}
