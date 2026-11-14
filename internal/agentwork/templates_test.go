package agentwork

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func phase3TemplateProfile() Profile {
	return Profile{
		Name:          "Web development",
		LaunchCommand: "agent {worktree}",
		ResumeCommand: "agent --resume {task}",
		RunCommand:    "npm run dev",
		SetupCommand:  "npm install",
		TestCommand:   "npm test",
		Environment:   map[string]string{"APP_ENV": "development"},
		SparsePaths:   []string{"web", "shared"},
		IgnoredFiles:  []string{"web/config.local.json"},
		Resources: Resources{
			DevelopmentPort: 3200,
			DatabaseName:    "web_development",
			CacheDirectory:  filepath.Join("cache", "web"),
		},
	}
}

func TestWorkspaceTemplateStorePersistsAcrossRestartAndClonesValues(t *testing.T) {
	store := testStore(t)
	templates, err := store.ListTemplates()
	if err != nil || len(templates) != 0 {
		t.Fatalf("legacy template list = %#v, %v", templates, err)
	}
	input := WorkspaceTemplate{Name: "  Web app  ", Profile: phase3TemplateProfile()}
	created, err := store.CreateTemplate(input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Web app" {
		t.Fatalf("template name = %q, want canonical trimmed name", created.Name)
	}
	input.Profile.Environment["APP_ENV"] = "changed input"
	input.Profile.SparsePaths[0] = "changed input"
	input.Profile.IgnoredFiles[0] = ".env"
	created.Profile.Environment["APP_ENV"] = "changed result"
	created.Profile.SparsePaths[0] = "changed result"
	created.Profile.IgnoredFiles[0] = "changed result"
	created.Profile.Resources.DevelopmentPort = 1

	second, err := store.CreateTemplate(WorkspaceTemplate{Name: "API", Profile: Profile{RunCommand: "go run ./cmd/api"}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "API" {
		t.Fatalf("second template = %#v", second)
	}
	restarted := &Store{dir: store.dir, now: time.Now}
	listed, err := restarted.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Name != "API" || listed[1].Name != "Web app" {
		t.Fatalf("restarted templates = %#v", listed)
	}
	web := listed[1].Profile
	if web.RunCommand != "npm run dev" || web.Environment["APP_ENV"] != "development" ||
		!slices.Equal(web.SparsePaths, []string{"web", "shared"}) ||
		!slices.Equal(web.IgnoredFiles, []string{"web/config.local.json"}) ||
		web.Resources.DevelopmentPort != 3200 || web.Resources.DatabaseName != "web_development" ||
		web.Resources.CacheDirectory != filepath.Join("cache", "web") {
		t.Fatalf("restarted web template profile = %#v", web)
	}
	if filepath.Dir(store.templatesPath()) != store.dir {
		t.Fatalf("template store escaped the common user-data root: %q", store.templatesPath())
	}

	listed[1].Profile.Environment["APP_ENV"] = "changed list"
	listed[1].Profile.SparsePaths[0] = "changed list"
	listed[1].Profile.IgnoredFiles[0] = "changed list"
	again, err := restarted.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	profile := again[1].Profile
	if profile.Environment["APP_ENV"] != "development" || profile.SparsePaths[0] != "web" ||
		profile.IgnoredFiles[0] != "web/config.local.json" {
		t.Fatalf("ListTemplates shared mutable profile data: %#v", profile)
	}
}

func TestWorkspaceTemplateUpdateAndDeleteDoNotMutateTasks(t *testing.T) {
	store := testStore(t)
	template, err := store.CreateTemplate(WorkspaceTemplate{Name: "Web app", Profile: phase3TemplateProfile()})
	if err != nil {
		t.Fatal(err)
	}
	taskInput := validTask("local:/repo")
	taskInput.Profile = template.Profile
	task, err := store.Create(taskInput)
	if err != nil {
		t.Fatal(err)
	}

	newProfile := template.Profile.Clone()
	newProfile.RunCommand = "npm run start"
	newProfile.Environment["APP_ENV"] = "production"
	newProfile.SparsePaths[0] = "frontend"
	newProfile.IgnoredFiles = []string{".env"}
	newProfile.Resources.DevelopmentPort = 4400
	updated, err := store.UpdateTemplate(" Web app ", newProfile)
	if err != nil {
		t.Fatal(err)
	}
	newProfile.Environment["APP_ENV"] = "changed input after update"
	updated.Profile.Environment["APP_ENV"] = "changed returned update"
	updated.Profile.SparsePaths[0] = "changed returned update"

	readTask, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if readTask.Profile.RunCommand != "npm run dev" || readTask.Profile.Environment["APP_ENV"] != "development" ||
		readTask.Profile.SparsePaths[0] != "web" || readTask.Profile.Resources.DevelopmentPort != 3200 ||
		slices.Contains(readTask.Profile.IgnoredFiles, ".env") {
		t.Fatalf("editing a template mutated an existing task: %#v", readTask.Profile)
	}
	listed, err := store.ListTemplates()
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListTemplates = %#v, %v", listed, err)
	}
	if listed[0].Name != "Web app" || listed[0].Profile.Environment["APP_ENV"] != "production" ||
		listed[0].Profile.SparsePaths[0] != "frontend" ||
		!slices.Equal(listed[0].Profile.IgnoredFiles, []string{".env"}) {
		t.Fatalf("updated template = %#v", listed[0])
	}

	if err := store.DeleteTemplate("Web app"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTemplate("Web app"); err == nil {
		t.Fatal("DeleteTemplate accepted a missing template")
	}
	if _, err := store.UpdateTemplate("Web app", Profile{}); err == nil {
		t.Fatal("UpdateTemplate accepted a missing template")
	}
	if _, err := store.Get(task.ID); err != nil {
		t.Fatalf("deleting a template affected its task: %v", err)
	}
}

func TestWorkspaceTemplateStoreRejectsInvalidNamesProfilesAndDuplicates(t *testing.T) {
	store := testStore(t)
	for _, name := range []string{"", "  ", "bad\nname", strings.Repeat("n", maxTemplateNameBytes+1)} {
		if _, err := store.CreateTemplate(WorkspaceTemplate{Name: name}); err == nil {
			t.Fatalf("CreateTemplate accepted invalid name %q", name)
		}
	}
	if _, err := store.CreateTemplate(WorkspaceTemplate{
		Name: "Invalid", Profile: Profile{Resources: Resources{DevelopmentPort: -1}},
	}); err == nil {
		t.Fatal("CreateTemplate accepted an invalid resource")
	}
	if _, err := store.CreateTemplate(WorkspaceTemplate{
		Name: "Invalid", Profile: Profile{IgnoredFiles: []string{filepath.Join("..", ".env")}},
	}); err == nil {
		t.Fatal("CreateTemplate accepted an escaping ignored-file path")
	}
	if _, err := store.CreateTemplate(WorkspaceTemplate{Name: "Stable", Profile: Profile{RunCommand: "one"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTemplate(WorkspaceTemplate{Name: " Stable ", Profile: Profile{RunCommand: "two"}}); err == nil {
		t.Fatal("CreateTemplate accepted a duplicate canonical name")
	}
	if _, err := store.UpdateTemplate("Stable", Profile{SparsePaths: []string{string(filepath.Separator) + "absolute"}}); err == nil {
		t.Fatal("UpdateTemplate accepted an invalid profile")
	}
	listed, err := store.ListTemplates()
	if err != nil || len(listed) != 1 || listed[0].Profile.RunCommand != "one" {
		t.Fatalf("invalid operations changed templates: %#v, %v", listed, err)
	}
}

func TestWorkspaceTemplateStoreDoesNotClobberDamagedData(t *testing.T) {
	store := testStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	damaged := []byte(`{"version":1,"templates":[`)
	if err := os.WriteFile(store.templatesPath(), damaged, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTemplate(WorkspaceTemplate{Name: "New"}); err == nil || !strings.Contains(err.Error(), "read workspace templates") {
		t.Fatalf("CreateTemplate error = %v, want malformed-store error", err)
	}
	if _, err := store.UpdateTemplate("New", Profile{}); err == nil || !strings.Contains(err.Error(), "read workspace templates") {
		t.Fatalf("UpdateTemplate error = %v, want malformed-store error", err)
	}
	if err := store.DeleteTemplate("New"); err == nil || !strings.Contains(err.Error(), "read workspace templates") {
		t.Fatalf("DeleteTemplate error = %v, want malformed-store error", err)
	}
	read, err := os.ReadFile(store.templatesPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(read) != string(damaged) {
		t.Fatalf("damaged template source was overwritten: %q", read)
	}
}

func TestWorkspaceTemplateStoreLimitsAreAtomic(t *testing.T) {
	store := testStore(t)
	original, err := store.CreateTemplate(WorkspaceTemplate{Name: "Original", Profile: Profile{RunCommand: "original"}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.templatesPath())
	if err != nil {
		t.Fatal(err)
	}
	tooLarge := original.Profile.Clone()
	tooLarge.RunCommand = strings.Repeat("x", maxTemplateStoreSize)
	if _, err := store.UpdateTemplate("Original", tooLarge); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized UpdateTemplate error = %v", err)
	}
	after, err := os.ReadFile(store.templatesPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("oversized template update changed persistent data")
	}

	full := diskTemplateStore{Version: templateStoreVersion, Templates: make([]WorkspaceTemplate, maxTemplateCount)}
	for index := range full.Templates {
		full.Templates[index].Name = fmt.Sprintf("Template %03d", index)
	}
	data, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.templatesPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTemplate(WorkspaceTemplate{Name: "One too many"}); err == nil || !strings.Contains(err.Error(), "limited") {
		t.Fatalf("full template store CreateTemplate error = %v", err)
	}
}

func TestWorkspaceTemplateRecordsOnlyExplicitIgnoredFiles(t *testing.T) {
	store := testStore(t)
	secretPath := filepath.Join(store.dir, ".env")
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("TOKEN=must-not-be-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	template, err := store.CreateTemplate(WorkspaceTemplate{Name: "No implicit env", Profile: Profile{
		IgnoredFiles: []string{"config/editor.json"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(template.Profile.IgnoredFiles, []string{"config/editor.json"}) ||
		slices.Contains(template.Profile.IgnoredFiles, ".env") {
		t.Fatalf("template gained implicit ignored files: %#v", template.Profile.IgnoredFiles)
	}
	if template.Profile.Environment["TOKEN"] != "" {
		t.Fatalf("template harvested environment data: %#v", template.Profile.Environment)
	}
}
