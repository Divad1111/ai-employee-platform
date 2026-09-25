package codex

import (
	"encoding/json"
	"testing"
)

func TestParseCodexModelList(t *testing.T) {
	raw := json.RawMessage(`{"data":[{"id":"gpt-5.6-terra","displayName":"GPT-5.6-Terra","hidden":false},{"id":"secret","displayName":"隐藏","hidden":true}]}`)
	got, err := parseCodexModelList(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "gpt-5.6-terra" || got[0].Label != "GPT-5.6-Terra" {
		t.Fatalf("got %#v", got)
	}
}
