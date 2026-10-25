package backup

import (
	"fmt"
	"io"
)

const MaxArtifactBytes int64 = 128 << 20
const MaxExpandedBytes int64 = 512 << 20
const MaxArchiveFiles = 10000

func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("备份超过安全大小限制 %d MiB", max>>20)
	}
	return b, nil
}

type boundedWriter struct {
	w         io.Writer
	remaining int64
}

func (w *boundedWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > w.remaining {
		return 0, fmt.Errorf("备份超过安全大小限制")
	}
	n, err := w.w.Write(b)
	w.remaining -= int64(n)
	return n, err
}
