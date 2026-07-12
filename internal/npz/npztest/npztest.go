// Package npztest writes synthetic .npz archives so tests elsewhere in the
// repo can exercise the npz reader hermetically.
package npztest

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Member is one array to write. Exactly one of F32, F64, Str should be set
// (Str implies scalar shape). Shape applies to F32/F64.
type Member struct {
	Shape []int
	F32   []float32
	F64   []float64
	Str   string
}

// Write creates path as a real .npz: a zip whose members are byte-exact
// npy v1.0 files (64-byte-aligned space-padded headers ending in \n).
// Deflate compression by default; store when store is true (both appear in
// the wild and the reader must handle both).
func Write(path string, members map[string]Member, store bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := writeZip(f, members, store); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeZip(f *os.File, members map[string]Member, store bool) error {
	method := uint16(zip.Deflate)
	if store {
		method = zip.Store
	}
	zw := zip.NewWriter(f)
	for _, name := range slices.Sorted(maps.Keys(members)) {
		raw, err := encodeNPY(members[name])
		if err != nil {
			return fmt.Errorf("npztest: member %q: %w", name, err)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name + ".npy", Method: method})
		if err != nil {
			return err
		}
		if _, err := w.Write(raw); err != nil {
			return err
		}
	}
	return zw.Close()
}

func encodeNPY(m Member) ([]byte, error) {
	set := 0
	for _, on := range []bool{m.F32 != nil, m.F64 != nil, m.Str != ""} {
		if on {
			set++
		}
	}
	if set != 1 {
		return nil, fmt.Errorf("exactly one of F32, F64, Str must be set (have %d)", set)
	}

	var descr string
	shape := m.Shape
	var payload []byte
	switch {
	case m.F32 != nil:
		if n := elemCount(shape); n != len(m.F32) {
			return nil, fmt.Errorf("shape %v holds %d elements, F32 has %d", shape, n, len(m.F32))
		}
		descr = "<f4"
		payload = make([]byte, 0, 4*len(m.F32))
		for _, v := range m.F32 {
			payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(v))
		}
	case m.F64 != nil:
		if n := elemCount(shape); n != len(m.F64) {
			return nil, fmt.Errorf("shape %v holds %d elements, F64 has %d", shape, n, len(m.F64))
		}
		descr = "<f8"
		payload = make([]byte, 0, 8*len(m.F64))
		for _, v := range m.F64 {
			payload = binary.LittleEndian.AppendUint64(payload, math.Float64bits(v))
		}
	default:
		runes := []rune(m.Str)
		descr = "<U" + strconv.Itoa(len(runes))
		shape = nil
		payload = make([]byte, 0, 4*len(runes))
		for _, r := range runes {
			payload = binary.LittleEndian.AppendUint32(payload, uint32(r))
		}
	}

	pre, err := preamble(descr, shape)
	if err != nil {
		return nil, err
	}
	return append(pre, payload...), nil
}

// preamble builds the npy v1.0 magic+version+length+header block, space-
// padded so its total length is a multiple of 64 and ending in '\n',
// matching numpy.lib.format byte for byte.
func preamble(descr string, shape []int) ([]byte, error) {
	dict := fmt.Sprintf("{'descr': '%s', 'fortran_order': False, 'shape': %s, }", descr, shapeExpr(shape))
	base := 6 + 2 + 2 // magic, version, uint16 header length
	pad := (64 - (base+len(dict)+1)%64) % 64
	hdr := dict + strings.Repeat(" ", pad) + "\n"
	if len(hdr) > math.MaxUint16 {
		return nil, fmt.Errorf("header too long for npy v1.0 (%d bytes)", len(hdr))
	}
	out := make([]byte, 0, base+len(hdr))
	out = append(out, "\x93NUMPY\x01\x00"...)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(hdr)))
	return append(out, hdr...), nil
}

func shapeExpr(shape []int) string {
	switch len(shape) {
	case 0:
		return "()"
	case 1:
		return "(" + strconv.Itoa(shape[0]) + ",)"
	}
	parts := make([]string, len(shape))
	for i, n := range shape {
		parts[i] = strconv.Itoa(n)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func elemCount(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}
