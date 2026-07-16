package course

import (
	"fmt"
	"math"
	"strings"

	"github.com/jhoblitt/putttron/internal/npz"
)

// Legal-pin tiers (green_maps Stage 4.5). The tighter the tier, the flatter
// the required cup bench: premium ⊂ standard ⊂ traditional. TierAt returns the
// TIGHTEST tier a point qualifies for, so a Standard cell is also Traditional.
const (
	TierOffGreen    = -1 // off the putting surface
	TierIllegal     = 0  // on the green but too sloped or too near the edge
	TierTraditional = 1  // macro slope ≤ 3%
	TierStandard    = 2  // ≤ 2% — the headline "legal" set
	TierPremium     = 3  // ≤ 1.5%
)

// TierName is a short human label for a tier value.
func TierName(t int) string {
	switch t {
	case TierPremium:
		return "premium"
	case TierStandard:
		return "standard"
	case TierTraditional:
		return "traditional"
	case TierIllegal:
		return "not a legal pin"
	default:
		return "off the green"
	}
}

// PinZones is a green's legal hole-location map: a per-cell tier grid on the
// same north-up grid as the heightmap (row 0 north, node (i,j) at
// (x0+j·dx, y0−i·dx), local frame). Off-green cells are stored as offGreenCell.
type PinZones struct {
	rows, cols int
	x0, y0, dx float64
	tier       []uint8
	Meta       PinZoneMeta
}

// offGreenCell is the tier_class value the pipeline uses for cells outside the
// putting surface.
const offGreenCell = 255

// loadPinZones reads pin_zones.npz and checks it is registered to the same
// grid as the heightmap (hmX0/hmY0/hmDx and the shared local origin), so the
// tier at a point is exactly the tier under the ball. It recenters to the
// local frame the same way the heightmap does.
func loadPinZones(path string, hmX0, hmY0, hmDx float64, localOrigin []float64, hmRows, hmCols int) (*PinZones, error) {
	arrays, err := npz.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if layout, ok := arrays["layout"]; !ok || !strings.Contains(layout.Str, layoutContract) {
		return nil, fmt.Errorf("%s: layout %q is not %q", path, layout.Str, layoutContract)
	}
	tc, ok := arrays["tier_class"]
	if !ok || len(tc.Shape) != 2 {
		return nil, fmt.Errorf("%s: missing 2-D tier_class array", path)
	}
	rows, cols := tc.Shape[0], tc.Shape[1]
	if rows != hmRows || cols != hmCols {
		return nil, fmt.Errorf("%s: tier grid (%d,%d) does not match the heightmap grid (%d,%d)",
			path, rows, cols, hmRows, hmCols)
	}
	scalar := func(name string) (float64, bool) {
		a, ok := arrays[name]
		if !ok || len(a.Data) != 1 {
			return 0, false
		}
		return a.Data[0], true
	}
	x0, ok1 := scalar("x0")
	y0, ok2 := scalar("y0")
	dx, ok3 := scalar("dx")
	if !ok1 || !ok2 || !ok3 {
		return nil, fmt.Errorf("%s: missing x0/y0/dx", path)
	}
	if x0 != hmX0 || y0 != hmY0 || dx != hmDx {
		return nil, fmt.Errorf("%s: grid origin (%.3f,%.3f,%.3f) does not match the heightmap's (%.3f,%.3f,%.3f)",
			path, x0, y0, dx, hmX0, hmY0, hmDx)
	}

	p := &PinZones{
		rows: rows, cols: cols,
		x0: x0 - localOrigin[0], y0: y0 - localOrigin[1], dx: dx,
		tier: make([]uint8, len(tc.Data)),
	}
	for i, v := range tc.Data {
		if v < 0 || v > 255 || (v > TierPremium && v != offGreenCell) {
			return nil, fmt.Errorf("%s: tier value %g out of range", path, v)
		}
		p.tier[i] = uint8(v)
	}
	return p, nil
}

// TierAt returns the tightest legal tier at a world point (TierPremium down to
// TierIllegal), or TierOffGreen off the putting surface or outside the grid.
func (p *PinZones) TierAt(x, y float64) int {
	j := int(math.Round((x - p.x0) / p.dx))
	i := int(math.Round((p.y0 - y) / p.dx))
	if i < 0 || i >= p.rows || j < 0 || j >= p.cols {
		return TierOffGreen
	}
	if v := p.tier[i*p.cols+j]; v != offGreenCell {
		return int(v)
	}
	return TierOffGreen
}

func (p *PinZones) GridSize() (rows, cols int) { return p.rows, p.cols }
func (p *PinZones) Origin() (x0, y0 float64)   { return p.x0, p.y0 }
func (p *PinZones) CellSize() float64          { return p.dx }

// TierNode returns the raw tier at grid node (i,j): TierOffGreen for an
// off-green cell, else 0..3.
func (p *PinZones) TierNode(i, j int) int {
	if v := p.tier[i*p.cols+j]; v != offGreenCell {
		return int(v)
	}
	return TierOffGreen
}
