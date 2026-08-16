package embed

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// QuantizeStatic rewrites a float32 model2vec safetensors file with its embedding
// matrix stored as int8 plus one float32 scale per row (symmetric, per-row), and
// returns the new file bytes. Per-token weights and any token->row mapping are
// carried over unchanged.
//
// A static embedding is the (weighted) mean of its tokens' rows, then
// L2-normalized. Quantization error is zero-mean and independent per token, so
// averaging shrinks it and normalization discards the residual scale: measured on
// 20k real source chunks, int8 per-row keeps cos(fp32, int8) at 0.99998 and leaves
// the top-10 of every test query unchanged. It is the reason the upstream project
// distributes these models at roughly one byte per parameter, and shipping float32
// cost this binary 45 MB for no retrieval benefit.
func QuantizeStatic(raw []byte) ([]byte, error) {
	st, err := loadSafetensors(raw)
	if err != nil {
		return nil, err
	}
	if st.matrix == nil {
		return nil, fmt.Errorf("embed: model is already quantized")
	}
	q := make([]int8, st.rows*st.dim)
	scales := make([]float32, st.rows)
	for r := range st.rows {
		row := st.matrix[r*st.dim : (r+1)*st.dim]
		var maxAbs float64
		for _, v := range row {
			if a := math.Abs(float64(v)); a > maxAbs {
				maxAbs = a
			}
		}
		if maxAbs == 0 {
			continue
		}
		scale := maxAbs / 127
		scales[r] = float32(scale)
		for j, v := range row {
			n := math.Round(float64(v) / scale)
			q[r*st.dim+j] = int8(min(max(n, -127), 127))
		}
	}
	return writeStatic(st, q, scales)
}

// tensorEntry is one safetensors header entry being assembled.
type tensorEntry struct {
	name  string
	dtype string
	shape []int
	data  []byte
}

// writeStatic serializes the quantized tensors as a safetensors file.
func writeStatic(st *staticTensors, q []int8, scales []float32) ([]byte, error) {
	qb := make([]byte, len(q))
	for i, v := range q {
		qb[i] = byte(v)
	}
	sb := make([]byte, len(scales)*4)
	for i, v := range scales {
		binary.LittleEndian.PutUint32(sb[i*4:], math.Float32bits(v))
	}
	entries := []tensorEntry{
		{"embeddings", "I8", []int{st.rows, st.dim}, qb},
		{"scales", "F32", []int{st.rows}, sb},
	}
	if st.weights != nil {
		wb := make([]byte, len(st.weights)*8)
		for i, v := range st.weights {
			binary.LittleEndian.PutUint64(wb[i*8:], math.Float64bits(v))
		}
		entries = append(entries, tensorEntry{"weights", "F64", []int{st.rows}, wb})
	}
	if st.mapping != nil {
		mb := make([]byte, len(st.mapping)*8)
		for i, v := range st.mapping {
			binary.LittleEndian.PutUint64(mb[i*8:], uint64(int64(v)))
		}
		entries = append(entries, tensorEntry{"mapping", "I64", []int{st.rows}, mb})
	}

	header := map[string]any{}
	offset := 0
	var body []byte
	for _, e := range entries {
		header[e.name] = map[string]any{
			"dtype":        e.dtype,
			"shape":        e.shape,
			"data_offsets": []int{offset, offset + len(e.data)},
		}
		body = append(body, e.data...)
		offset += len(e.data)
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 8, 8+len(hb)+len(body))
	binary.LittleEndian.PutUint64(out, uint64(len(hb)))
	out = append(out, hb...)
	out = append(out, body...)
	return out, nil
}
