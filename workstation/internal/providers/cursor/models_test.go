package cursor

import (
	"context"
	"encoding/json"
	"testing"
)

func TestParseModelLines(t *testing.T) {
	text := "Available models\n\nauto - Auto (current, default)\ncomposer-2.5 - Composer 2.5\nbad line\n"
	got := ParseModelLines(text)
	if len(got) != 2 || got[0].ID != "auto" || got[1].Label != "Composer 2.5" {
		t.Fatalf("got %#v", got)
	}
}

func TestModelsFromSessionResult(t *testing.T) {
	raw := json.RawMessage(`{"configOptions":[{"id":"model","category":"model","options":[{"value":"default[]","name":"Auto"},{"value":"gemini-3.8-flash[reasoning_effort=high]","name":"gemini-3.8-flash"}]}]}`)
	got, err := modelsFromSessionResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "default[]" || got[1].Label != "gemini-3.8-flash · high" {
		t.Fatalf("got %#v", got)
	}
}

func TestLiveListModels(t *testing.T) {
	p := NewProvider(nil, "")
	models, err := p.ListModels(context.Background())
	t.Logf("models: %+v, err: %v", models, err)
}

