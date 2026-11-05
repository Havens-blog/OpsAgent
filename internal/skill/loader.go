// Package skill loads file-based operational runbooks so agents can pull in
// task-specific instructions on demand instead of carrying them in the system
// prompt.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Skill is a single runbook loaded from a SKILL.md file.
type Skill struct {
	Name        string
	Description string
	Body        string
}

// Loader discovers and serves skills from a directory tree. Each skill lives
// in its own directory as SKILL.md, with optional YAML frontmatter.
type Loader struct {
	dir string
	mu  sync.RWMutex
	m   map[string]Skill
}

// New builds a loader by scanning dir. A missing directory yields an empty
// catalog rather than an error, so skill loading is strictly optional.
func New(dir string) *Loader {
	l := &Loader{dir: dir, m: make(map[string]Skill)}
	l.scan()
	return l
}

// List returns the available skills sorted by name.
func (l *Loader) List() []Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Skill, 0, len(l.m))
	for _, s := range l.m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Load returns the full content of the named skill.
func (l *Loader) Load(name string) (Skill, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s, ok := l.m[name]
	if !ok {
		return Skill{}, fmt.Errorf("skill %q not found", name)
	}
	return s, nil
}

// Missing returns the names in want that are absent from the loader. It is the
// startup guard for built-in required skills.
func (l *Loader) Missing(want []string) []string {
	var missing []string
	for _, name := range want {
		if _, err := l.Load(name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

// Summary renders a compact catalog suitable for the system prompt.
func (l *Loader) Summary() string {
	skills := l.List()
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("可用运维技能：\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return b.String()
}

func (l *Loader) scan() {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(l.dir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s := parseSkill(string(data))
		if s.Name == "" {
			s.Name = entry.Name()
		}
		l.m[s.Name] = s
	}
}

// parseSkill extracts frontmatter name/description and the runbook body.
func parseSkill(content string) Skill {
	s := Skill{Body: strings.TrimSpace(content)}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				body := strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
				s.Body = body
				s.parseFrontmatter(strings.Join(lines[1:i], "\n"))
				break
			}
		}
	}
	return s
}

// parseFrontmatter reads the name and description from simple YAML lines.
func (s *Skill) parseFrontmatter(fm string) {
	for _, line := range strings.Split(fm, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "name":
			s.Name = strings.Trim(value, `"'`)
		case "description":
			s.Description = strings.Trim(value, `"'`)
		}
	}
}
