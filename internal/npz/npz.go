// Package npz reads NumPy .npz archives — zip files whose members are .npy
// arrays — covering the subset the green_maps LiDAR pipeline emits:
// little-endian '<f4'/'<f8' numerics and scalar '<U…' unicode strings.
// Format reference: numpy.lib.format (npy versions 1.0–3.0).
package npz

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Array is one .npy member. Numeric dtypes ('<f4', '<f8') are widened to
// float64 in Data; unicode scalars ('<U…') are decoded into Str with Data
// nil. Shape is the numpy shape (empty for scalars; Data then has length 1).
type Array struct {
	Shape []int
	Data  []float64
	Str   string
}

// ReadFile reads the .npz archive at path.
func ReadFile(path string) (map[string]Array, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return Read(f, st.Size())
}

// Read parses the .npz archive in r. Keys are member names with the ".npy"
// suffix stripped.
func Read(r io.ReaderAt, size int64) (map[string]Array, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("npz: %w", err)
	}
	arrays := make(map[string]Array, len(zr.File))
	for _, zf := range zr.File {
		key, ok := strings.CutSuffix(zf.Name, ".npy")
		if !ok {
			return nil, fmt.Errorf("npz: member %q is not a .npy file", zf.Name)
		}
		raw, err := readMember(zf)
		if err != nil {
			return nil, fmt.Errorf("npz: member %q: %w", zf.Name, err)
		}
		a, err := parseNPY(raw)
		if err != nil {
			return nil, fmt.Errorf("npz: member %q: %w", zf.Name, err)
		}
		arrays[key] = a
	}
	return arrays, nil
}

func readMember(zf *zip.File) ([]byte, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

var npyMagic = []byte("\x93NUMPY")

var (
	descrRe   = regexp.MustCompile(`'descr'\s*:\s*'([^']*)'`)
	fortranRe = regexp.MustCompile(`'fortran_order'\s*:\s*(True|False)`)
	shapeRe   = regexp.MustCompile(`'shape'\s*:\s*\(([^)]*)\)`)
	strDescr  = regexp.MustCompile(`^<U([0-9]+)$`)
)

func parseNPY(b []byte) (Array, error) {
	if len(b) < 8 || !bytes.Equal(b[:6], npyMagic) {
		return Array{}, errors.New("bad npy magic")
	}
	major, minor := b[6], b[7]
	// Version 1.x stores the header length as uint16; 2.x/3.x widen it to
	// uint32.
	var preamble, headerLen int
	switch major {
	case 1:
		if len(b) < 10 {
			return Array{}, errors.New("truncated npy preamble")
		}
		headerLen = int(binary.LittleEndian.Uint16(b[8:10]))
		preamble = 10
	case 2, 3:
		if len(b) < 12 {
			return Array{}, errors.New("truncated npy preamble")
		}
		headerLen = int(binary.LittleEndian.Uint32(b[8:12]))
		preamble = 12
	default:
		return Array{}, fmt.Errorf("unsupported npy version %d.%d", major, minor)
	}
	if len(b) < preamble+headerLen {
		return Array{}, errors.New("truncated npy header")
	}
	header := string(b[preamble : preamble+headerLen])
	payload := b[preamble+headerLen:]

	descr, shape, err := parseHeader(header)
	if err != nil {
		return Array{}, err
	}
	count := 1
	for _, n := range shape {
		if n > 0 && count > math.MaxInt/n {
			return Array{}, fmt.Errorf("shape %v overflows element count", shape)
		}
		count *= n
	}

	switch descr {
	case "<f4":
		if err := checkPayload(payload, 4, count, descr, shape); err != nil {
			return Array{}, err
		}
		data := make([]float64, count)
		for i := range data {
			data[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[4*i:])))
		}
		return Array{Shape: shape, Data: data}, nil
	case "<f8":
		if err := checkPayload(payload, 8, count, descr, shape); err != nil {
			return Array{}, err
		}
		data := make([]float64, count)
		for i := range data {
			data[i] = math.Float64frombits(binary.LittleEndian.Uint64(payload[8*i:]))
		}
		return Array{Shape: shape, Data: data}, nil
	default:
		m := strDescr.FindStringSubmatch(descr)
		if m == nil {
			return Array{}, fmt.Errorf("unsupported dtype %q", descr)
		}
		if len(shape) != 0 {
			return Array{}, fmt.Errorf("unicode dtype %q with non-scalar shape %v is not supported", descr, shape)
		}
		width, err := strconv.Atoi(m[1])
		if err != nil {
			return Array{}, fmt.Errorf("unsupported dtype %q", descr)
		}
		if err := checkPayload(payload, 4, width, descr, shape); err != nil {
			return Array{}, err
		}
		return Array{Str: decodeUTF32LE(payload)}, nil
	}
}

func parseHeader(header string) (descr string, shape []int, err error) {
	d := descrRe.FindStringSubmatch(header)
	if d == nil {
		return "", nil, fmt.Errorf("header has no 'descr': %q", header)
	}
	f := fortranRe.FindStringSubmatch(header)
	if f == nil {
		return "", nil, fmt.Errorf("header has no 'fortran_order': %q", header)
	}
	if f[1] == "True" {
		return "", nil, errors.New("fortran_order arrays are not supported")
	}
	s := shapeRe.FindStringSubmatch(header)
	if s == nil {
		return "", nil, fmt.Errorf("header has no 'shape': %q", header)
	}
	shape, err = parseShape(s[1])
	if err != nil {
		return "", nil, err
	}
	return d[1], shape, nil
}

func parseShape(dims string) ([]int, error) {
	dims = strings.TrimSuffix(strings.TrimSpace(dims), ",")
	if dims == "" {
		return nil, nil
	}
	parts := strings.Split(dims, ",")
	shape := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("malformed shape (%s)", dims)
		}
		shape[i] = n
	}
	return shape, nil
}

func checkPayload(payload []byte, itemSize, count int, descr string, shape []int) error {
	if want := itemSize * count; len(payload) != want {
		return fmt.Errorf("payload is %d bytes, want %d (dtype %s, shape %v)", len(payload), want, descr, shape)
	}
	return nil
}

// numpy '<U' payloads are UTF-32LE code units, NUL-padded to the dtype width.
func decodeUTF32LE(b []byte) string {
	runes := make([]rune, len(b)/4)
	for i := range runes {
		runes[i] = rune(binary.LittleEndian.Uint32(b[4*i:]))
	}
	for len(runes) > 0 && runes[len(runes)-1] == 0 {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
