package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

func LoadMemory(dataDir, channelID string) string {
	var parts []string
	globalPath := filepath.Join(dataDir, "MEMORY.md")
	if b, err := os.ReadFile(globalPath); err == nil {
		content := strings.TrimSpace(string(b))
		if content != "" {
			parts = append(parts, "### Global Workspace Memory\n"+content)
		}
	}
	chPath := filepath.Join(dataDir, channelID, "MEMORY.md")
	if b, err := os.ReadFile(chPath); err == nil {
		content := strings.TrimSpace(string(b))
		if content != "" {
			parts = append(parts, "### Channel-Specific Memory\n"+content)
		}
	}
	if len(parts) == 0 {
		return "(no working memory yet)"
	}
	return strings.Join(parts, "\n\n")
}

func DiscoverSkills(dataDir, channelID, promptWorkspacePath string) []SkillSummary {
	skillMap := make(map[string]SkillSummary)
	channelDir := filepath.Join(dataDir, channelID)
	hostWorkspacePath := filepath.Clean(filepath.Join(channelDir, ".."))

	translatePath := func(hostPath string) string {
		if promptWorkspacePath == "" {
			return hostPath
		}
		hostPath = filepath.Clean(hostPath)
		if hostPath == hostWorkspacePath {
			return promptWorkspacePath
		}
		prefix := hostWorkspacePath + string(filepath.Separator)
		if strings.HasPrefix(hostPath, prefix) {
			rel := strings.TrimPrefix(hostPath, hostWorkspacePath)
			return filepath.ToSlash(promptWorkspacePath + rel)
		}
		return hostPath
	}

	bases := []struct {
		dir string
	}{
		{dir: filepath.Join(hostWorkspacePath, "skills")},
		{dir: filepath.Join(channelDir, "skills")},
	}
	for _, base := range bases {
		entries, err := os.ReadDir(base.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillDir := filepath.Join(base.dir, e.Name())
			p := filepath.Join(skillDir, "SKILL.md")
			body, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			name, desc := parseSkillFrontMatter(body)
			if name == "" {
				name = e.Name()
			}
			skillMap[name] = SkillSummary{
				Name:        name,
				Description: desc,
				Dir:         translatePath(skillDir),
			}
		}
	}
	out := make([]SkillSummary, 0, len(skillMap))
	for _, s := range skillMap {
		out = append(out, s)
	}
	return out
}

func parseSkillFrontMatter(data []byte) (name, description string) {
	s := string(data)
	if !strings.HasPrefix(strings.TrimSpace(s), "---") {
		return "", ""
	}
	trim := strings.TrimSpace(s)
	rest := strings.TrimPrefix(trim, "---")
	rest = strings.TrimLeft(rest, "\n")
	idx := strings.Index(rest, "---")
	if idx < 0 {
		return "", ""
	}
	fm := rest[:idx]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
		if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return name, description
}
