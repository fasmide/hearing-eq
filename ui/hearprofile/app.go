package hearprofile

import (
	"fmt"
	"image"
	"image/color"
	"errors"
	"math"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"hearing-eq/internal/profile"
	"hearing-eq/internal/staircase"
	"hearing-eq/internal/tone"
)

type Screen int

const (
	ScreenWelcome Screen = iota
	ScreenCalibration
	ScreenTest
	ScreenResults
)

type TestPoint struct {
	FrequencyHz float64
	Ear         tone.Ear
}

type AudioPlayer interface {
	PlayBuffer(samples []float32, mediaName string) error
	StartCalibrationTone(freqHz, dbfs float64) error
	StopCalibrationTone() error
}

type App struct {
	theme *material.Theme
	w     *app.Window
	ops   op.Ops

	audio AudioPlayer
	rand  *rand.Rand

	screen Screen
	err    error

	continueBtn widget.Clickable
	saveBtn     widget.Clickable
	hearBtn     widget.Clickable
	abortBtn    widget.Clickable
	backBtn     widget.Clickable

	testPlan []TestPoint
	current  int
	active   *staircase.Staircase
	left     []float64
	right    []float64
	loadedAt time.Time
	hasSaved bool

	runningTest    bool
	responseOpen   bool
	waitingResult  bool
	responded      bool
	currentLevel   float64
	currentFreqHz  float64
	currentEar     tone.Ear
	nextToneAt     time.Time
	windowDeadline time.Time
	progressText   string

	mu         sync.Mutex
	invalidate func()
}

func New(w *app.Window, audio AudioPlayer) *App {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	plan := make([]TestPoint, 0, len(profile.DefaultFrequenciesHz)*2)
	for _, freq := range profile.DefaultFrequenciesHz {
		plan = append(plan, TestPoint{FrequencyHz: freq, Ear: tone.Left}, TestPoint{FrequencyHz: freq, Ear: tone.Right})
	}
	a := &App{
		theme:    material.NewTheme(),
		w:        w,
		audio:    audio,
		rand:     r,
		screen:   ScreenWelcome,
		testPlan: plan,
		left:     make([]float64, len(profile.DefaultFrequenciesHz)),
		right:    make([]float64, len(profile.DefaultFrequenciesHz)),
	}
	a.loadSavedProfile()
	return a
}

func (a *App) Run() error {
	for {
		e := a.w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			_ = a.audio.StopCalibrationTone()
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&a.ops, e)
			a.invalidate = a.w.Invalidate
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) layout(gtx layout.Context) {
	for a.continueBtn.Clicked(gtx) {
		a.onContinue()
	}
	for a.saveBtn.Clicked(gtx) {
		a.onSave()
	}
	for a.hearBtn.Clicked(gtx) {
		a.onHeard()
	}
	for a.abortBtn.Clicked(gtx) {
		a.stopTest("test aborted")
	}
	for a.backBtn.Clicked(gtx) {
		a.screen = ScreenWelcome
		a.err = nil
	}

	if a.screen == ScreenTest {
		gtx.Execute(key.FocusCmd{Tag: &a.hearBtn})
		a.advanceTestClock(time.Now())
	}

	inset := layout.UniformInset(unit.Dp(20))
	inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceStart}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.H4(a.theme, "Personalized Hearing EQ")
				return label.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				switch a.screen {
				case ScreenWelcome:
					return a.layoutWelcome(gtx)
				case ScreenCalibration:
					return a.layoutCalibration(gtx)
				case ScreenTest:
					return a.layoutTest(gtx)
				case ScreenResults:
					return a.layoutResults(gtx)
				default:
					return layout.Dimensions{}
				}
			}),
		)
	})
}

func (a *App) layoutWelcome(gtx layout.Context) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(label(a.theme, "Put on your headphones, sit in a quiet room, set system volume to a comfortable level, and do not change it during or after the test.")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
	}
	if a.hasSaved {
		children = append(children,
			layout.Rigid(label(a.theme, fmt.Sprintf("Current saved profile: %s", a.loadedAt.Format("2006-01-02 15:04:05Z07:00")))),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutAudiogram(gtx, a.left, a.right)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		)
	} else {
		children = append(children,
			layout.Rigid(label(a.theme, "No saved profile found yet.")),
			layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		)
	}
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.continueBtn, "Start Calibration")
			return btn.Layout(gtx)
		}),
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (a *App) layoutCalibration(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(label(a.theme, "A steady 1 kHz tone at -25 dBFS is playing in both ears. Adjust system volume until it feels comfortable but not loud, then continue.")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.continueBtn, "Continue To Test")
			return btn.Layout(gtx)
		}),
	)
}

func (a *App) layoutTest(gtx layout.Context) layout.Dimensions {
	progress := float32(a.current) / float32(len(a.testPlan))
	ear := "Left"
	if a.currentEar == tone.Right {
		ear = "Right"
	}
	button := material.Button(a.theme, &a.hearBtn, "I HEAR IT")
	button.TextSize = unit.Sp(28)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(label(a.theme, fmt.Sprintf("Band %d of %d", a.current+1, len(a.testPlan)))),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(label(a.theme, fmt.Sprintf("Frequency: %.0f Hz", a.currentFreqHz))),
		layout.Rigid(label(a.theme, fmt.Sprintf("Ear: %s", ear))),
		layout.Rigid(label(a.theme, fmt.Sprintf("Level: %.1f dBFS", a.currentLevel))),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			bar := material.ProgressBar(a.theme, progress)
			return bar.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Pt(gtx.Dp(unit.Dp(320)), gtx.Dp(unit.Dp(120)))
			return button.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(label(a.theme, "Press Space or click the button any time during the response window.")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.abortBtn, "Abort")
			return btn.Layout(gtx)
		}),
	)
}

func (a *App) layoutResults(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(label(a.theme, "Relative thresholds are shown below. Lower, more negative values indicate better hearing at that band.")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutAudiogram(gtx, a.left, a.right)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.saveBtn, "Save Profile")
			return btn.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.backBtn, "Back To Start")
			return btn.Layout(gtx)
		}),
	)
}

func (a *App) onContinue() {
	switch a.screen {
	case ScreenWelcome:
		if err := a.audio.StartCalibrationTone(1000, -25); err != nil {
			a.err = fmt.Errorf("start calibration tone: %w", err)
			return
		}
		a.screen = ScreenCalibration
		a.err = nil
	case ScreenCalibration:
		if err := a.audio.StopCalibrationTone(); err != nil {
			a.err = fmt.Errorf("stop calibration tone: %w", err)
			return
		}
		a.err = nil
		a.startTest()
	}
}

func (a *App) onSave() {
	p, err := profile.New(a.left, a.right)
	if err != nil {
		a.err = fmt.Errorf("build profile: %w", err)
		return
	}
	if err := profile.SaveDefault(p); err != nil {
		a.err = fmt.Errorf("save profile: %w", err)
		return
	}
	a.loadedAt = p.CreatedAt
	a.hasSaved = true
	a.err = fmt.Errorf("saved profile to ~/.config/hearing-eq/profile.json")
}

func (a *App) startTest() {
	a.screen = ScreenTest
	a.current = 0
	a.runningTest = true
	a.left = make([]float64, len(profile.DefaultFrequenciesHz))
	a.right = make([]float64, len(profile.DefaultFrequenciesHz))
	a.startCurrentBand()
}

func (a *App) loadSavedProfile() {
	p, err := profile.LoadDefault()
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return
		}
		return
	}
	copy(a.left, p.LeftThresholdsDBFS)
	copy(a.right, p.RightThresholdsDBFS)
	a.loadedAt = p.CreatedAt
	a.hasSaved = true
}

func (a *App) stopTest(message string) {
	a.runningTest = false
	a.screen = ScreenWelcome
	a.err = fmt.Errorf("%s", message)
	_ = a.audio.StopCalibrationTone()
}

func (a *App) startCurrentBand() {
	if a.current >= len(a.testPlan) {
		a.runningTest = false
		a.screen = ScreenResults
		return
	}
	point := a.testPlan[a.current]
	a.active = staircase.New()
	a.currentFreqHz = point.FrequencyHz
	a.currentEar = point.Ear
	a.currentLevel = a.active.Level()
	a.responseOpen = false
	a.waitingResult = false
	a.responded = false
	a.nextToneAt = time.Now()
	a.windowDeadline = time.Now()
	if a.invalidate != nil {
		a.invalidate()
	}
}

func (a *App) onHeard() {
	if a.screen != ScreenTest || !a.responseOpen || a.responded {
		return
	}
	a.responded = true
	if err := a.applyResponse(true); err != nil {
		a.err = err
	}
}

func (a *App) advanceTestClock(now time.Time) {
	if !a.runningTest || a.active == nil {
		return
	}
	if !a.responseOpen && now.After(a.nextToneAt) {
		if err := a.playCurrentTone(); err != nil {
			a.err = err
			a.stopTest("audio playback failed")
			return
		}
		gap := time.Duration(1500+a.rand.Intn(1501)) * time.Millisecond
		a.responseOpen = true
		a.responded = false
		a.waitingResult = true
		a.nextToneAt = now.Add(gap)
		a.windowDeadline = a.nextToneAt.Add(200 * time.Millisecond)
	}
	if a.responseOpen && !a.responded && now.After(a.windowDeadline) {
		if err := a.applyResponse(false); err != nil {
			a.err = err
		}
	}
	if a.runningTest && a.invalidate != nil {
		time.AfterFunc(30*time.Millisecond, a.invalidate)
	}
}

func (a *App) playCurrentTone() error {
	samples := tone.BurstStereo(a.currentFreqHz, a.currentLevel, a.currentEar, tone.DefaultSampleRate)
	return a.audio.PlayBuffer(samples, fmt.Sprintf("Hearing Test %.0f Hz", a.currentFreqHz))
}

func (a *App) applyResponse(heard bool) error {
	a.responseOpen = false
	a.waitingResult = false
	if err := a.active.Apply(heard); err != nil {
		return fmt.Errorf("apply staircase response: %w", err)
	}
	a.currentLevel = a.active.Level()
	if !a.active.Done() {
		return nil
	}
	result, err := a.active.Result()
	if err != nil {
		return fmt.Errorf("finalize staircase: %w", err)
	}
	idx := frequencyIndex(a.currentFreqHz)
	if idx < 0 {
		return fmt.Errorf("unknown frequency %.0f", a.currentFreqHz)
	}
	if a.currentEar == tone.Left {
		a.left[idx] = result.ThresholdDBFS
	} else {
		a.right[idx] = result.ThresholdDBFS
	}
	a.current++
	a.startCurrentBand()
	return nil
}

func label(th *material.Theme, text string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Body1(th, text)
		return l.Layout(gtx)
	}
}

func frequencyIndex(freq float64) int {
	for i, f := range profile.DefaultFrequenciesHz {
		if f == freq {
			return i
		}
	}
	return -1
}

func layoutAudiogram(gtx layout.Context, left, right []float64) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(260)))
	if size.X <= 0 {
		size.X = gtx.Dp(unit.Dp(480))
	}
	rect := image.Rectangle{Max: size}
	paint.FillShape(gtx.Ops, color.NRGBA{R: 20, G: 22, B: 26, A: 255}, clip.Rect(rect).Op())
	drawGrid(gtx, rect)
	drawSeries(gtx, rect, left, color.NRGBA{R: 100, G: 181, B: 246, A: 255})
	drawSeries(gtx, rect, right, color.NRGBA{R: 239, G: 83, B: 80, A: 255})
	return layout.Dimensions{Size: size}
}

func drawGrid(gtx layout.Context, rect image.Rectangle) {
	for _, f := range profile.DefaultFrequenciesHz {
		x := mapX(f, rect)
		path := clip.Path{}
		path.Begin(gtx.Ops)
		path.MoveTo(f32.Pt(float32(x), float32(rect.Min.Y)))
		path.LineTo(f32.Pt(float32(x), float32(rect.Max.Y)))
		paint.FillShape(gtx.Ops, color.NRGBA{R: 60, G: 64, B: 72, A: 255}, clip.Stroke{Path: path.End(), Width: 1}.Op())
	}
	for db := -60.0; db <= -20; db += 10 {
		y := mapY(db, rect)
		path := clip.Path{}
		path.Begin(gtx.Ops)
		path.MoveTo(f32.Pt(float32(rect.Min.X), float32(y)))
		path.LineTo(f32.Pt(float32(rect.Max.X), float32(y)))
		paint.FillShape(gtx.Ops, color.NRGBA{R: 60, G: 64, B: 72, A: 255}, clip.Stroke{Path: path.End(), Width: 1}.Op())
	}
}

func drawSeries(gtx layout.Context, rect image.Rectangle, values []float64, c color.NRGBA) {
	if len(values) == 0 {
		return
	}
	path := clip.Path{}
	path.Begin(gtx.Ops)
	points := make([]f32.Point, 0, len(values))
	for i, v := range values {
		if v == 0 {
			continue
		}
		p := f32.Pt(float32(mapX(profile.DefaultFrequenciesHz[i], rect)), float32(mapY(v, rect)))
		points = append(points, p)
	}
	if len(points) == 0 {
		return
	}
	path.MoveTo(points[0])
	for _, p := range points[1:] {
		path.LineTo(p)
	}
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: path.End(), Width: 2}.Op())
	for _, p := range points {
		r := image.Rect(int(p.X)-3, int(p.Y)-3, int(p.X)+3, int(p.Y)+3)
		paint.FillShape(gtx.Ops, c, clip.Ellipse(r).Op(gtx.Ops))
	}
}

func mapX(freq float64, rect image.Rectangle) int {
	minF := math.Log10(profile.DefaultFrequenciesHz[0])
	maxF := math.Log10(profile.DefaultFrequenciesHz[len(profile.DefaultFrequenciesHz)-1])
	x := (math.Log10(freq) - minF) / (maxF - minF)
	return rect.Min.X + int(x*float64(rect.Dx()-20)) + 10
}

func mapY(db float64, rect image.Rectangle) int {
	minDB := -60.0
	maxDB := -20.0
	clamped := math.Max(minDB, math.Min(maxDB, db))
	y := (clamped - minDB) / (maxDB - minDB)
	return rect.Max.Y - int(y*float64(rect.Dy()-20)) - 10
}

func SortedThresholds(values []float64) []float64 {
	out := append([]float64(nil), values...)
	sort.Float64s(out)
	return out
}
