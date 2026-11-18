package collector

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

func (s *Service) readSheetTool(ctx context.Context, owner, identity string, a map[string]any) (any, error) {
	targets, err := list[Target](ctx, s.Store, owner, "target")
	if err != nil {
		return nil, err
	}
	var target *Target
	for _, t := range targets {
		if t.Source != "sheet" {
			continue
		}
		if text(a, "name") != "" && (strings.Contains(strings.ToLower(t.Name), strings.ToLower(text(a, "name"))) || t.ID == text(a, "name")) || text(a, "url") != "" && t.URL == text(a, "url") {
			cp := t
			target = &cp
			break
		}
	}
	if target == nil {
		if text(a, "url") == "" {
			return nil, errors.New("请提供表格 url 或已配置目标 name")
		}
		target = &Target{ID: "read", OwnerID: owner, Source: "sheet", URL: text(a, "url"), Config: map[string]any{"discover_sheets": true, "range": "A1:AZ500", "header_row": 1}}
	}
	target.Config["identity"] = identity
	target.Config["only_mine"] = boolean(a, "only_mine", false)
	if !boolean(a, "refresh", true) && target.ID != "read" && boolean(a, "only_mine", false) {
		snapshot, e := load[Snapshot](ctx, s.Store, owner, "snapshot", target.ID)
		if e == nil {
			profile, e := s.Profile(ctx, owner)
			if e != nil {
				return nil, e
			}
			return map[string]any{"rows": mySheetItems(snapshot.Items, profile), "cached": true, "updated_at": snapshot.ScannedAt}, nil
		}
		if !errors.Is(e, ErrNotFound) {
			return nil, e
		}
	}
	profile, err := s.Profile(ctx, owner)
	if err != nil {
		return nil, err
	}
	rows, err := s.fetchSheetRows(ctx, *target, profile, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(rows), "rows": rows, "cached": false}, nil
}

func (s *Service) searchWikiTool(ctx context.Context, owner, identity string, a map[string]any) (any, error) {
	question := text(a, "question")
	if question == "" {
		return nil, errors.New("请提供 question")
	}
	limit := min(100, max(1, integer(a, "limit", 5)))
	results := []Item{}
	seen := map[string]bool{}
	add := func(items []Item) {
		for _, item := range items {
			if seen[item.ExternalID] {
				continue
			}
			haystack := strings.ToLower(item.Title + "\n" + item.Content)
			matched := strings.Contains(haystack, strings.ToLower(question))
			if !matched {
				for _, term := range strings.Fields(question) {
					if strings.Contains(haystack, strings.ToLower(term)) {
						matched = true
						break
					}
				}
			}
			if matched {
				seen[item.ExternalID] = true
				results = append(results, item)
			}
		}
	}
	roots := []string{}
	if root := text(a, "root_url"); root != "" {
		roots = append(roots, root)
	}
	if boolean(a, "include_configured", true) {
		items, err := s.Items(ctx, owner)
		if err != nil {
			return nil, err
		}
		docs := []Item{}
		for _, item := range items {
			if item.Source == "wiki" {
				docs = append(docs, item)
			}
		}
		add(docs)
		targets, err := list[Target](ctx, s.Store, owner, "target")
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if target.Source == "wiki" && boolean(target.Config, "search_only", false) && !contains(roots, target.URL) {
				roots = append(roots, target.URL)
			}
		}
	}
	for _, root := range roots {
		nodes, err := s.wikiTree(ctx, owner, identity, root, true, 5, min(1000, max(1, integer(a, "max_nodes", 80))))
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			kind, token := text(node, "obj_type"), text(node, "obj_token")
			item := Item{ExternalID: token, Title: text(node, "title"), Source: "wiki", URL: "https://feishu.cn/wiki/" + text(node, "node_token")}
			if kind == "docx" || kind == "doc" {
				if boolean(a, "read_content", true) {
					content, e := s.readDocument(ctx, owner, identity, kind, token)
					if e != nil {
						return nil, e
					}
					item.Content = content
				}
			} else if (kind == "sheet" || kind == "sheets") && boolean(a, "include_sheets", true) {
				profile, e := s.Profile(ctx, owner)
				if e != nil {
					return nil, e
				}
				t := Target{ID: "search", OwnerID: owner, Source: "sheet", URL: "https://feishu.cn/sheets/" + token, Config: map[string]any{"identity": identity, "discover_sheets": true, "only_mine": false}}
				rows, e := s.fetchSheetRows(ctx, t, profile, true)
				if e != nil {
					return nil, e
				}
				parts := []string{}
				for _, row := range rows {
					parts = append(parts, row.Title+" "+fieldSearchText(row.Fields))
				}
				item.Content = strings.Join(parts, "\n")
			} else {
				continue
			}
			add([]Item{item})
		}
	}
	for i := range results {
		runes := []rune(results[i].Content)
		if len(runes) > 4000 {
			results[i].Content = string(runes[:4000])
		}
	}
	return map[string]any{"items": results[:min(limit, len(results))], "count": len(results), "question": question, "source": "本人配置的快照与指定 Wiki 范围"}, nil
}

func mySheetItems(items []Item, p Profile) []Item {
	out := []Item{}
	for _, item := range items {
		if matchesAny(item.Assignees, append([]string{p.DisplayName, p.FeishuName, p.JiraAccount}, p.Aliases...)) {
			out = append(out, item)
		}
	}
	return out
}

func fieldSearchText(fields map[string]any) string {
	values := []string{}
	for k, v := range fields {
		values = append(values, k+": "+cellText(v))
	}
	return strings.Join(values, " ")
}

// readWhiteboards returns boards referenced by document blocks. The same user
// token is used for each request, including nested resources.
func (s *Service) readWhiteboards(ctx context.Context, owner, identity, documentID string) ([]map[string]any, error) {
	boards := []map[string]any{}
	pageToken := ""
	for page := 0; page < 50; page++ {
		q := url.Values{"page_size": {"500"}}
		if pageToken != "" {
			q.Set("page_token", pageToken)
		}
		data, err := s.Feishu(ctx, owner, identity, "GET", "/open-apis/docx/v1/documents/"+documentID+"/blocks", q, nil)
		if err != nil {
			return nil, err
		}
		for _, v := range array(data["items"]) {
			board := object(object(v)["board"])
			id := text(board, "token")
			if id == "" {
				continue
			}
			if _, err := segment(id); err != nil {
				return nil, err
			}
			nodes, err := s.Feishu(ctx, owner, identity, "GET", "/open-apis/board/v1/whiteboards/"+id+"/nodes", nil, nil)
			if err != nil {
				return nil, err
			}
			boards = append(boards, map[string]any{"token": id, "data": nodes})
		}
		if !boolean(data, "has_more", false) {
			return boards, nil
		}
		next := text(data, "page_token")
		if next == "" || next == pageToken {
			return nil, errors.New("文档块分页游标异常")
		}
		pageToken = next
	}
	return nil, errors.New("文档块超过读取上限")
}
