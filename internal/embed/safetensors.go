package embed

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// staticTensors holds a model2vec static model's tensors: the embedding matrix
// (required), optional per-token weights for weighted mean-pooling, and an
// optional token->row mapping. Newer models (e.g. potion-code-16M) ship weights
// (and an identity mapping); older ones (potion-base-8M) ship only the matrix.
type staticTensors struct {
	matrix  []float32 // row-major [rows*dim]
	rows    int
	dim     int
	weights []float64 // len rows, or nil (unweighted mean)
	mapping []int     // len rows, or nil (identity)
}

type tensorMeta struct {
	DType   string `json:"dtype"`
	Shape   []int  `json:"shape"`
	Offsets [2]int `json:"data_offsets"`
}

// loadSafetensors parses safetensors bytes (8-byte little-endian header length,
// JSON header, raw tensor bytes) and extracts the model2vec tensors.
func loadSafetensors(raw []byte) (*staticTensors, error) {
	const path = "bundled model"
	if len(raw) < 8 {
		return nil, fmt.Errorf("safetensors %s: too short", path)
	}
	hlen := binary.LittleEndian.Uint64(raw[:8])
	if 8+hlen > uint64(len(raw)) {
		return nil, fmt.Errorf("safetensors %s: header length %d exceeds file", path, hlen)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw[8:8+hlen], &header); err != nil {
		return nil, fmt.Errorf("safetensors %s: header: %w", path, err)
	}
	base := 8 + int(hlen)
	tensor := func(name string) ([]byte, tensorMeta, bool, error) {
		rm, ok := header[name]
		if !ok {
			return nil, tensorMeta{}, false, nil
		}
		var m tensorMeta
		if err := json.Unmarshal(rm, &m); err != nil {
			return nil, m, true, fmt.Errorf("safetensors %s: tensor %s meta: %w", path, name, err)
		}
		s, e := base+m.Offsets[0], base+m.Offsets[1]
		if s < base || e > len(raw) || e < s {
			return nil, m, true, fmt.Errorf("safetensors %s: tensor %s bytes out of range", path, name)
		}
		return raw[s:e], m, true, nil
	}

	eb, em, ok, err := tensor("embeddings")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("safetensors %s: no \"embeddings\" tensor", path)
	}
	if em.DType != "F32" || len(em.Shape) != 2 {
		return nil, fmt.Errorf("safetensors %s: embeddings dtype %q shape %v (want F32 matrix)", path, em.DType, em.Shape)
	}
	rows, dim := em.Shape[0], em.Shape[1]
	if len(eb) != rows*dim*4 {
		return nil, fmt.Errorf("safetensors %s: embeddings byte count mismatch", path)
	}
	matrix := make([]float32, rows*dim)
	for i := range matrix {
		matrix[i] = math.Float32frombits(binary.LittleEndian.Uint32(eb[i*4:]))
	}
	st := &staticTensors{matrix: matrix, rows: rows, dim: dim}

	if wb, wm, ok, err := tensor("weights"); err != nil {
		return nil, err
	} else if ok {
		if wm.DType != "F64" || len(wm.Shape) != 1 || wm.Shape[0] != rows || len(wb) != rows*8 {
			return nil, fmt.Errorf("safetensors %s: weights dtype/shape mismatch", path)
		}
		w := make([]float64, rows)
		for i := range w {
			w[i] = math.Float64frombits(binary.LittleEndian.Uint64(wb[i*8:]))
		}
		st.weights = w
	}

	if mb, mm, ok, err := tensor("mapping"); err != nil {
		return nil, err
	} else if ok {
		if mm.DType != "I64" || len(mm.Shape) != 1 || mm.Shape[0] != rows || len(mb) != rows*8 {
			return nil, fmt.Errorf("safetensors %s: mapping dtype/shape mismatch", path)
		}
		mp := make([]int, rows)
		identity := true
		for i := range mp {
			v := int(int64(binary.LittleEndian.Uint64(mb[i*8:])))
			if v < 0 || v >= rows {
				return nil, fmt.Errorf("safetensors %s: mapping[%d]=%d out of range", path, i, v)
			}
			mp[i] = v
			if v != i {
				identity = false
			}
		}
		if !identity { // store only when it actually remaps, to skip the lookup
			st.mapping = mp
		}
	}
	return st, nil
}
