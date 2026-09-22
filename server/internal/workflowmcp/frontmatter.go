package workflowmcp

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var frontMatterRe = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n?(.*)$`)

// ParseFrontMatter 解析 Markdown YAML front matter，返回元数据与正文。
func ParseFrontMatter(raw string) (map[string]any, string, error) {
	m := frontMatterRe.FindStringSubmatch(raw)
	if m == nil {
		return map[string]any{}, raw, nil
	}
	meta := map[string]any{}
	if err := yaml.Unmarshal([]byte(m[1]), &meta); err != nil {
		return nil, "", fmt.Errorf("解析 front matter: %w", err)
	}
	return meta, strings.TrimPrefix(m[2], "\n"), nil
}

// RenderMarkdown 将 front matter + body 渲染为 Markdown。
func RenderMarkdown(meta map[string]any, body string) (string, error) {
	b, err := yaml.Marshal(meta)
	if err != nil {
		return "", err
	}
	return "---\n" + string(b) + "---\n" + strings.TrimLeft(body, "\n"), nil
}

// CursorSkillName 将逻辑 id 转为 Cursor 安装目录名。
func CursorSkillName(id string) string {
	s := strings.ToLower(id)
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	re := regexp.MustCompile(`[^a-z0-9-]+`)
	s = re.ReplaceAllString(s, "")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// ParseSkillMD 从 SKILL.md 内容解析技能包（不含附属文件）。
func ParseSkillMD(content string) (*SkillPackage, error) {
	meta, body, err := ParseFrontMatter(content)
	if err != nil {
		return nil, err
	}
	sp := &SkillPackage{
		Body:                   body,
		Metadata:               map[string]any{},
		DisableModelInvocation: true,
		Version:                "1.0.0",
	}
	if v, ok := meta["id"].(string); ok {
		sp.ID = v
	}
	if v, ok := meta["name"].(string); ok {
		sp.CursorName = v
	}
	if v, ok := meta["title"].(string); ok {
		sp.Name = v
	}
	if sp.Name == "" {
		sp.Name = sp.CursorName
	}
	if v, ok := meta["version"].(string); ok && v != "" {
		sp.Version = v
	}
	if v, ok := meta["description"].(string); ok {
		sp.Description = strings.TrimSpace(v)
	}
	if v, ok := meta["disable-model-invocation"].(bool); ok {
		sp.DisableModelInvocation = v
	}
	for _, key := range []string{"capabilities", "inputs", "knowledge", "tools", "output"} {
		if v, ok := meta[key]; ok {
			sp.Metadata[key] = v
		}
	}
	if sp.ID == "" {
		return nil, fmt.Errorf("SKILL.md 缺少 id")
	}
	if sp.CursorName == "" {
		sp.CursorName = CursorSkillName(sp.ID)
	}
	return sp, nil
}

// RenderSkillMD 将技能包渲染为 SKILL.md。
func RenderSkillMD(sp *SkillPackage) (string, error) {
	meta := map[string]any{
		"name":                     sp.CursorName,
		"id":                       sp.ID,
		"version":                  sp.Version,
		"description":              strings.TrimSpace(sp.Description),
		"disable-model-invocation": sp.DisableModelInvocation,
	}
	if sp.Name != "" && sp.Name != sp.CursorName {
		meta["title"] = sp.Name
	}
	for _, key := range []string{"capabilities", "inputs", "knowledge", "tools", "output"} {
		if sp.Metadata != nil {
			if v, ok := sp.Metadata[key]; ok {
				meta[key] = v
			}
		}
	}
	return RenderMarkdown(meta, sp.Body)
}

// ParseKnowledgeMD 解析知识 Markdown。
func ParseKnowledgeMD(path, raw string) (*KnowledgeDoc, error) {
	meta, body, err := ParseFrontMatter(raw)
	if err != nil {
		return nil, err
	}
	doc := &KnowledgeDoc{
		Path:      path,
		Content:   body,
		Source:    "local",
		Version:   "1.0.0",
		Metadata:  map[string]any{},
		Aliases:   nil,
	}
	// 默认 id = 去 .md 的路径
	id := strings.TrimSuffix(path, ".md")
	id = strings.ReplaceAll(id, "\\", "/")
	doc.ID = id

	if v, ok := meta["id"].(string); ok && v != "" {
		if v != doc.ID {
			doc.Aliases = append(doc.Aliases, v)
		}
		// 保留路径 id 为主键，alias 存 front matter id
	}
	if v, ok := meta["title"].(string); ok {
		doc.Title = v
	}
	if doc.Title == "" {
		// 取正文第一行标题
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				doc.Title = strings.TrimPrefix(line, "# ")
				break
			}
		}
		if doc.Title == "" {
			doc.Title = doc.ID
		}
	}
	if v, ok := meta["source"].(string); ok {
		doc.Source = v
	}
	if v, ok := meta["namespace"].(string); ok {
		doc.Namespace = v
	}
	if doc.Namespace == "" {
		parts := strings.Split(id, "/")
		if len(parts) > 0 {
			doc.Namespace = parts[0]
		}
	}
	if v, ok := meta["version"].(string); ok && v != "" {
		doc.Version = v
	}
	for k, v := range meta {
		switch k {
		case "id", "title", "source", "namespace", "version":
			continue
		default:
			doc.Metadata[k] = v
		}
	}
	return doc, nil
}

// RenderKnowledgeMD 渲染知识文档为 Markdown。
func RenderKnowledgeMD(doc *KnowledgeDoc) (string, error) {
	meta := map[string]any{
		"title":     doc.Title,
		"source":    doc.Source,
		"namespace": doc.Namespace,
		"version":   doc.Version,
	}
	if len(doc.Aliases) > 0 {
		meta["id"] = doc.Aliases[0]
	} else {
		meta["id"] = doc.ID
	}
	for k, v := range doc.Metadata {
		meta[k] = v
	}
	return RenderMarkdown(meta, doc.Content)
}

// ChunkKnowledge 按 ## 标题切分，超长段落滑窗（1200/150）。
func ChunkKnowledge(doc *KnowledgeDoc) []KnowledgeChunk {
	const maxLen = 1200
	const overlap = 150

	body := doc.Content
	sections := splitByHeading(body)
	var chunks []KnowledgeChunk
	idx := 0
	for _, sec := range sections {
		text := sec.content
		if len([]rune(text)) <= maxLen {
			chunks = append(chunks, KnowledgeChunk{
				DocID: doc.ID, ChunkIndex: idx, Heading: sec.heading,
				Content: text, Tokens: TokenizeJoined(sec.heading + " " + text),
			})
			idx++
			continue
		}
		runes := []rune(text)
		start := 0
		for start < len(runes) {
			end := start + maxLen
			if end > len(runes) {
				end = len(runes)
			}
			part := string(runes[start:end])
			chunks = append(chunks, KnowledgeChunk{
				DocID: doc.ID, ChunkIndex: idx, Heading: sec.heading,
				Content: part, Tokens: TokenizeJoined(sec.heading + " " + part),
			})
			idx++
			if end >= len(runes) {
				break
			}
			start = end - overlap
			if start < 0 {
				start = 0
			}
		}
	}
	return chunks
}

type section struct {
	heading string
	content string
}

func splitByHeading(body string) []section {
	lines := strings.Split(body, "\n")
	var secs []section
	cur := section{heading: "", content: ""}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if strings.TrimSpace(cur.content) != "" || cur.heading != "" {
				secs = append(secs, cur)
			}
			cur = section{heading: strings.TrimPrefix(line, "## "), content: ""}
			continue
		}
		if cur.content == "" {
			cur.content = line
		} else {
			cur.content += "\n" + line
		}
	}
	if strings.TrimSpace(cur.content) != "" || cur.heading != "" {
		secs = append(secs, cur)
	}
	if len(secs) == 0 {
		return []section{{heading: "", content: body}}
	}
	return secs
}
