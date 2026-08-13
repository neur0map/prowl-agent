package embed

import (
	_ "embed" // for the //go:embed directives below
	"sync"
)

// The embedder model ships inside the binary: potion-code-16M is a frozen, pinned
// static model (it never receives remote updates), so embedding it removes the
// last setup step -- no download, no daemon, no cache, no network. Semantic code
// search works the instant the binary runs, even fully offline.
//
//go:embed models/potion-code-16M/model.safetensors
var bundledMatrix []byte

//go:embed models/potion-code-16M/tokenizer.json
var bundledTokenizer []byte

var (
	loadOnce sync.Once
	loaded   *Model
	loadErr  error
)

// Load returns the process-wide embedder, parsing the bundled model on first use.
// It is safe for concurrent callers; the model parses at most once.
func Load() (*Model, error) {
	loadOnce.Do(func() {
		loaded, loadErr = loadModel(bundledMatrix, bundledTokenizer)
	})
	return loaded, loadErr
}
