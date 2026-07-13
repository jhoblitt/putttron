package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jhoblitt/putttron/internal/course"
	"github.com/jhoblitt/putttron/internal/geom"
	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

//go:embed serve_ui.html
var serveUI string

// Interactive quality trades Monte Carlo precision for turnaround: "quick" is
// for placing a pin and looking around, "full" matches the committed sweeps.
var qualities = map[string]struct{ Trials, FieldTrials, FieldSweeps int }{
	"quick": {2000, 400, 3},
	"full":  {8000, 1500, 5},
}

// elevContourStepM is the contour interval drawn on the green map. Green
// contours are conventionally tight; the source data resolves macro contours
// only (5-10 cm RMSE), so anything finer would be drawing noise.
const elevContourStepM = 0.025

type server struct {
	repo string
	idx  *course.Index

	mu      sync.Mutex
	greens  map[string]*course.Green
	fields  *fieldCache
	jobs    map[string]*jobState
	order   []string
	running bool
	nextID  int
}

type jobState struct {
	ID   string `json:"id"`
	Seed uint64 `json:"seed"`

	mu    sync.Mutex
	State string `json:"state"` // running | done | error
	Phase string `json:"phase"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Err   string `json:"error,omitempty"`

	spec RunSpec
	res  *RunResult
}

func cmdServe(args []string) {
	fs := newFlagSet("serve")
	greensDir := fs.String("greens", defaultGreensRepo, "greens repository (green_maps output tree)")
	port := fs.Int("port", 7888, "port to listen on (localhost only)")
	fs.Parse(args)

	repo := expandHome(*greensDir)
	idx, err := course.LoadIndex(repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s := newServer(repo, idx)
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "putttron serve: %d greens from %s\n", len(idx.Greens), repo)
	fmt.Fprintf(os.Stderr, "open http://%s\n", addr)
	if err := http.Serve(ln, s.routes()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newServer(repo string, idx *course.Index) *server {
	return &server{
		repo: repo, idx: idx,
		greens: map[string]*course.Green{},
		fields: newFieldCache(),
		jobs:   map[string]*jobState{},
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, serveUI)
	})
	mux.HandleFunc("GET /api/greens", s.handleGreens)
	mux.HandleFunc("GET /api/green/{label}", s.handleGreen)
	mux.HandleFunc("POST /api/run", s.handleRun)
	mux.HandleFunc("GET /api/job/{id}", s.handleJob)
	mux.HandleFunc("GET /api/job/{id}/export.csv", s.handleExportCSV)
	mux.HandleFunc("GET /api/job/{id}/manifest.yaml", s.handleExportManifest)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, args...)})
}

// loadGreen caches surfaces: reparsing a heightmap on every click would make
// the UI feel broken.
func (s *server) loadGreen(label string, stimp float64) (*course.Green, error) {
	s.mu.Lock()
	g, ok := s.greens[label]
	s.mu.Unlock()
	if !ok {
		var err error
		g, err = course.LoadGreen(s.idx, label, physics.DecelFromStimp(stimp))
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.greens[label] = g
		s.mu.Unlock()
	}
	return g, nil
}

func (s *server) handleGreens(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Label        string   `json:"label"`
		Hole         int      `json:"hole"`
		SlopeMeanPct float64  `json:"slope_mean_pct"`
		ElevRangeM   float64  `json:"elevation_range_m"`
		Flags        []string `json:"flags"`
		NeedsReview  bool     `json:"needs_review"`
	}
	out := struct {
		Course    string  `json:"course"`
		Repo      string  `json:"repo"`
		GitDesc   string  `json:"git"`
		NoStopPct float64 `json:"no_stop_grade_pct_stimp10"`
		Greens    []item  `json:"greens"`
	}{
		Course:    s.idx.Course,
		Repo:      s.repo,
		GitDesc:   course.GitDescribe(s.repo),
		NoStopPct: 100 * 7 * physics.DecelFromStimp(10) / (5 * physics.G),
	}
	for _, g := range s.idx.Greens {
		out.Greens = append(out.Greens, item{
			Label: g.Label, Hole: g.Hole, SlopeMeanPct: g.SlopeMeanPct,
			ElevRangeM: g.ElevationRangeM, Flags: g.Flags, NeedsReview: g.NeedsReview,
		})
	}
	writeJSON(w, 200, out)
}

// elevGrid re-lays a heightmap onto geom grids, flipping the row axis: the
// file stores rows north-to-south, geom grids run +Y upward. It returns the
// elevation, the putting-surface mask, and the modeled-support mask (the
// green plus its collar buffer, which the ball is NOT on).
func elevGrid(h *green.Heightmap) (elev, greenMask, support *geom.Grid) {
	rows, cols := h.GridSize()
	dx := h.CellSize()
	x0, y0 := h.Origin()
	mk := func() *geom.Grid {
		return &geom.Grid{
			X0: x0, Y0: y0 - float64(rows-1)*dx, Dx: dx, Dy: dx,
			Nx: cols, Ny: rows, Z: make([]float64, rows*cols),
		}
	}
	elev, greenMask, support = mk(), mk(), mk()
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			iy := rows - 1 - i
			elev.Z[iy*cols+j] = h.ZAt(i, j)
			if h.GreenAt(i, j) {
				greenMask.Z[iy*cols+j] = 1
			}
			if h.ValidAt(i, j) {
				support.Z[iy*cols+j] = 1
			}
		}
	}
	return elev, greenMask, support
}

func (s *server) handleGreen(w http.ResponseWriter, r *http.Request) {
	label := r.PathValue("label")
	g, err := s.loadGreen(label, 10)
	if err != nil {
		httpError(w, 404, "%v", err)
		return
	}
	h := g.Surf
	rows, cols := h.GridSize()
	dx := h.CellSize()
	x0, y0 := h.Origin()
	elev, greenMask, support := elevGrid(h)

	type contour struct {
		ZMM   int            `json:"z_mm"`
		Rings [][][2]float64 `json:"rings"`
	}
	type arrow struct {
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
		DX float64 `json:"dx"`
		DY float64 `json:"dy"`
	}
	type slopeLayer struct {
		Nx int       `json:"nx"`
		Ny int       `json:"ny"`
		X0 float64   `json:"x0"`
		Y0 float64   `json:"y0"`
		D  float64   `json:"d"`
		V  []float64 `json:"v"`
	}
	out := struct {
		Label    string           `json:"label"`
		Meta     course.Meta      `json:"meta"`
		Info     course.GreenInfo `json:"info"`
		Bounds   [4]float64       `json:"bounds"` // minX, minY, maxX, maxY
		Dx       float64          `json:"dx"`
		Outline  [][][2]float64   `json:"outline"` // the putting surface
		Support  [][][2]float64   `json:"support"` // modeled terrain (green + collar)
		Contours []contour        `json:"contours"`
		Slope    slopeLayer       `json:"slope"`
		Arrows   []arrow          `json:"arrows"`
		Area     float64          `json:"area_m2"`
	}{
		Label: label, Meta: g.Meta, Info: g.Info,
		Bounds: [4]float64{x0, y0 - float64(rows-1)*dx, x0 + float64(cols-1)*dx, y0},
		Dx:     dx,
		Area:   g.GreenAreaM2,
	}

	out.Outline = ringsToJSON(geom.Contours(greenMask, 0.5))
	out.Support = ringsToJSON(geom.Contours(support, 0.5))

	// Contour levels bracket the putting surface's own elevation range; the
	// collar falls away steeply and would otherwise add dozens of levels.
	var lo, hi float64 = math.Inf(1), math.Inf(-1)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if !h.GreenAt(i, j) {
				continue
			}
			z := h.ZAt(i, j)
			lo = math.Min(lo, z)
			hi = math.Max(hi, z)
		}
	}
	for z := math.Ceil(lo/elevContourStepM) * elevContourStepM; z <= hi; z += elevContourStepM {
		rings := geom.Contours(elev, z)
		if len(rings) == 0 {
			continue
		}
		out.Contours = append(out.Contours, contour{
			ZMM: int(math.Round(z * 1000)), Rings: ringsToJSON(rings),
		})
	}

	// Slope heat layer, subsampled: the grid is 0.25 m and the display does
	// not need that.
	const heatStride = 2
	sl := &out.Slope
	sl.Nx = (cols + heatStride - 1) / heatStride
	sl.Ny = (rows + heatStride - 1) / heatStride
	sl.D = dx * heatStride
	sl.X0 = x0
	sl.Y0 = y0 - float64(rows-1)*dx
	sl.V = make([]float64, sl.Nx*sl.Ny)
	for iy := 0; iy < sl.Ny; iy++ {
		for ix := 0; ix < sl.Nx; ix++ {
			x := sl.X0 + float64(ix)*sl.D
			y := sl.Y0 + float64(iy)*sl.D
			v := -1.0 // off-green
			if h.OnGreen(x, y) {
				gx, gy := h.Gradient(x, y)
				v = math.Round(1000*math.Hypot(gx, gy)) / 10 // percent, 0.1 resolution
			}
			sl.V[iy*sl.Nx+ix] = v
		}
	}

	// Fall-line arrows on a coarse grid: which way a ball rolls, at a glance.
	for y := out.Bounds[1]; y <= out.Bounds[3]; y += 2 {
		for x := out.Bounds[0]; x <= out.Bounds[2]; x += 2 {
			if !h.OnGreen(x, y) {
				continue
			}
			gx, gy := h.Gradient(x, y)
			m := math.Hypot(gx, gy)
			if m < flatGradient {
				continue
			}
			// Downhill is the negative gradient.
			out.Arrows = append(out.Arrows, arrow{
				X: round3(x), Y: round3(y), DX: round3(-gx / m), DY: round3(-gy / m),
			})
		}
	}
	writeJSON(w, 200, out)
}

func ringsToJSON(rings [][]geom.Pt) [][][2]float64 {
	out := make([][][2]float64, 0, len(rings))
	for _, r := range rings {
		out = append(out, flatten(r))
	}
	return out
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

type runRequest struct {
	Green string                 `json:"green"`
	Stimp float64                `json:"stimp"`
	Pin   struct{ X, Y float64 } `json:"pin"`
	Ring  *struct {
		DistFt float64 `json:"dist_ft"`
		Hours  []int   `json:"hours"`
		Mode   string  `json:"mode"`
	} `json:"ring"`
	Balls      []struct{ X, Y float64 } `json:"balls"`
	Skills     []string                 `json:"skills"`
	Quality    string                   `json:"quality"`
	Lag        float64                  `json:"lag"`
	OffPenalty float64                  `json:"offpenalty"`
	Seed       uint64                   `json:"seed"`
}

func (s *server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, 400, "bad request body: %v", err)
		return
	}
	if req.Stimp <= 0 {
		req.Stimp = 10
	}
	if req.Lag <= 0 {
		req.Lag = 0.25
	}
	if req.OffPenalty == 0 {
		req.OffPenalty = 0.5
	}
	q, ok := qualities[req.Quality]
	if !ok {
		q = qualities["quick"]
	}
	g, err := s.loadGreen(req.Green, req.Stimp)
	if err != nil {
		httpError(w, 404, "%v", err)
		return
	}
	surf := g.Surf.WithDecel(physics.DecelFromStimp(req.Stimp))
	pin := physics.Vec2{X: req.Pin.X, Y: req.Pin.Y}
	if err := validatePin(surf, pin); err != nil {
		httpError(w, 400, "%v", err)
		return
	}

	var skills []player.Skill
	for _, name := range req.Skills {
		sk, ok := player.ProfileByName(name)
		if !ok {
			httpError(w, 400, "unknown skill %q", name)
			return
		}
		skills = append(skills, sk)
	}
	if len(skills) == 0 {
		sk, _ := player.ProfileByName("mid")
		skills = []player.Skill{sk}
	}

	var balls []BallSpec
	var hours []int
	ringFt := 20.0
	mode := "fall"
	switch {
	case len(req.Balls) > 0:
		for _, b := range req.Balls {
			p := physics.Vec2{X: b.X, Y: b.Y}
			spec := BallSpec{Pos: p, Mode: "xy", Status: statusOK, DistFt: p.Sub(pin).Norm() / 0.3048}
			switch {
			case !surf.OnGreen(p.X, p.Y):
				spec.Status = statusOffGreen
			case p.Sub(pin).Norm() < 0.25:
				spec.Status = statusTooClose
			}
			balls = append(balls, spec)
		}
	case req.Ring != nil:
		ringFt = req.Ring.DistFt
		if ringFt <= 0 {
			ringFt = 20
		}
		hours = req.Ring.Hours
		if len(hours) == 0 {
			hours = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
		}
		if req.Ring.Mode == "compass" {
			mode = "compass"
		}
		balls, _ = ringBalls(surf, pin, ringFt*0.3048, hours, mode)
	default:
		httpError(w, 400, "give either a ring or explicit ball positions")
		return
	}
	playable := 0
	for _, b := range balls {
		if b.Status == statusOK {
			playable++
		}
	}
	if playable == 0 {
		httpError(w, 400, "no ball position is playable from this pin (all off the green or too close)")
		return
	}

	seed := req.Seed
	if seed == 0 {
		seed = uint64(time.Now().UnixNano())
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		httpError(w, 409, "a simulation is already running")
		return
	}
	s.running = true
	s.nextID++
	id := fmt.Sprintf("j%d", s.nextID)
	job := &jobState{ID: id, Seed: seed, State: "running", Phase: "starting"}
	job.spec = RunSpec{
		Green: g, GreensRepo: s.repo, Stimp: req.Stimp, Pin: pin, Balls: balls,
		Skills: skills, Rollouts: standardRollouts(),
		Trials: q.Trials, FieldNodes: q.FieldTrials, FieldSweep: q.FieldSweeps,
		Lag: req.Lag, OffPenalty: req.OffPenalty, Seed: seed,
		RingFt: ringFt, Hours: hours, ClockMode: mode,
	}
	s.jobs[id] = job
	s.order = append(s.order, id)
	for len(s.order) > 8 {
		delete(s.jobs, s.order[0])
		s.order = s.order[1:]
	}
	s.mu.Unlock()

	go s.runJob(job)
	writeJSON(w, 202, map[string]any{"job": id, "seed": seed})
}

func (s *server) runJob(job *jobState) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	res, err := runGreen(job.spec, s.fields, func(phase string, done, total int) {
		job.mu.Lock()
		job.Phase, job.Done, job.Total = phase, done, total
		job.mu.Unlock()
	})
	job.mu.Lock()
	defer job.mu.Unlock()
	if err != nil {
		job.State, job.Err = "error", err.Error()
		return
	}
	job.res = res
	job.State = "done"
}

func (s *server) job(id string) *jobState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (s *server) handleJob(w http.ResponseWriter, r *http.Request) {
	job := s.job(r.PathValue("id"))
	if job == nil {
		httpError(w, 404, "no such job")
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	out := map[string]any{
		"id": job.ID, "seed": job.Seed, "state": job.State,
		"phase": job.Phase, "done": job.Done, "total": job.Total,
	}
	if job.Err != "" {
		out["error"] = job.Err
	}
	if job.State == "done" {
		out["result"] = jobResult(job)
	}
	writeJSON(w, 200, out)
}

type curvePoint struct {
	Ro  float64 `json:"ro"`
	E   float64 `json:"e"`
	DE  float64 `json:"de"`
	DSE float64 `json:"dse"`
}

// jobResult reshapes the row-per-rollout run into what the UI draws: one
// entry per ball and skill, carrying the optimal pace and the curve behind it.
func jobResult(job *jobState) map[string]any {
	spec, res := job.spec, job.res

	balls := make([]map[string]any, len(spec.Balls))
	for i, b := range spec.Balls {
		balls[i] = map[string]any{
			"idx": i, "hour": b.Hour, "x": round3(b.Pos.X), "y": round3(b.Pos.Y),
			"dist_ft": math.Round(b.DistFt*10) / 10, "status": b.Status, "mode": b.Mode,
		}
	}

	type key struct {
		ball  int
		skill string
	}
	curves := map[key][]curvePoint{}
	stats := map[key]GreenRow{}
	for _, r := range res.Rows {
		if r.Ball.Status != statusOK || !r.Res.SolveOK {
			continue
		}
		k := key{r.BallIdx, r.Skill}
		curves[k] = append(curves[k], curvePoint{
			Ro: r.Rollout, E: round3(r.Res.EStrokes),
			DE: math.Round(r.DE*1e5) / 1e5, DSE: math.Round(r.DSE*1e5) / 1e5,
		})
		// ΔE is zero exactly at the group's best rollout.
		if r.DE == 0 {
			stats[k] = r
		}
	}

	var cells []map[string]any
	for _, sk := range spec.Skills {
		for bi := range spec.Balls {
			k := key{bi, sk.Name}
			best, ok := stats[k]
			if !ok {
				continue
			}
			cells = append(cells, map[string]any{
				"ball": bi, "skill": sk.Name,
				"rollout_m":  best.Rollout,
				"rollout_in": math.Round(best.Rollout * 39.3701),
				"make":       math.Round(1000*best.Res.Make) / 10,
				"three_plus": math.Round(1000*best.Res.ThreePlus) / 10,
				"e_strokes":  round3(best.Res.EStrokes),
				"e_se":       math.Round(best.Res.EStrokesSE*1e4) / 1e4,
				"off_green":  math.Round(1000*best.Res.OffGreen) / 10,
				"past_in":    math.Round(best.Res.MeanPastMiss * 39.3701),
				"short_pct":  math.Round(100 * best.Res.PctMissShort),
				"curve":      curves[k],
			})
		}
	}

	return map[string]any{
		"green":            spec.Green.Info.Label,
		"pin":              []float64{round3(spec.Pin.X), round3(spec.Pin.Y)},
		"stimp":            spec.Stimp,
		"slope_at_pin_pct": math.Round(res.SlopeAtPinPct*10) / 10,
		"fall_azimuth_deg": math.Round(res.FallAzimuthDeg),
		"compass_fallback": res.CompassFallback,
		"runaway":          res.Runaway,
		"trials":           spec.Trials,
		"balls":            balls,
		"cells":            cells,
		"dispersion":       res.Dispersion,
		"reproduce":        reproduceCmd(spec),
	}
}

// reproduceCmd is the greensweep invocation that reruns exactly this
// configuration from the command line.
func reproduceCmd(spec RunSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "go run ./cmd/putttron greensweep -greens %s -green %s -pin %q",
		spec.GreensRepo, spec.Green.Info.Label,
		fmt.Sprintf("%.3f,%.3f", spec.Pin.X, spec.Pin.Y))
	if len(spec.Hours) > 0 {
		hours := make([]string, len(spec.Hours))
		for i, h := range spec.Hours {
			hours[i] = fmt.Sprint(h)
		}
		fmt.Fprintf(&b, " -ringft %g -hours %s -clock %s", spec.RingFt, strings.Join(hours, ","), spec.ClockMode)
	} else {
		var pts []string
		for _, ball := range spec.Balls {
			pts = append(pts, fmt.Sprintf("%.3f,%.3f", ball.Pos.X, ball.Pos.Y))
		}
		fmt.Fprintf(&b, " -balls %q", strings.Join(pts, ";"))
	}
	names := make([]string, len(spec.Skills))
	for i, sk := range spec.Skills {
		names[i] = sk.Name
	}
	fmt.Fprintf(&b, " -skills %s -stimp %g -trials %d -fieldtrials %d -fieldsweeps %d -seed %d",
		strings.Join(names, ","), spec.Stimp, spec.Trials, spec.FieldNodes, spec.FieldSweep, spec.Seed)
	return b.String()
}

func (s *server) finishedJob(w http.ResponseWriter, r *http.Request) *jobState {
	job := s.job(r.PathValue("id"))
	if job == nil {
		httpError(w, 404, "no such job")
		return nil
	}
	job.mu.Lock()
	state := job.State
	job.mu.Unlock()
	if state != "done" {
		httpError(w, 409, "job is %s", state)
		return nil
	}
	return job
}

func (s *server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	job := s.finishedJob(w, r)
	if job == nil {
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%s-%s.csv", job.spec.Green.Info.Label, job.ID))
	fmt.Fprint(w, greenCSV(job.spec, job.res))
}

func (s *server) handleExportManifest(w http.ResponseWriter, r *http.Request) {
	job := s.finishedJob(w, r)
	if job == nil {
		return
	}
	w.Header().Set("Content-Type", "text/yaml")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%s-%s.manifest.yaml", job.spec.Green.Info.Label, job.ID))
	fmt.Fprint(w, greenManifest(job.spec, job.res))
}
