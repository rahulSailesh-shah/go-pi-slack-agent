package prompt

import (
	"fmt"
	"strings"
	"time"

	slackctx "slack-agent/internal/slack"
)

const maxIDTableUsers = 500
const maxIDTableChannels = 500

type SkillSummary struct {
	Name        string
	Description string
	Dir         string
}

type Options struct {
	WorkspacePath string
	ChannelID     string
	Memory        string
	IsDocker      bool
	HostCwd       string
	Timezone      string
	Channels      []slackctx.ChannelSummary
	Users         []slackctx.UserSummary
	Skills        []SkillSummary
}

func BuildSystemPrompt(o Options) string {
	wp := o.WorkspacePath
	if wp == "" {
		wp = "data"
	}
	ch := o.ChannelID
	tz := o.Timezone
	if tz == "" {
		tz = time.Now().Location().String()
	}
	hostCwd := o.HostCwd
	if hostCwd == "" {
		hostCwd = "."
	}

	envDescription := fmt.Sprintf(`You are running directly on the host machine.
- Bash working directory: %s
- Be careful with system modifications`, hostCwd)
	if o.IsDocker {
		envDescription = `You are running inside a Docker container (Alpine Linux).
- Bash working directory: / (use cd or absolute paths)
- Install tools with: apk add <package>
- Your changes persist across sessions`
	}

	channelMappings := formatChannelTable(o.Channels)
	userMappings := formatUserTable(o.Users)

	skillsBlock := "(no skills installed yet)"
	if len(o.Skills) > 0 {
		var sb strings.Builder
		for _, s := range o.Skills {
			desc := s.Description
			if desc == "" {
				desc = "(no description)"
			}
			sb.WriteString(fmt.Sprintf("- *%s*: %s (`%s/`)\n", s.Name, desc, s.Dir))
		}
		skillsBlock = sb.String()
	}

	mem := strings.TrimSpace(o.Memory)
	if mem == "" {
		mem = "(none)"
	}

	channelPath := fmt.Sprintf("%s/%s", wp, ch)

	return fmt.Sprintf(`You are a Slack bot assistant. Be concise. No emojis.

## Context
- For current date/time, use: date
- You have access to previous conversation context including tool results from prior turns.
- For older history beyond your context, search log.jsonl (contains user messages and your final responses, but not tool results).

## Slack Formatting (mrkdwn, NOT Markdown)
Bold: *text*, Italic: _text_, Code: `+"`code`"+`, Block: `+"```code```"+`, Links: <url|text>
Do NOT use **double asterisks** or [markdown](links).

## Slack IDs
Channels:
%s

Users:
%s

When mentioning users in Slack, use <@USER_ID> with the user's Slack member ID (example: <@U123ABC456>), not bare names.

## Environment
%s

Harness timezone for interpreting relative times: %s

## Workspace Layout
%s/
├── MEMORY.md                    # Global memory (all channels)
├── skills/                      # Global CLI tools you create
└── %s/                # This channel
    ├── MEMORY.md                # Channel-specific memory
    ├── log.jsonl                # Message history (no tool results)
    ├── attachments/             # User-shared files
    ├── scratch/                 # Your working directory
    └── skills/                  # Channel-specific tools

## Skills (Custom CLI Tools)
You can create reusable CLI tools for recurring tasks (email, APIs, data processing, etc.).

### Creating Skills
Store in `+"`%s/skills/<name>/`"+` (global) or `+"`%s/skills/<name>/`"+` (channel-specific).
Each skill directory needs a SKILL.md with YAML frontmatter:

`+"```markdown"+`
---
name: skill-name
description: Short description of what this skill does
---

# Skill Name

Usage instructions, examples, etc.
Scripts are in: {baseDir}/
`+"```"+`

`+"`name` and `description` are required. Use `{baseDir}` as placeholder for the skill's directory path."+`

### Available Skills
%s

## Memory
Write to MEMORY.md files to persist context across conversations.
- Global (%s/MEMORY.md): skills, preferences, project info
- Channel (%s/MEMORY.md): channel-specific decisions, ongoing work
Update when you learn something important or when asked to remember something.

### Current Memory
%s

## System Configuration Log
Maintain %s/SYSTEM.md to log all environment modifications:
- Installed packages (apk add, npm install, pip install)
- Environment variables set
- Config files modified (~/.gitconfig, cron jobs, etc.)
- Skill dependencies installed

Update this file whenever you modify the environment.

## Log Queries (for older history)
Messages are stored as JSON lines in log.jsonl under this channel directory.

`+"```bash"+`
# Recent messages
tail -30 log.jsonl | jq .

# Search for a topic
grep -i "topic" log.jsonl | jq .

# Messages containing a user id (replace ID)
grep '"UserID":"U123"' log.jsonl | tail -20 | jq .
`+"```"+`

%s

## Tools
- bash: Run shell commands (primary tool). Install packages as needed.
- read: Read files
- write: Create/overwrite files
- edit: Surgical file edits
- slack_post: Send a message to another Slack destination in this workspace. Parameters: destination (hash-prefix channel name, at-prefix member handle, or raw C/G/D/U id), text (mrkdwn). Use only for explicit cross-channel or DM delivery; your normal reply still goes to the current conversation.

Each tool requires a "label" parameter (shown to user).
`,
		channelMappings,
		userMappings,
		envDescription,
		tz,
		wp,
		ch,
		wp,
		channelPath,
		skillsBlock,
		wp,
		channelPath,
		mem,
		wp,
		jqHint(o.IsDocker),
	)
}

func formatChannelTable(channels []slackctx.ChannelSummary) string {
	if len(channels) == 0 {
		return "(no channels loaded)"
	}
	n := len(channels)
	trunc := ""
	if n > maxIDTableChannels {
		channels = channels[:maxIDTableChannels]
		trunc = fmt.Sprintf("\n… truncated, showing %d of %d", maxIDTableChannels, n)
	}
	var b strings.Builder
	for _, c := range channels {
		label := c.Name
		if label == "" {
			label = "(unnamed)"
		}
		b.WriteString(fmt.Sprintf("%s\t#%s\n", c.ID, label))
	}
	b.WriteString(trunc)
	return strings.TrimSuffix(b.String(), "\n")
}

func formatUserTable(users []slackctx.UserSummary) string {
	if len(users) == 0 {
		return "(no users loaded)"
	}
	n := len(users)
	trunc := ""
	if n > maxIDTableUsers {
		users = users[:maxIDTableUsers]
		trunc = fmt.Sprintf("\n… truncated, showing %d of %d", maxIDTableUsers, n)
	}
	var b strings.Builder
	for _, u := range users {
		disp := u.DisplayName
		if disp == "" {
			disp = "-"
		}
		h := u.Name
		if h == "" {
			h = "-"
		}
		b.WriteString(fmt.Sprintf("%s\t@%s\t%s\n", u.ID, h, disp))
	}
	b.WriteString(trunc)
	return strings.TrimSuffix(b.String(), "\n")
}

func jqHint(isDocker bool) string {
	if isDocker {
		return "Install jq if needed: apk add jq"
	}
	return "Install jq if needed for your host OS (e.g. apt install jq, brew install jq)."
}
