package workflowmcp

import (
	"strings"
	"unicode"
)
// Tokenize 将文本切分为 ASCII 词 + CJK 单字与 bigram（对齐 PersonalWorkMCP）。
func Tokenize(text string) []string {
	if text == "" {
		return nil
	}
	var tokens []string
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		// ASCII 连续词
		if isASCIIWordStart(runes[i]) {
			start := i
			for i < len(runes) && isASCIIWordChar(runes[i]) {
				i++
			}
			tokens = append(tokens, strings.ToLower(string(runes[start:i])))
			continue
		}
		// CJK 连续段
		if isCJK(runes[i]) {
			start := i
			for i < len(runes) && isCJK(runes[i]) {
				i++
			}
			run := runes[start:i]
			for _, ch := range run {
				tokens = append(tokens, string(ch))
			}
			for j := 0; j < len(run)-1; j++ {
				tokens = append(tokens, string(run[j:j+2]))
			}
			continue
		}
		i++
	}
	return tokens
}

// TokenizeJoined 分词后以空格拼接，供 to_tsvector('simple', ...) 使用。
func TokenizeJoined(text string) string {
	return strings.Join(Tokenize(text), " ")
}

// TokenizeToTSQuery 构造 OR 连接的 tsquery 文本（simple 配置）。
func TokenizeToTSQuery(text string) string {
	toks := Tokenize(text)
	if len(toks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(toks))
	for _, t := range toks {
		// 过滤 tsquery 特殊字符
		t = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '\'' || r == ':' || r == '&' || r == '|' || r == '!' || r == '(' || r == ')' {
				return -1
			}
			return r
		}, t)
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " | ")
}

// ScoreText 内存 token overlap 打分（工作流/技能搜索）。
func ScoreText(query, text string) float64 {
	qt := uniqueSet(Tokenize(query))
	if len(qt) == 0 {
		return 0
	}
	tt := uniqueSet(Tokenize(text))
	if len(tt) == 0 {
		return 0
	}
	overlap := 0
	for k := range qt {
		if _, ok := tt[k]; ok {
			overlap++
		}
	}
	return float64(overlap) / float64(len(qt))
}

func uniqueSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

func isASCIIWordStart(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
}

func isASCIIWordChar(r rune) bool {
	return isASCIIWordStart(r)
}

func isCJK(r rune) bool {
	return (r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf)
}
