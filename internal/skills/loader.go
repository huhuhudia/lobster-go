package skills

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// SkillInfo describes a discovered skill.
type SkillInfo struct {
	Name        string
	Path        string
	Source      string
	Description string
	Available   bool
	Missing     string
}

// Loader discovers and loads skills from workspace and optional builtin dirs.
type Loader struct {
	Workspace       string
	WorkspaceSkills string
	BuiltinSkills   string
}

// NewLoader creates a loader for a workspace and optional builtin dir.
func NewLoader(workspace string, builtinDir string) *Loader {
	ws := filepath.Clean(workspace)
	return &Loader{
		Workspace:       ws,
		WorkspaceSkills: filepath.Join(ws, "skills"),
		BuiltinSkills:   filepath.Clean(builtinDir),
	}
}

// ListSkills returns skills from workspace (priority) and builtin dirs.
func (l *Loader) ListSkills(filterUnavailable bool) []SkillInfo {
	skills := make([]SkillInfo, 0)
	seen := map[string]bool{}

	addSkills := func(base, source string) {
		if base == "" {
			return
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			return
		}
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			name := ent.Name()
			if seen[name] {
				continue
			}
			skillPath := filepath.Join(base, name, "SKILL.md")
			if _, err := os.Stat(skillPath); err != nil {
				continue
			}
			meta := l.getSkillMeta(name)
			available := checkRequirements(meta)
			info := SkillInfo{
				Name:        name,
				Path:        skillPath,
				Source:      source,
				Description: l.getSkillDescription(name),
				Available:   available,
			}
			if !available {
				info.Missing = getMissingRequirements(meta)
			}
			if filterUnavailable && !available {
				continue
			}
			skills = append(skills, info)
			seen[name] = true
		}
	}

	addSkills(l.WorkspaceSkills, "workspace")
	addSkills(l.BuiltinSkills, "builtin")

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills
}

// LoadSkill loads a skill by name, preferring workspace.
func (l *Loader) LoadSkill(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if l.WorkspaceSkills != "" {
		p := filepath.Join(l.WorkspaceSkills, name, "SKILL.md")
		if b, err := os.ReadFile(p); err == nil {
			return string(b), true
		}
	}
	if l.BuiltinSkills != "" {
		p := filepath.Join(l.BuiltinSkills, name, "SKILL.md")
		if b, err := os.ReadFile(p); err == nil {
			return string(b), true
		}
	}
	return "", false
}

// LoadSkillsForContext loads specified skills and formats them for prompt context.
func (l *Loader) LoadSkillsForContext(skillNames []string) string {
	parts := make([]string, 0, len(skillNames))
	for _, name := range skillNames {
		content, ok := l.LoadSkill(name)
		if !ok {
			continue
		}
		content = stripFrontmatter(content)
		parts = append(parts, "### Skill: "+name+"\n\n"+content)
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// BuildSkillsSummary returns an XML-like summary for progressive loading.
func (l *Loader) BuildSkillsSummary() string {
	all := l.ListSkills(false)
	if len(all) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<skills>\n")
	for _, s := range all {
		b.WriteString("  <skill available=\"")
		if s.Available {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		b.WriteString("\">\n")
		b.WriteString("    <name>")
		b.WriteString(escapeXML(s.Name))
		b.WriteString("</name>\n")
		b.WriteString("    <description>")
		b.WriteString(escapeXML(s.Description))
		b.WriteString("</description>\n")
		b.WriteString("    <location>")
		b.WriteString(escapeXML(s.Path))
		b.WriteString("</location>\n")
		if !s.Available && s.Missing != "" {
			b.WriteString("    <requires>")
			b.WriteString(escapeXML(s.Missing))
			b.WriteString("</requires>\n")
		}
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</skills>")
	return b.String()
}

// GetAlwaysSkills returns skills marked always=true and available.
func (l *Loader) GetAlwaysSkills() []string {
	result := []string{}
	for _, s := range l.ListSkills(true) {
		meta := l.getSkillMetadata(s.Name)
		if meta == nil {
			continue
		}
		always := strings.EqualFold(meta["always"], "true")
		skillMeta := parseSkillMetadata(meta["metadata"])
		if skillMeta != nil {
			if v, ok := skillMeta["always"].(bool); ok && v {
				always = true
			}
		}
		if always {
			result = append(result, s.Name)
		}
	}
	sort.Strings(result)
	return result
}

// GetSkillMetadata returns frontmatter fields for a skill.
func (l *Loader) GetSkillMetadata(name string) map[string]string {
	return l.getSkillMetadata(name)
}

func (l *Loader) getSkillMetadata(name string) map[string]string {
	content, ok := l.LoadSkill(name)
	if !ok {
		return nil
	}
	return parseFrontmatter(content)
}

func (l *Loader) getSkillDescription(name string) string {
	meta := l.getSkillMetadata(name)
	if meta != nil {
		if desc := strings.TrimSpace(meta["description"]); desc != "" {
			return desc
		}
	}
	return name
}

func (l *Loader) getSkillMeta(name string) map[string]interface{} {
	meta := l.getSkillMetadata(name)
	if meta == nil {
		return nil
	}
	return parseSkillMetadata(meta["metadata"])
}

func parseFrontmatter(content string) map[string]string {
	if !strings.HasPrefix(content, "---") {
		return nil
	}
	rest := strings.TrimPrefix(content, "---")
	if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return nil
	}
	block := rest[:end]
	out := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, "\"'")
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return strings.TrimSpace(content)
	}
	rest := strings.TrimPrefix(content, "---")
	if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest[end+len("\n---"):])
}

func parseSkillMetadata(raw string) map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}
	if v, ok := data["nanobot"].(map[string]interface{}); ok {
		return v
	}
	if v, ok := data["openclaw"].(map[string]interface{}); ok {
		return v
	}
	return data
}

func checkRequirements(meta map[string]interface{}) bool {
	if meta == nil {
		return true
	}
	reqs, ok := meta["requires"].(map[string]interface{})
	if !ok || reqs == nil {
		return true
	}
	for _, b := range stringSlice(reqs["bins"]) {
		if _, err := exec.LookPath(b); err != nil {
			return false
		}
	}
	for _, env := range stringSlice(reqs["env"]) {
		if strings.TrimSpace(os.Getenv(env)) == "" {
			return false
		}
	}
	return true
}

func getMissingRequirements(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	reqs, ok := meta["requires"].(map[string]interface{})
	if !ok || reqs == nil {
		return ""
	}
	missing := []string{}
	for _, b := range stringSlice(reqs["bins"]) {
		if _, err := exec.LookPath(b); err != nil {
			missing = append(missing, "CLI: "+b)
		}
	}
	for _, env := range stringSlice(reqs["env"]) {
		if strings.TrimSpace(os.Getenv(env)) == "" {
			missing = append(missing, "ENV: "+env)
		}
	}
	return strings.Join(missing, ", ")
}

func stringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []string:
		return val
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func escapeXML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(s)
}
