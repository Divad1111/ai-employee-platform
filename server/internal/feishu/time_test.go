package feishu_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/feishu"
)

func TestCardTimeUsesServerTimezoneAnd24Hours(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("Server", 8*60*60)
	defer func() { time.Local = previous }()
	at := time.Date(2026, 11, 13, 15, 59, 59, 0, time.UTC)
	if got := feishu.FormatCardTime(at); got != "2026-11-13 23:59:59 +08:00" {
		t.Fatal(got)
	}
	card := feishu.BuildInquirySubmittedCard("job", "megre", "回答\n第二行", at, true)
	if card.Schema != "2.0" || card.Header.Title.Content != "megre · 已提交" || card.Header.Template != "green" {
		t.Fatal(card)
	}
	json := card.MustJSON()
	if !strings.Contains(json, "23:59:59 +08:00") || strings.Contains(json, `"tag":"form"`) || strings.Contains(json, `"tag":"input"`) {
		t.Fatal(json)
	}
	if legacy := feishu.BuildInquirySubmittedCard("job", "megre", "允许", at, false); legacy.Schema != "" || legacy.Body != nil {
		t.Fatal("legacy card changed schema")
	}
}
