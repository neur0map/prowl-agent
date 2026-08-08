package extract

import (
	"fmt"
	"strings"
	"testing"
)

// makeLines builds an n-line source, applying per-line overrides (1-based).
func makeLines(n int, overrides map[int]string) ([]byte, []string) {
	lines := make([]string, n)
	for i := 1; i <= n; i++ {
		if s, ok := overrides[i]; ok {
			lines[i-1] = s
		} else {
			lines[i-1] = fmt.Sprintf("// filler %d", i)
		}
	}
	return []byte(strings.Join(lines, "\n")), lines
}

// assertCovers checks the chunks tile [1,n] exactly: start at 1, contiguous, no
// overlap, end at n, and each chunk's text is its own line range.
func assertCovers(t *testing.T, chunks []Chunk, lines []string) {
	t.Helper()
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if chunks[0].StartLine != 1 {
		t.Fatalf("first chunk starts at %d, want 1", chunks[0].StartLine)
	}
	for i, c := range chunks {
		if c.StartLine < 1 || c.EndLine > len(lines) || c.StartLine > c.EndLine {
			t.Fatalf("chunk %d has bad range %d-%d (n=%d)", i, c.StartLine, c.EndLine, len(lines))
		}
		if i > 0 && c.StartLine != chunks[i-1].EndLine+1 {
			t.Fatalf("chunk %d starts at %d, want %d (contiguous)", i, c.StartLine, chunks[i-1].EndLine+1)
		}
		if want := strings.Join(lines[c.StartLine-1:c.EndLine], "\n"); c.Text != want {
			t.Fatalf("chunk %d text mismatch", i)
		}
	}
	if last := chunks[len(chunks)-1]; last.EndLine != len(lines) {
		t.Fatalf("last chunk ends at %d, want %d", last.EndLine, len(lines))
	}
}

// chunkAt returns the chunk containing 1-based line.
func chunkAt(chunks []Chunk, line int) Chunk {
	for _, c := range chunks {
		if line >= c.StartLine && line <= c.EndLine {
			return c
		}
	}
	return Chunk{}
}

func TestChunkStructuredKeepsSymbolWhole(t *testing.T) {
	// A function spans lines 30-70, crossing the fixed 40-line window boundary.
	// The deep call is at line 65; the signature is at line 30.
	src, lines := makeLines(100, map[int]string{
		30: "func Pay(amount int) error {",
		65: "\treconcileLedger(amount)",
	})
	chunks := chunkStructured(src, []Symbol{{Name: "Pay", Kind: "function", StartLine: 30, EndLine: 70}}, 40)
	assertCovers(t, chunks, lines)

	deep := chunkAt(chunks, 65)
	if deep.StartLine > 30 || deep.EndLine < 70 {
		t.Fatalf("chunk for line 65 is %d-%d, want it to contain the whole 30-70 symbol", deep.StartLine, deep.EndLine)
	}
	if !strings.Contains(deep.Text, "func Pay") {
		t.Fatalf("chunk for the deep call must include the signature; got:\n%s", deep.Text)
	}
	if !strings.Contains(deep.Text, "reconcileLedger") {
		t.Fatalf("chunk must include the deep call; got:\n%s", deep.Text)
	}
}

func TestChunkStructuredNoSymbolsMatchesWindows(t *testing.T) {
	src, lines := makeLines(100, nil)
	chunks := chunkStructured(src, nil, 40)
	assertCovers(t, chunks, lines)
	got := make([][2]int, len(chunks))
	for i, c := range chunks {
		got[i] = [2]int{c.StartLine, c.EndLine}
	}
	want := [][2]int{{1, 40}, {41, 80}, {81, 100}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("no-symbol chunks = %v, want fixed windows %v", got, want)
	}
}

func TestChunkStructuredKeepsOversizedSymbolWholeUpToHard(t *testing.T) {
	// A 60-line symbol exceeds the 40-line window but is under window*2, so it
	// stays whole rather than being split mid-body.
	src, lines := makeLines(100, map[int]string{20: "func Mid() {", 79: "}"})
	chunks := chunkStructured(src, []Symbol{{Name: "Mid", Kind: "function", StartLine: 20, EndLine: 79}}, 40)
	assertCovers(t, chunks, lines)
	c := chunkAt(chunks, 50)
	if c.StartLine > 20 || c.EndLine < 79 {
		t.Fatalf("chunk for line 50 is %d-%d, want the whole 20-79 symbol kept together", c.StartLine, c.EndLine)
	}
}

func TestChunkStructuredSplitsHugeSymbol(t *testing.T) {
	// A 100-line symbol exceeds window*2, so it must be split into windows.
	src, lines := makeLines(100, map[int]string{1: "func Huge() {"})
	chunks := chunkStructured(src, []Symbol{{Name: "Huge", Kind: "function", StartLine: 1, EndLine: 100}}, 40)
	assertCovers(t, chunks, lines)
	if len(chunks) < 2 {
		t.Fatalf("a 100-line symbol under window*2=80 must split, got %d chunk(s)", len(chunks))
	}
	for _, c := range chunks {
		if c.EndLine-c.StartLine+1 > 80 {
			t.Fatalf("chunk %d-%d exceeds the hard cap window*2=80", c.StartLine, c.EndLine)
		}
	}
}

func TestChunkStructuredPacksSmallSymbols(t *testing.T) {
	// Many one-line symbols must not become one chunk each; they pack up to window.
	overrides := map[int]string{}
	syms := make([]Symbol, 0, 30)
	for i := 1; i <= 30; i++ {
		overrides[i] = fmt.Sprintf("const c%d = %d", i, i)
		syms = append(syms, Symbol{Name: fmt.Sprintf("c%d", i), Kind: "const", StartLine: i, EndLine: i})
	}
	src, lines := makeLines(30, overrides)
	chunks := chunkStructured(src, syms, 40)
	assertCovers(t, chunks, lines)
	if len(chunks) != 1 {
		t.Fatalf("30 one-line symbols under a 40-line window = %d chunks, want 1 packed chunk", len(chunks))
	}
}
