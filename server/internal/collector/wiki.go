package collector

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func (s *Service) wikiChildren(ctx context.Context, owner, identity, space, node string) ([]map[string]any, error) {
	if _, err := segment(space); err != nil {
		return nil, err
	}
	if _, err := segment(node); err != nil {
		return nil, err
	}
	out := []map[string]any{}
	pageToken := ""
	for page := 0; page < 100; page++ {
		q := url.Values{"parent_node_token": {node}, "page_size": {"50"}}
		if pageToken != "" {
			q.Set("page_token", pageToken)
		}
		data, err := s.Feishu(ctx, owner, identity, "GET", "/open-apis/wiki/v2/spaces/"+space+"/nodes", q, nil)
		if err != nil {
			return nil, err
		}
		for _, v := range array(data["items"]) {
			out = append(out, object(v))
		}
		if !boolean(data, "has_more", false) {
			return out, nil
		}
		next := text(data, "page_token")
		if next == "" || next == pageToken {
			return nil, errors.New("Wiki 分页游标异常")
		}
		pageToken = next
	}
	return nil, errors.New("Wiki 子节点过多，请缩小目录范围")
}
func (s *Service) wikiTree(ctx context.Context, owner, identity, raw string, recursive bool, maxDepth, maxNodes int) ([]map[string]any, error) {
	kind, token, node, err := s.resolveRef(ctx, owner, identity, raw)
	if err != nil {
		return nil, err
	}
	if node == nil {
		node = map[string]any{"obj_type": kind, "obj_token": token, "title": token}
	}
	if maxDepth < 0 || maxDepth > 10 || maxNodes < 1 || maxNodes > 1000 {
		return nil, errors.New("目录深度限 0–10，节点数量限 1–1000")
	}
	type entry struct {
		node  map[string]any
		depth int
	}
	queue := []entry{{node, 0}}
	out := []map[string]any{}
	seen := map[string]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		id := valueOr(text(current.node, "node_token"), text(current.node, "obj_token"))
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(out) >= maxNodes {
			return nil, errors.New("Wiki 已超过节点上限，请缩小范围或提高上限")
		}
		out = append(out, current.node)
		if recursive && current.depth < maxDepth && boolean(current.node, "has_child", false) {
			children, err := s.wikiChildren(ctx, owner, identity, text(current.node, "space_id"), text(current.node, "node_token"))
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if text(child, "space_id") == "" {
					child["space_id"] = text(current.node, "space_id")
				}
				queue = append(queue, entry{child, current.depth + 1})
			}
		}
	}
	return out, nil
}

func (s *Service) readDocument(ctx context.Context, owner, identity, kind, token string) (string, error) {
	if _, err := segment(token); err != nil {
		return "", err
	}
	path := "/open-apis/docx/v1/documents/" + token + "/raw_content"
	if kind == "doc" {
		path = "/open-apis/doc/v2/" + token + "/raw_content"
	}
	if kind != "docx" && kind != "doc" {
		return "", errors.New("此资源不是云文档，请使用对应表格工具")
	}
	data, err := s.Feishu(ctx, owner, identity, "GET", path, nil, nil)
	if err != nil {
		return "", err
	}
	return text(data, "content"), nil
}

func (s *Service) fetchWiki(ctx context.Context, t Target) ([]Item, error) {
	identity := text(t.Config, "identity")
	nodes, err := s.wikiTree(ctx, t.OwnerID, identity, t.URL, boolean(t.Config, "recursive", false), integer(t.Config, "max_depth", 3), integer(t.Config, "max_nodes", 200))
	if err != nil {
		return nil, err
	}
	var include, exclude *regexp.Regexp
	if str := text(t.Config, "include"); str != "" {
		include, err = regexp.Compile(str)
		if err != nil {
			return nil, errors.New("包含规则的正则表达式无效")
		}
	}
	if str := text(t.Config, "exclude"); str != "" {
		exclude, err = regexp.Compile(str)
		if err != nil {
			return nil, errors.New("排除规则的正则表达式无效")
		}
	}
	types := stringsOf(t.Config["obj_types"])
	if len(types) == 0 {
		types = []string{"docx", "doc"}
	}
	out := []Item{}
	for _, node := range nodes {
		kind, token, title := text(node, "obj_type"), text(node, "obj_token"), text(node, "title")
		if !contains(types, kind) || (include != nil && !include.MatchString(title)) || (exclude != nil && exclude.MatchString(title)) {
			continue
		}
		if kind != "docx" && kind != "doc" {
			continue
		}
		content, err := s.readDocument(ctx, t.OwnerID, identity, kind, token)
		if err != nil {
			return nil, err
		}
		link := "https://feishu.cn/" + kind + "/" + token
		if n := text(node, "node_token"); n != "" {
			link = "https://feishu.cn/wiki/" + n
		}
		fields := map[string]any{"title": title, "content": content, "obj_type": kind, "node_token": text(node, "node_token"), "obj_token": token}
		if kind == "docx" && boolean(t.Config, "include_whiteboards", false) {
			boards, err := s.readWhiteboards(ctx, t.OwnerID, identity, token)
			if err != nil {
				return nil, err
			}
			fields["whiteboards"] = boards
			if len(boards) > 0 {
				encoded, _ := json.Marshal(boards)
				content += "\n" + string(encoded)
			}
		}
		out = append(out, Item{ID: t.ID + ":" + token, TargetID: t.ID, Source: "wiki", ExternalID: token, Title: title, URL: link, Content: content, Fields: fields, UpdatedAt: time.Now().UTC()})
	}
	return out, nil
}

func (s *Service) SearchWiki(ctx context.Context, owner, query string, limit int) ([]Item, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("请提供搜索词")
	}
	items, err := s.Items(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := []Item{}
	for _, item := range items {
		if item.Source == "wiki" && strings.Contains(strings.ToLower(item.Title+"\n"+item.Content), strings.ToLower(query)) {
			if len(item.Content) > 4000 {
				item.Content = string([]rune(item.Content)[:min(1500, len([]rune(item.Content)))])
			}
			out = append(out, item)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
