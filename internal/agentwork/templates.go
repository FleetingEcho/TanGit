package agentwork

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	templateStoreVersion = 1
	maxTemplateCount     = 100
	maxTemplateNameBytes = 128
	maxTemplateStoreSize = 1 << 20
)

// WorkspaceTemplate is a named, reusable trusted profile. Applying or editing a
// template never changes the independent Profile values already stored on tasks.
type WorkspaceTemplate struct {
	Name    string  `json:"name"`
	Profile Profile `json:"profile"`
}

// Clone returns a template whose Profile shares no mutable state with t.
func (t WorkspaceTemplate) Clone() WorkspaceTemplate {
	clone := t
	clone.Profile = t.Profile.Clone()
	return clone
}

type diskTemplateStore struct {
	Version   int                 `json:"version"`
	Templates []WorkspaceTemplate `json:"templates"`
}

func (s *Store) templatesPath() string {
	return filepath.Join(s.dir, "agent-workspace-templates.json")
}

func normalizeTemplateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("workspace template name is required")
	}
	if len(name) > maxTemplateNameBytes {
		return "", fmt.Errorf("workspace template name exceeds %d bytes", maxTemplateNameBytes)
	}
	if containsControl(name) {
		return "", errors.New("workspace template name contains a control character")
	}
	return name, nil
}

func (s *Store) readTemplates() (diskTemplateStore, error) {
	data, err := os.ReadFile(s.templatesPath())
	if errors.Is(err, os.ErrNotExist) {
		return diskTemplateStore{Version: templateStoreVersion}, nil
	}
	if err != nil {
		return diskTemplateStore{}, fmt.Errorf("read workspace templates: %w", err)
	}
	if len(data) > maxTemplateStoreSize {
		return diskTemplateStore{}, fmt.Errorf("workspace template store exceeds %d bytes", maxTemplateStoreSize)
	}
	var disk diskTemplateStore
	if err := json.Unmarshal(data, &disk); err != nil {
		return diskTemplateStore{}, fmt.Errorf("read workspace templates: %w", err)
	}
	if disk.Version != templateStoreVersion {
		return diskTemplateStore{}, fmt.Errorf("unsupported workspace template store version %d", disk.Version)
	}
	if len(disk.Templates) > maxTemplateCount {
		return diskTemplateStore{}, fmt.Errorf("workspace template store exceeds %d entries", maxTemplateCount)
	}
	seen := make(map[string]bool, len(disk.Templates))
	for _, template := range disk.Templates {
		name, err := normalizeTemplateName(template.Name)
		if err != nil {
			return diskTemplateStore{}, err
		}
		if name != template.Name {
			return diskTemplateStore{}, fmt.Errorf("workspace template name %q is not canonical", template.Name)
		}
		if seen[name] {
			return diskTemplateStore{}, fmt.Errorf("duplicate workspace template name %q", name)
		}
		seen[name] = true
		if err := template.Profile.validate(); err != nil {
			return diskTemplateStore{}, fmt.Errorf("workspace template %s: %w", name, err)
		}
	}
	return disk, nil
}

func (s *Store) writeTemplates(disk diskTemplateStore) error {
	disk.Version = templateStoreVersion
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace templates: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxTemplateStoreSize {
		return fmt.Errorf("workspace template store exceeds %d bytes", maxTemplateStoreSize)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create workspace template store: %w", err)
	}
	if err := writeFileAtomic(s.templatesPath(), data, 0o600); err != nil {
		return fmt.Errorf("save workspace templates: %w", err)
	}
	return nil
}

// ListTemplates returns independent templates sorted by their stable names.
// An absent legacy template file is an empty collection, not an error.
func (s *Store) ListTemplates() ([]WorkspaceTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	disk, err := s.readTemplates()
	if err != nil {
		return nil, err
	}
	templates := make([]WorkspaceTemplate, 0, len(disk.Templates))
	for _, template := range disk.Templates {
		templates = append(templates, template.Clone())
	}
	slices.SortStableFunc(templates, func(left, right WorkspaceTemplate) int {
		return strings.Compare(left.Name, right.Name)
	})
	return templates, nil
}

// CreateTemplate persists a uniquely named profile in the task user-data root.
func (s *Store) CreateTemplate(template WorkspaceTemplate) (WorkspaceTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, err := normalizeTemplateName(template.Name)
	if err != nil {
		return WorkspaceTemplate{}, err
	}
	template.Name = name
	if err := template.Profile.validate(); err != nil {
		return WorkspaceTemplate{}, err
	}
	template = template.Clone()
	disk, err := s.readTemplates()
	if err != nil {
		return WorkspaceTemplate{}, err
	}
	if len(disk.Templates) >= maxTemplateCount {
		return WorkspaceTemplate{}, fmt.Errorf("workspace template store is limited to %d entries", maxTemplateCount)
	}
	for _, existing := range disk.Templates {
		if existing.Name == name {
			return WorkspaceTemplate{}, fmt.Errorf("workspace template %q already exists", name)
		}
	}
	disk.Templates = append(disk.Templates, template)
	if err := s.writeTemplates(disk); err != nil {
		return WorkspaceTemplate{}, err
	}
	return template.Clone(), nil
}

// UpdateTemplate replaces only the named template's profile. The stable name
// cannot be changed by an update; create a new template to use another name.
func (s *Store) UpdateTemplate(name string, profile Profile) (WorkspaceTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, err := normalizeTemplateName(name)
	if err != nil {
		return WorkspaceTemplate{}, err
	}
	if err := profile.validate(); err != nil {
		return WorkspaceTemplate{}, err
	}
	profile = profile.Clone()
	disk, err := s.readTemplates()
	if err != nil {
		return WorkspaceTemplate{}, err
	}
	for index, template := range disk.Templates {
		if template.Name == name {
			template.Profile = profile
			disk.Templates[index] = template
			if err := s.writeTemplates(disk); err != nil {
				return WorkspaceTemplate{}, err
			}
			return template.Clone(), nil
		}
	}
	return WorkspaceTemplate{}, fmt.Errorf("workspace template %q was not found", name)
}

// DeleteTemplate removes exactly the named template without touching tasks.
func (s *Store) DeleteTemplate(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, err := normalizeTemplateName(name)
	if err != nil {
		return err
	}
	disk, err := s.readTemplates()
	if err != nil {
		return err
	}
	for index, template := range disk.Templates {
		if template.Name == name {
			disk.Templates = append(disk.Templates[:index], disk.Templates[index+1:]...)
			return s.writeTemplates(disk)
		}
	}
	return fmt.Errorf("workspace template %q was not found", name)
}
