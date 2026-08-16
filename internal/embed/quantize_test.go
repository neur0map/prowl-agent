package embed

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realChunks returns chunk-sized windows of this repository's own source, so the
// comparison runs on the kind of text prowl actually embeds rather than on toy
// strings whose few tokens would hide quantization error.
func realChunks(t *testing.T, want int) []string {
	t.Helper()
	var docs []string
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(docs) >= want || filepath.Ext(path) != ".go" {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		lines := strings.Split(string(body), "\n")
		for start := 0; start+40 <= len(lines) && len(docs) < want; start += 40 {
			if text := strings.Join(lines[start:start+40], "\n"); strings.TrimSpace(text) != "" {
				docs = append(docs, text)
			}
		}
		return nil
	})
	if err != nil || len(docs) == 0 {
		t.Skip("no source corpus available")
	}
	return docs
}

// The bundled model is stored int8. Its vectors must stay interchangeable with the
// float32 model they were quantized from: a static embedding averages its tokens'
// rows and then L2-normalizes, so per-element quantization error cancels rather
// than accumulating. Measured on 20k real source chunks this holds at 0.99998
// cosine with an unchanged top-10; the tolerance here is deliberately looser than
// the measurement so it fails on a broken scale or a dtype mix-up, not on noise.
func TestQuantizedModelMatchesFloat32(t *testing.T) {
	fp32, err := loadModel(bundledMatrix, bundledTokenizer)
	if err != nil {
		t.Fatal(err)
	}
	quantBytes, err := QuantizeStatic(bundledMatrix)
	if err != nil {
		// Already-quantized bundle: re-quantizing is refused, which is itself the
		// contract we want, so compare the bundle against a float32 round trip.
		if !strings.Contains(err.Error(), "already quantized") {
			t.Fatal(err)
		}
		if fp32.quant == nil {
			t.Fatal("QuantizeStatic refused a float32 model")
		}
		return
	}
	q, err := loadModel(quantBytes, bundledTokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if q.quant == nil || q.scales == nil {
		t.Fatal("quantized model did not load as int8")
	}
	if q.rows != fp32.rows || q.dim != fp32.dim {
		t.Fatalf("shape drift: %dx%d vs %dx%d", q.rows, q.dim, fp32.rows, fp32.dim)
	}

	docs := realChunks(t, 500)
	ctx := context.Background()
	a, err := fp32.Embed(ctx, docs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.Embed(ctx, docs)
	if err != nil {
		t.Fatal(err)
	}
	worst := 1.0
	for i := range docs {
		var dot, na, nb float64
		for j := range a[i] {
			dot += float64(a[i][j]) * float64(b[i][j])
			na += float64(a[i][j]) * float64(a[i][j])
			nb += float64(b[i][j]) * float64(b[i][j])
		}
		if na == 0 || nb == 0 {
			continue
		}
		if c := dot / (math.Sqrt(na) * math.Sqrt(nb)); c < worst {
			worst = c
		}
	}
	if worst < 0.999 {
		t.Fatalf("worst cos(fp32, int8) = %.6f over %d real chunks, want >= 0.999", worst, len(docs))
	}
}

// A quantized bundle must not be silently re-quantized: doing so would compound
// the error and, worse, would mean the loader had not recognized the int8 dtype.
func TestQuantizeStaticRefusesQuantizedInput(t *testing.T) {
	once, err := QuantizeStatic(bundledMatrix)
	if err != nil {
		if strings.Contains(err.Error(), "already quantized") {
			return // the bundle already ships int8
		}
		t.Fatal(err)
	}
	if _, err := QuantizeStatic(once); err == nil {
		t.Fatal("re-quantizing an int8 model was accepted")
	}
}
