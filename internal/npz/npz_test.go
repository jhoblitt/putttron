package npz_test

import (
	"archive/zip"
	"encoding/binary"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jhoblitt/putttron/internal/npz"
	"github.com/jhoblitt/putttron/internal/npz/npztest"
)

func writeRead(t *testing.T, members map[string]npztest.Member, store bool) map[string]npz.Array {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.npz")
	if err := npztest.Write(path, members, store); err != nil {
		t.Fatalf("npztest.Write: %v", err)
	}
	arrays, err := npz.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return arrays
}

// rawNPY assembles a single .npy member from an arbitrary header dict, so
// tests can craft inputs npztest never writes: fortran order, foreign
// dtypes, v2 preambles, truncated payloads.
func rawNPY(major byte, dict string, payload []byte) []byte {
	lenField := 2
	if major >= 2 {
		lenField = 4
	}
	base := 6 + 2 + lenField
	pad := (64 - (base+len(dict)+1)%64) % 64
	hdr := dict + strings.Repeat(" ", pad) + "\n"
	out := append([]byte("\x93NUMPY"), major, 0)
	if major >= 2 {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(hdr)))
	} else {
		out = binary.LittleEndian.AppendUint16(out, uint16(len(hdr)))
	}
	out = append(out, hdr...)
	return append(out, payload...)
}

func writeRawNPZ(t *testing.T, members map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "raw.npz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, raw := range members {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip member %s: %v", name, err)
		}
		if _, err := w.Write(raw); err != nil {
			t.Fatalf("zip member %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path
}

func f64le(vals ...float64) []byte {
	var b []byte
	for _, v := range vals {
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(v))
	}
	return b
}

func TestRoundTripF4NaN(t *testing.T) {
	nan := float32(math.NaN())
	vals := []float32{nan, -1.25, 0.1, nan, 0, -273.15}
	arrays := writeRead(t, map[string]npztest.Member{"z": {Shape: []int{2, 3}, F32: vals}}, false)
	z, ok := arrays["z"]
	if !ok {
		t.Fatalf("z missing; keys %v", slices.Sorted(maps.Keys(arrays)))
	}
	if !slices.Equal(z.Shape, []int{2, 3}) {
		t.Fatalf("shape = %v, want [2 3]", z.Shape)
	}
	if len(z.Data) != len(vals) {
		t.Fatalf("len(Data) = %d, want %d", len(z.Data), len(vals))
	}
	for i, want := range vals {
		switch got := z.Data[i]; {
		case math.IsNaN(float64(want)):
			if !math.IsNaN(got) {
				t.Errorf("Data[%d] = %v, want NaN", i, got)
			}
		case got != float64(want):
			t.Errorf("Data[%d] = %v, want %v", i, got, float64(want))
		}
	}
}

func TestRoundTripScalarsAndVector(t *testing.T) {
	origin := []float64{494443.2850257461, 3581398.905609859, 715.3603515625}
	arrays := writeRead(t, map[string]npztest.Member{
		"x0":           {F64: []float64{494416.25}},
		"local_origin": {Shape: []int{3}, F64: origin},
	}, false)
	x0 := arrays["x0"]
	if len(x0.Shape) != 0 || len(x0.Data) != 1 || x0.Data[0] != 494416.25 {
		t.Errorf("x0 = %+v, want scalar 494416.25", x0)
	}
	lo := arrays["local_origin"]
	if !slices.Equal(lo.Shape, []int{3}) || !slices.Equal(lo.Data, origin) {
		t.Errorf("local_origin = %+v, want shape [3] data %v", lo, origin)
	}
}

func TestRoundTripUnicode(t *testing.T) {
	const s = "grün ⛳ nördlich — 🏌 hole №7"
	arrays := writeRead(t, map[string]npztest.Member{"crs": {Str: s}}, false)
	crs := arrays["crs"]
	if crs.Str != s {
		t.Errorf("crs = %q, want %q", crs.Str, s)
	}
	if crs.Data != nil || len(crs.Shape) != 0 {
		t.Errorf("unicode scalar carries Data/Shape: %+v", crs)
	}
}

func TestStoreAndDeflate(t *testing.T) {
	members := map[string]npztest.Member{
		"z":      {Shape: []int{2, 2}, F32: []float32{1, -2.5, 3.25, 4}},
		"dx":     {F64: []float64{0.25}},
		"layout": {Str: "row-major north-up"},
	}
	dir := t.TempDir()
	read := func(name string, store bool) map[string]npz.Array {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := npztest.Write(path, members, store); err != nil {
			t.Fatalf("Write(store=%v): %v", store, err)
		}
		wantMethod := uint16(zip.Deflate)
		if store {
			wantMethod = zip.Store
		}
		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("zip open %s: %v", name, err)
		}
		defer zr.Close()
		for _, zf := range zr.File {
			if zf.Method != wantMethod {
				t.Errorf("%s: member %s method = %d, want %d", name, zf.Name, zf.Method, wantMethod)
			}
		}
		arrays, err := npz.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		return arrays
	}
	deflated := read("deflate.npz", false)
	stored := read("store.npz", true)
	if !reflect.DeepEqual(deflated, stored) {
		t.Errorf("store and deflate disagree:\ndeflate: %+v\nstore:   %+v", deflated, stored)
	}
}

func TestRejectFortranOrder(t *testing.T) {
	raw := rawNPY(1, "{'descr': '<f8', 'fortran_order': True, 'shape': (2, 2), }", f64le(1, 2, 3, 4))
	path := writeRawNPZ(t, map[string][]byte{"bad.npy": raw})
	_, err := npz.ReadFile(path)
	if err == nil || !strings.Contains(err.Error(), "fortran") {
		t.Fatalf("err = %v, want fortran_order rejection", err)
	}
}

func TestRejectUnknownDtype(t *testing.T) {
	raw := rawNPY(1, "{'descr': '<i8', 'fortran_order': False, 'shape': (), }", make([]byte, 8))
	path := writeRawNPZ(t, map[string][]byte{"count.npy": raw})
	_, err := npz.ReadFile(path)
	if err == nil || !strings.Contains(err.Error(), "<i8") {
		t.Fatalf("err = %v, want unsupported dtype naming <i8", err)
	}
}

func TestTruncatedPayload(t *testing.T) {
	raw := rawNPY(1, "{'descr': '<f8', 'fortran_order': False, 'shape': (3,), }", f64le(1, 2))
	path := writeRawNPZ(t, map[string][]byte{"z.npy": raw})
	_, err := npz.ReadFile(path)
	if err == nil || !strings.Contains(err.Error(), "z.npy") {
		t.Fatalf("err = %v, want payload-length error naming z.npy", err)
	}
}

func TestBadMagic(t *testing.T) {
	raw := rawNPY(1, "{'descr': '<f8', 'fortran_order': False, 'shape': (), }", f64le(0.25))
	raw[0] ^= 0xff
	path := writeRawNPZ(t, map[string][]byte{"dx.npy": raw})
	_, err := npz.ReadFile(path)
	if err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("err = %v, want bad-magic error", err)
	}
}

func TestV2Header(t *testing.T) {
	raw := rawNPY(2, "{'descr': '<f8', 'fortran_order': False, 'shape': (2,), }", f64le(6341, 0.25))
	path := writeRawNPZ(t, map[string][]byte{"v2.npy": raw})
	arrays, err := npz.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	a := arrays["v2"]
	if !slices.Equal(a.Shape, []int{2}) || !slices.Equal(a.Data, []float64{6341, 0.25}) {
		t.Errorf("v2 = %+v, want shape [2] data [6341 0.25]", a)
	}
}

func TestRealLayoutShapes(t *testing.T) {
	const rows, cols = 12, 10
	z := make([]float32, rows*cols)
	for r := range rows {
		for c := range cols {
			if r == 0 || c == 0 || r == rows-1 || c == cols-1 {
				z[r*cols+c] = float32(math.NaN())
			} else {
				z[r*cols+c] = 715 + 0.01*float32(r*cols+c)
			}
		}
	}
	const crs = "EPSG:6341 (NAD83(2011)/UTM 12N) + NAVD88 m (EPSG:5703)"
	const layout = "row-major north-up: z[0,0] node at (x0, y0), row step -dx north->south"
	arrays := writeRead(t, map[string]npztest.Member{
		"z":            {Shape: []int{rows, cols}, F32: z},
		"x0":           {F64: []float64{494416.25}},
		"y0":           {F64: []float64{3581425.0}},
		"dx":           {F64: []float64{0.25}},
		"local_origin": {Shape: []int{3}, F64: []float64{494443.285, 3581398.906, 715.36}},
		"crs":          {Str: crs},
		"layout":       {Str: layout},
	}, false)
	for _, key := range []string{"z", "x0", "y0", "dx", "local_origin", "crs", "layout"} {
		if _, ok := arrays[key]; !ok {
			t.Errorf("key %q missing", key)
		}
	}
	if got := arrays["z"].Shape; !slices.Equal(got, []int{rows, cols}) {
		t.Errorf("z shape = %v, want [%d %d]", got, rows, cols)
	}
	if data := arrays["z"].Data; len(data) != rows*cols {
		t.Errorf("z has %d values, want %d", len(data), rows*cols)
	} else {
		if !math.IsNaN(data[0]) || !math.IsNaN(data[rows*cols-1]) {
			t.Errorf("NaN border lost: corners %v, %v", data[0], data[rows*cols-1])
		}
		if math.IsNaN(data[cols+1]) {
			t.Errorf("interior cell is NaN")
		}
	}
	for key, want := range map[string]float64{"x0": 494416.25, "y0": 3581425.0, "dx": 0.25} {
		a := arrays[key]
		if len(a.Shape) != 0 || len(a.Data) != 1 || a.Data[0] != want {
			t.Errorf("%s = %+v, want scalar %v", key, a, want)
		}
	}
	if lo := arrays["local_origin"]; !slices.Equal(lo.Shape, []int{3}) || len(lo.Data) != 3 {
		t.Errorf("local_origin = %+v, want shape [3]", lo)
	}
	if got := arrays["crs"].Str; got != crs {
		t.Errorf("crs = %q, want %q", got, crs)
	}
	if got := arrays["layout"].Str; got != layout {
		t.Errorf("layout = %q, want %q", got, layout)
	}
}
