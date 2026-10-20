package codex

import (
	"context"
	"github.com/ai-employee-platform/workstation/internal/acp"
)

func turnInput(ctx context.Context, text string) []map[string]any {
	input := []map[string]any{{"type": "text", "text": text}}
	for _, file := range acp.InputFiles(ctx) {
		if acp.NativeImage(file.MIMEType) {
			input = append(input, map[string]any{"type": "localImage", "path": file.Path})
		}
	}
	return input
}
