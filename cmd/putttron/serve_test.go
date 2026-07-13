package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jhoblitt/putttron/internal/course"
	"github.com/jhoblitt/putttron/internal/physics"
)

func vec(x, y float64) physics.Vec2 { return physics.Vec2{X: x, Y: y} }

func loadFixtureGreen(t *testing.T, repo, label string) (*course.Index, *course.Green) {
	t.Helper()
	idx, err := course.LoadIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	g, err := course.LoadGreen(idx, label, physics.DecelFromStimp(10))
	if err != nil {
		t.Fatal(err)
	}
	return idx, g
}

func testServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	repo := fixtureCourse(t)
	idx, err := course.LoadIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(repo, idx)
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts
}

func getJSON(t *testing.T, ts *httptest.Server, path string, into any) int {
	t.Helper()
	res, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if into != nil {
		if err := json.NewDecoder(res.Body).Decode(into); err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
	}
	return res.StatusCode
}

func TestServeGreensAndGreen(t *testing.T) {
	_, ts := testServer(t)

	var list struct {
		Course string `json:"course"`
		Greens []struct {
			Label       string   `json:"label"`
			Flags       []string `json:"flags"`
			NeedsReview bool     `json:"needs_review"`
		} `json:"greens"`
	}
	if code := getJSON(t, ts, "/api/greens", &list); code != 200 {
		t.Fatalf("GET /api/greens = %d", code)
	}
	if len(list.Greens) != 2 {
		t.Fatalf("listed %d greens, want 2", len(list.Greens))
	}
	if !list.Greens[1].NeedsReview || len(list.Greens[1].Flags) == 0 {
		t.Error("the flagged green's review status did not survive the API")
	}

	var g struct {
		Label    string           `json:"label"`
		Bounds   [4]float64       `json:"bounds"`
		Outline  [][][2]float64   `json:"outline"`
		Support  [][][2]float64   `json:"support"`
		Contours []struct{}       `json:"contours"`
		Arrows   []struct{}       `json:"arrows"`
		Area     float64          `json:"area_m2"`
		Slope    struct{ Nx int } `json:"slope"`
	}
	if code := getJSON(t, ts, "/api/green/hole_01", &g); code != 200 {
		t.Fatalf("GET /api/green/hole_01 = %d", code)
	}
	if len(g.Outline) == 0 || len(g.Outline[0]) < 8 {
		t.Fatal("green outline is missing or degenerate")
	}
	if len(g.Support) == 0 {
		t.Fatal("modeled-support outline is missing")
	}
	// The support ring is the collar: it must enclose the green's ring.
	greenR, supportR := ringRadius(g.Outline[0]), ringRadius(g.Support[0])
	if supportR < greenR+8 {
		t.Errorf("support radius %.1f m barely exceeds the green's %.1f m — the collar is not being distinguished",
			supportR, greenR)
	}
	if want := 3.14159 * 100; g.Area < 0.8*want || g.Area > 1.25*want {
		t.Errorf("putting surface %.0f m², want about %.0f", g.Area, want)
	}
	if len(g.Contours) == 0 || len(g.Arrows) == 0 || g.Slope.Nx == 0 {
		t.Error("the green map is missing contours, fall lines, or the slope layer")
	}

	if code := getJSON(t, ts, "/api/green/nope", nil); code != 404 {
		t.Errorf("unknown green = %d, want 404", code)
	}
}

func ringRadius(ring [][2]float64) float64 {
	var maxR float64
	for _, p := range ring {
		maxR = math.Max(maxR, math.Hypot(p[0], p[1]))
	}
	return maxR
}

func post(t *testing.T, ts *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(ts.URL+"/api/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestServeRunLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end Monte Carlo")
	}
	_, ts := testServer(t)

	code, out := post(t, ts, `{"green":"hole_01","stimp":10,"pin":{"x":0,"y":0},
		"ring":{"dist_ft":15,"hours":[12,6],"mode":"fall"},"skills":["mid"],
		"quality":"quick","seed":7}`)
	if code != 202 {
		t.Fatalf("POST /api/run = %d (%v)", code, out)
	}
	id, _ := out["job"].(string)
	if id == "" {
		t.Fatal("no job id returned")
	}

	var job map[string]any
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		job = nil
		getJSON(t, ts, "/api/job/"+id, &job)
		if job["state"] == "done" || job["state"] == "error" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job["state"] != "done" {
		t.Fatalf("job did not finish: %v", job)
	}
	res, _ := job["result"].(map[string]any)
	if res == nil {
		t.Fatal("finished job carries no result")
	}
	cells, _ := res["cells"].([]any)
	if len(cells) != 2 {
		t.Fatalf("got %d result cells, want 2 (one per ring position)", len(cells))
	}
	first, _ := cells[0].(map[string]any)
	for _, key := range []string{"make", "three_plus", "e_strokes", "rollout_in", "curve", "off_green"} {
		if _, ok := first[key]; !ok {
			t.Errorf("result cell is missing %q", key)
		}
	}
	if curve, _ := first["curve"].([]any); len(curve) != 13 {
		t.Errorf("rollout curve has %d points, want 13", len(curve))
	}
	disp, _ := res["dispersion"].([]any)
	if len(disp) != 2 {
		t.Errorf("got %d dispersion clouds, want 2", len(disp))
	}
	if repro, _ := res["reproduce"].(string); !strings.Contains(repro, "greensweep") ||
		!strings.Contains(repro, "-seed 7") {
		t.Errorf("reproduce command does not rebuild this run: %q", repro)
	}

	// The exports must be the same bytes the batch command writes.
	res2, err := http.Get(ts.URL + "/api/job/" + id + "/export.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(res2.Body)
	csv := buf.String()
	if !strings.HasPrefix(csv, "green,pin_x_m") || strings.Count(csv, "\n") != 1+2*13 {
		t.Errorf("exported CSV does not match the batch schema:\n%.120s", csv)
	}
	if code := getJSON(t, ts, "/api/job/"+id+"/manifest.yaml", nil); code != 200 {
		t.Errorf("manifest export = %d", code)
	}
	if code := getJSON(t, ts, "/api/job/nope", nil); code != 404 {
		t.Errorf("unknown job = %d, want 404", code)
	}
}

func TestServeRejectsBadRuns(t *testing.T) {
	_, ts := testServer(t)
	for _, tc := range []struct {
		name string
		body string
		code int
	}{
		{"pin in the collar", `{"green":"hole_01","pin":{"x":15,"y":0},
			"ring":{"dist_ft":10,"hours":[12]},"skills":["mid"]}`, 400},
		{"unknown green", `{"green":"nope","pin":{"x":0,"y":0},
			"ring":{"dist_ft":10,"hours":[12]},"skills":["mid"]}`, 404},
		{"unknown skill", `{"green":"hole_01","pin":{"x":0,"y":0},
			"ring":{"dist_ft":10,"hours":[12]},"skills":["hustler"]}`, 400},
		{"no ball positions", `{"green":"hole_01","pin":{"x":0,"y":0},"skills":["mid"]}`, 400},
		{"ring entirely off the green", `{"green":"hole_01","pin":{"x":0,"y":0},
			"ring":{"dist_ft":90,"hours":[12,3,6,9]},"skills":["mid"]}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := post(t, ts, tc.body)
			if code != tc.code {
				t.Errorf("status %d, want %d (%v)", code, tc.code, out)
			}
			if _, ok := out["error"]; !ok {
				t.Error("rejection carries no reason")
			}
		})
	}
}

// Ring positions that fall off the green are reported, not dropped: a putt
// from there is not a putt.
func TestServeRingOffGreen(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end Monte Carlo")
	}
	_, ts := testServer(t)
	// A pin near the edge of a 10 m green with a 25 ft ring: some hours land
	// out in the collar.
	code, out := post(t, ts, `{"green":"hole_01","stimp":10,"pin":{"x":5,"y":0},
		"ring":{"dist_ft":25,"hours":[12,3,6,9],"mode":"compass"},"skills":["mid"],
		"quality":"quick","seed":3}`)
	if code != 202 {
		t.Fatalf("POST = %d (%v)", code, out)
	}
	id := out["job"].(string)
	var job map[string]any
	for i := 0; i < 900; i++ {
		job = nil
		getJSON(t, ts, "/api/job/"+id, &job)
		if job["state"] == "done" || job["state"] == "error" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job["state"] != "done" {
		t.Fatalf("job did not finish: %v", job)
	}
	res := job["result"].(map[string]any)
	balls := res["balls"].([]any)
	skipped := 0
	for _, b := range balls {
		if b.(map[string]any)["status"] != statusOK {
			skipped++
		}
	}
	if skipped == 0 {
		t.Error("a 25 ft ring around an edge pin put every position on the green")
	}
	if len(balls) != 4 {
		t.Errorf("%d ball positions reported, want all 4 hours accounted for", len(balls))
	}
}
