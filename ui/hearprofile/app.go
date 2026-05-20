package hearprofile

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
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

	"hearing-eq/internal/engine"
	"hearing-eq/internal/profile"
	"hearing-eq/internal/spectrum"
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
	eng   *engine.Engine
	rand  *rand.Rand

	screen Screen
	err    error

	continueBtn    widget.Clickable
	saveBtn        widget.Clickable
	hearBtn        widget.Clickable
	abortBtn       widget.Clickable
	backBtn        widget.Clickable
	selectBtns     []widget.Clickable
	renameBtns     []widget.Clickable
	deleteBtns     []widget.Clickable
	renameSaveBtns []widget.Clickable
	saveAsBtn      widget.Clickable
	nameEditor     widget.Editor
	renameEditor   widget.Editor

	testPlan        []TestPoint
	current         int
	active          *staircase.Staircase
	left            []float64
	right           []float64
	selectedProfile string
	profiles        []profile.NamedProfile
	loadedAt        time.Time
	hasSaved        bool
	renamingProfile string

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
	lastFrameAt    time.Time
	uiFPS          float64

	mu         sync.Mutex
	invalidate func()
}

func New(w *app.Window, audio AudioPlayer, eng *engine.Engine) *App {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	plan := make([]TestPoint, 0, len(profile.DefaultFrequenciesHz)*2)
	for _, freq := range profile.DefaultFrequenciesHz {
		plan = append(plan, TestPoint{FrequencyHz: freq, Ear: tone.Left}, TestPoint{FrequencyHz: freq, Ear: tone.Right})
	}
	a := &App{
		theme:    newDarkTheme(),
		w:        w,
		audio:    audio,
		eng:      eng,
		rand:     r,
		screen:   ScreenWelcome,
		testPlan: plan,
		left:     make([]float64, len(profile.DefaultFrequenciesHz)),
		right:    make([]float64, len(profile.DefaultFrequenciesHz)),
	}
	a.nameEditor.SingleLine = true
	a.renameEditor.SingleLine = true
	a.loadSavedProfile()
	a.refreshProfiles()
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
			a.noteFrame(time.Now())
			gtx := app.NewContext(&a.ops, e)
			a.invalidate = a.w.Invalidate
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) noteFrame(now time.Time) {
	if !a.lastFrameAt.IsZero() {
		dt := now.Sub(a.lastFrameAt).Seconds()
		if dt > 0 {
			instant := 1 / dt
			if a.uiFPS == 0 {
				a.uiFPS = instant
			} else {
				a.uiFPS = a.uiFPS*0.85 + instant*0.15
			}
		}
	}
	a.lastFrameAt = now
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
	for i := range a.selectBtns {
		if a.selectBtns[i].Clicked(gtx) {
			a.onSelectProfile(i)
		}
	}
	for i := range a.renameBtns {
		if a.renameBtns[i].Clicked(gtx) {
			a.onStartRename(i)
		}
	}
	for i := range a.deleteBtns {
		if a.deleteBtns[i].Clicked(gtx) {
			a.onDeleteProfile(i)
		}
	}
	for i := range a.renameSaveBtns {
		if a.renameSaveBtns[i].Clicked(gtx) {
			a.onConfirmRename(i)
		}
	}
	for a.saveAsBtn.Clicked(gtx) {
		a.onSaveAsCurrent()
	}

	if a.screen == ScreenTest {
		gtx.Execute(key.FocusCmd{Tag: &a.hearBtn})
		a.advanceTestClock(time.Now())
	}
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(13 * time.Millisecond)})
	paint.FillShape(gtx.Ops, a.theme.Palette.Bg, clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Op())

	inset := layout.UniformInset(unit.Dp(20))
	inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceStart}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return a.layoutEngineBadge(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if a.err == nil {
					return layout.Dimensions{}
				}
				msg := material.Body1(a.theme, a.err.Error())
				msg.Color = color.NRGBA{R: 255, G: 193, B: 7, A: 255}
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, msg.Layout)
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

func (a *App) layoutEngineBadge(gtx layout.Context) layout.Dimensions {
	labelText := "EQ Idle"
	badgeColor := color.NRGBA{R: 120, G: 124, B: 132, A: 255}
	fftText := "FFT -- Hz"
	audioText := "Audio -- buf/s"
	uiText := "UI -- fps"
	if a.eng != nil {
		snap := a.eng.Spectrum()
		if snap.UpdatesPerSecond > 0 {
			fftText = fmt.Sprintf("FFT %.0f Hz", snap.UpdatesPerSecond)
		}
		stats := a.eng.AudioStats()
		if stats.BuffersPerSecond > 0 {
			audioText = fmt.Sprintf("Audio %.0f buf/s %.0f fr/s", stats.BuffersPerSecond, stats.FramesPerSecond)
		}
	}
	if a.uiFPS > 0 {
		uiText = fmt.Sprintf("UI %.0f fps", a.uiFPS)
	}
	if a.eng != nil && a.eng.Running() {
		labelText = "EQ Running"
		badgeColor = color.NRGBA{R: 76, G: 175, B: 80, A: 255}
	}
	return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceStart}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutBadge(gtx, labelText, badgeColor)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutBadge(gtx, fftText, color.NRGBA{R: 63, G: 81, B: 181, A: 255})
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutBadge(gtx, audioText, color.NRGBA{R: 0, G: 137, B: 123, A: 255})
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutBadge(gtx, uiText, color.NRGBA{R: 109, G: 76, B: 65, A: 255})
		}),
	)
}

func (a *App) layoutBadge(gtx layout.Context, text string, bgColor color.NRGBA) layout.Dimensions {
	inset := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			rect := image.Rectangle{Max: gtx.Constraints.Min}
			paint.FillShape(gtx.Ops, bgColor, clip.UniformRRect(rect, 10).Op(gtx.Ops))
			return layout.Dimensions{Size: rect.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(13), text)
				lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				return lbl.Layout(gtx)
			})
		}),
	)
}

func newDarkTheme() *material.Theme {
	th := material.NewTheme()
	th.Palette = material.Palette{
		Bg:         color.NRGBA{R: 12, G: 14, B: 18, A: 255},
		Fg:         color.NRGBA{R: 232, G: 236, B: 241, A: 255},
		ContrastBg: color.NRGBA{R: 45, G: 96, B: 247, A: 255},
		ContrastFg: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	}
	th.TextSize = unit.Sp(15)
	return th
}

func (a *App) layoutWelcome(gtx layout.Context) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(label(a.theme, "Choose a profile or run a new hearing test.")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
	}
	if a.hasSaved {
		currentLabel := "Current saved profile"
		if a.selectedProfile != "" {
			currentLabel = fmt.Sprintf("Current saved profile: %s", a.selectedProfile)
		}
		children = append(children,
			layout.Rigid(label(a.theme, currentLabel)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
			layout.Rigid(label(a.theme, fmt.Sprintf("Saved: %s", a.loadedAt.Format("2006-01-02 15:04:05Z07:00")))),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
		)
	} else {
		children = append(children,
			layout.Rigid(label(a.theme, "No saved profile found yet.")),
			layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		)
	}
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutProfiles(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !a.hasSaved || a.selectedProfile != "" {
				return layout.Dimensions{}
			}
			return layoutLeftColumn(gtx, 560, label(a.theme, "Legacy unnamed profile: save it with a name to manage it here."))
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !a.hasSaved || a.selectedProfile != "" {
				return layout.Dimensions{}
			}
			return layoutLeftColumn(gtx, 560, a.layoutSaveAs)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.continueBtn, "Start Calibration")
			return btn.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !a.hasSaved {
				return layout.Dimensions{}
			}
			return layoutAudiogram(gtx, a.theme, a.left, a.right)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutSpectrum(gtx)
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
			if a.selectedProfile == "" {
				return label(a.theme, "Save as a new named profile, or Save Profile to update the legacy default profile.")(gtx)
			}
			return label(a.theme, fmt.Sprintf("Selected profile: %s", a.selectedProfile))(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutSaveAs(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			labelText := "Save Profile"
			if a.selectedProfile != "" {
				labelText = fmt.Sprintf("Update %s", a.selectedProfile)
			}
			btn := material.Button(a.theme, &a.saveBtn, labelText)
			return btn.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutAudiogram(gtx, a.theme, a.left, a.right)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutSpectrum(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.backBtn, "Back To Start")
			return btn.Layout(gtx)
		}),
	)
}

func (a *App) layoutProfiles(gtx layout.Context) layout.Dimensions {
	if len(a.profiles) == 0 {
		return label(a.theme, "Profiles: no named profiles yet.")(gtx)
	}
	cardWidth := gtx.Dp(unit.Dp(260))
	gap := gtx.Dp(unit.Dp(12))
	if gtx.Constraints.Max.X > 0 && gtx.Constraints.Max.X < cardWidth {
		cardWidth = gtx.Constraints.Max.X
	}
	perRow := 1
	if cardWidth > 0 {
		perRow = max(1, (gtx.Constraints.Max.X+gap)/(cardWidth+gap))
	}
	rows := (len(a.profiles) + perRow - 1) / perRow
	children := make([]layout.FlexChild, 0, rows*2+1)
	children = append(children, layout.Rigid(label(a.theme, "Profiles")))
	for row := 0; row < rows; row++ {
		start := row * perRow
		end := min(start+perRow, len(a.profiles))
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				rowChildren := make([]layout.FlexChild, 0, perRow*2)
				for i := start; i < end; i++ {
					idx := i
					p := a.profiles[i]
					if len(rowChildren) > 0 {
						rowChildren = append(rowChildren, layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout))
					}
					rowChildren = append(rowChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutProfileCell(gtx, cardWidth, func(gtx layout.Context) layout.Dimensions {
							return a.layoutProfileCard(gtx, p, idx)
						})
					}))
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx, rowChildren...)
			}),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (a *App) layoutProfileCard(gtx layout.Context, p profile.NamedProfile, idx int) layout.Dimensions {
	selected := a.selectedProfile == p.Name
	panelColor := color.NRGBA{R: 20, G: 22, B: 26, A: 255}
	borderColor := color.NRGBA{R: 44, G: 48, B: 56, A: 255}
	if selected {
		panelColor = color.NRGBA{R: 28, G: 36, B: 56, A: 255}
		borderColor = color.NRGBA{R: 86, G: 142, B: 255, A: 255}
	}
	return layoutPanel(gtx, panelColor, borderColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if a.renamingProfile == p.Name {
					ed := material.Editor(a.theme, &a.renameEditor, "Rename profile")
					return ed.Layout(gtx)
				}
				name := p.Name
				if selected {
					name += " (selected)"
				}
				if p.Name == profile.FlatProfileName {
					name += " - EQ off"
				}
				lbl := material.Body1(a.theme, name)
				lbl.Font.Weight = 600
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceStart, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if a.renamingProfile == p.Name {
							btn := material.Button(a.theme, &a.renameSaveBtns[idx], "Apply")
							return btn.Layout(gtx)
						}
						btn := material.Button(a.theme, &a.selectBtns[idx], "Select")
						if selected {
							btn.Background = color.NRGBA{R: 66, G: 133, B: 244, A: 255}
						}
						return btn.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if p.Name == profile.FlatProfileName {
							return label(a.theme, "Built-in")(gtx)
						}
						btn := material.Button(a.theme, &a.renameBtns[idx], "Rename")
						return btn.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if p.Name == profile.FlatProfileName {
							return layout.Dimensions{}
						}
						btn := material.Button(a.theme, &a.deleteBtns[idx], "Delete")
						return btn.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func layoutPanel(gtx layout.Context, bg, border color.NRGBA, child layout.Widget) layout.Dimensions {
	pad := layout.UniformInset(unit.Dp(12))
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			size := gtx.Constraints.Min
			if size.X == 0 {
				size.X = gtx.Constraints.Max.X
			}
			rect := image.Rectangle{Max: size}
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(rect, 10).Op(gtx.Ops))
			paint.FillShape(gtx.Ops, border, clip.Stroke{Path: clip.UniformRRect(rect, 10).Path(gtx.Ops), Width: float32(gtx.Dp(unit.Dp(1)))}.Op())
			return layout.Dimensions{Size: rect.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return pad.Layout(gtx, child)
		}),
	)
}

func layoutLeftColumn(gtx layout.Context, maxWidthDp float32, child layout.Widget) layout.Dimensions {
	maxWidth := gtx.Dp(unit.Dp(maxWidthDp))
	if gtx.Constraints.Max.X > 0 && gtx.Constraints.Max.X < maxWidth {
		maxWidth = gtx.Constraints.Max.X
	}
	gtx.Constraints.Max.X = maxWidth
	if gtx.Constraints.Min.X > maxWidth {
		gtx.Constraints.Min.X = maxWidth
	}
	return child(gtx)
}

func layoutProfileCell(gtx layout.Context, width int, child layout.Widget) layout.Dimensions {
	if width > 0 {
		gtx.Constraints.Min.X = width
		gtx.Constraints.Max.X = width
	}
	return child(gtx)
}

func (a *App) layoutSaveAs(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceStart, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			ed := material.Editor(a.theme, &a.nameEditor, "Profile name")
			return ed.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(a.theme, &a.saveAsBtn, "Save As")
			return btn.Layout(gtx)
		}),
	)
}

func (a *App) layoutSpectrum(gtx layout.Context) layout.Dimensions {
	if a.eng == nil {
		return layout.Dimensions{}
	}
	snap := a.eng.Spectrum()
	return layoutSpectrum(gtx, a.theme, snap)
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
	if strings.TrimSpace(a.selectedProfile) != "" {
		if err := a.saveNamedProfile(a.selectedProfile); err != nil {
			a.err = err
		}
		return
	}
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
	a.refreshProfiles()
	if a.eng != nil {
		if err := a.eng.EnsureStartedWithCurrentProfile(); err != nil {
			a.err = fmt.Errorf("start EQ engine: %w", err)
			return
		}
	}
	a.err = fmt.Errorf("saved profile to ~/.config/hearing-eq/profile.json")
}

func (a *App) onSaveAsCurrent() {
	name := strings.TrimSpace(a.nameEditor.Text())
	if name == "" {
		a.err = fmt.Errorf("profile name must not be empty")
		return
	}
	if err := a.saveNamedProfile(name); err != nil {
		a.err = err
		return
	}
	a.nameEditor.SetText("")
}

func (a *App) saveNamedProfile(name string) error {
	p, err := profile.New(a.left, a.right)
	if err != nil {
		return fmt.Errorf("build profile: %w", err)
	}
	if err := profile.SaveNamed(name, p); err != nil {
		return fmt.Errorf("save named profile: %w", err)
	}
	a.selectedProfile = name
	a.loadedAt = p.CreatedAt
	a.hasSaved = true
	a.refreshProfiles()
	if a.eng != nil {
		if err := a.eng.EnsureStartedWithCurrentProfile(); err != nil {
			return fmt.Errorf("start EQ engine: %w", err)
		}
	}
	a.err = fmt.Errorf("saved profile %q", name)
	return nil
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
	np, err := profile.LoadCurrent()
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return
		}
		return
	}
	copy(a.left, np.Profile.LeftThresholdsDBFS)
	copy(a.right, np.Profile.RightThresholdsDBFS)
	a.loadedAt = np.Profile.CreatedAt
	a.hasSaved = true
	a.selectedProfile = np.Name
}

func (a *App) stopTest(message string) {
	a.runningTest = false
	a.screen = ScreenWelcome
	a.err = fmt.Errorf("%s", message)
	a.left = make([]float64, len(profile.DefaultFrequenciesHz))
	a.right = make([]float64, len(profile.DefaultFrequenciesHz))
	a.hasSaved = false
	a.loadedAt = time.Time{}
	a.loadSavedProfile()
	_ = a.audio.StopCalibrationTone()
}

func (a *App) refreshProfiles() {
	profiles, err := profile.List()
	if err != nil {
		return
	}
	a.profiles = profiles
	a.selectBtns = make([]widget.Clickable, len(profiles))
	a.renameBtns = make([]widget.Clickable, len(profiles))
	a.deleteBtns = make([]widget.Clickable, len(profiles))
	a.renameSaveBtns = make([]widget.Clickable, len(profiles))
}

func (a *App) onSelectProfile(i int) {
	if i < 0 || i >= len(a.profiles) {
		return
	}
	if err := profile.Select(a.profiles[i].Name); err != nil {
		a.err = fmt.Errorf("select profile: %w", err)
		return
	}
	a.resetRenameState()
	a.reloadProfilesView()
	if a.eng != nil {
		if err := a.eng.EnsureStartedWithCurrentProfile(); err != nil {
			a.err = fmt.Errorf("reload EQ engine: %w", err)
		}
	}
}

func (a *App) onStartRename(i int) {
	if i < 0 || i >= len(a.profiles) {
		return
	}
	a.renamingProfile = a.profiles[i].Name
	a.renameEditor.SetText(a.profiles[i].Name)
}

func (a *App) onConfirmRename(i int) {
	if i < 0 || i >= len(a.profiles) {
		return
	}
	newName := strings.TrimSpace(a.renameEditor.Text())
	if err := profile.Rename(a.profiles[i].Name, newName); err != nil {
		a.err = fmt.Errorf("rename profile: %w", err)
		return
	}
	a.resetRenameState()
	a.reloadProfilesView()
	if a.eng != nil {
		if err := a.eng.EnsureStartedWithCurrentProfile(); err != nil {
			a.err = fmt.Errorf("reload EQ engine: %w", err)
		}
	}
}

func (a *App) onDeleteProfile(i int) {
	if i < 0 || i >= len(a.profiles) {
		return
	}
	if err := profile.Delete(a.profiles[i].Name); err != nil {
		a.err = fmt.Errorf("delete profile: %w", err)
		return
	}
	a.resetRenameState()
	a.left = make([]float64, len(profile.DefaultFrequenciesHz))
	a.right = make([]float64, len(profile.DefaultFrequenciesHz))
	a.hasSaved = false
	a.selectedProfile = ""
	a.loadedAt = time.Time{}
	a.reloadProfilesView()
	if a.eng != nil && a.hasSaved {
		if err := a.eng.EnsureStartedWithCurrentProfile(); err != nil {
			a.err = fmt.Errorf("reload EQ engine: %w", err)
		}
	}
}

func (a *App) resetRenameState() {
	a.renamingProfile = ""
	a.renameEditor.SetText("")
}

func (a *App) reloadProfilesView() {
	a.loadSavedProfile()
	a.refreshProfiles()
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

func layoutAudiogram(gtx layout.Context, th *material.Theme, left, right []float64) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(260)))
	if size.X <= 0 {
		size.X = gtx.Dp(unit.Dp(480))
	}
	rect := image.Rectangle{Max: size}
	paint.FillShape(gtx.Ops, color.NRGBA{R: 20, G: 22, B: 26, A: 255}, clip.Rect(rect).Op())
	drawGrid(gtx, rect)
	drawSeries(gtx, rect, left, color.NRGBA{R: 100, G: 181, B: 246, A: 255})
	drawSeries(gtx, rect, right, color.NRGBA{R: 239, G: 83, B: 80, A: 255})
	drawAudiogramAxes(gtx, th, rect)
	drawLegend(gtx, rect, th, []legendItem{
		{Label: "Left Threshold", Color: color.NRGBA{R: 100, G: 181, B: 246, A: 255}},
		{Label: "Right Threshold", Color: color.NRGBA{R: 239, G: 83, B: 80, A: 255}},
	})
	return layout.Dimensions{Size: size}
}

func layoutSpectrum(gtx layout.Context, th *material.Theme, snap spectrum.Snapshot) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(220)))
	if size.X <= 0 {
		size.X = gtx.Dp(unit.Dp(480))
	}
	rect := image.Rectangle{Max: size}
	paint.FillShape(gtx.Ops, color.NRGBA{R: 14, G: 18, B: 22, A: 255}, clip.Rect(rect).Op())
	drawSpectrumGrid(gtx, rect)
	drawSpectrumBars(gtx, rect, snap)
	drawSpectrumAxes(gtx, th, rect)
	drawLegend(gtx, rect, th, []legendItem{
		{Label: "Left Output", Color: color.NRGBA{R: 100, G: 181, B: 246, A: 255}},
		{Label: "Input", Color: color.NRGBA{R: 224, G: 224, B: 224, A: 255}},
		{Label: "Right Output", Color: color.NRGBA{R: 239, G: 83, B: 80, A: 255}},
	})
	return layout.Dimensions{Size: size}
}

type legendItem struct {
	Label string
	Color color.NRGBA
}

func drawLegend(gtx layout.Context, rect image.Rectangle, th *material.Theme, items []legendItem) {
	if len(items) == 0 {
		return
	}
	legendHeight := 22
	legendRect := image.Rect(rect.Min.X+10, rect.Min.Y+8, rect.Max.X-10, rect.Min.Y+8+legendHeight)
	paint.FillShape(gtx.Ops, color.NRGBA{R: 8, G: 10, B: 12, A: 180}, clip.UniformRRect(legendRect, 6).Op(gtx.Ops))
	itemWidth := max(120, legendRect.Dx()/len(items))
	for i, item := range items {
		x := legendRect.Min.X + i*itemWidth + 10
		y := legendRect.Min.Y + 11
		line := clip.Path{}
		line.Begin(gtx.Ops)
		line.MoveTo(f32.Pt(float32(x), float32(y)))
		line.LineTo(f32.Pt(float32(x+18), float32(y)))
		paint.FillShape(gtx.Ops, item.Color, clip.Stroke{Path: line.End(), Width: 3}.Op())
		paintLabel(gtx, th, x+24, legendRect.Min.Y+5, item.Label, color.NRGBA{R: 230, G: 232, B: 235, A: 255})
	}
}

func drawAudiogramAxes(gtx layout.Context, th *material.Theme, rect image.Rectangle) {
	paintLabel(gtx, th, rect.Min.X+12, rect.Max.Y-18, "Hz", color.NRGBA{R: 210, G: 214, B: 220, A: 255})
	paintLabel(gtx, th, rect.Min.X+12, rect.Min.Y+34, "dBFS", color.NRGBA{R: 210, G: 214, B: 220, A: 255})
	for _, freq := range profile.DefaultFrequenciesHz {
		x := mapX(freq, rect)
		paintLabel(gtx, th, x-16, rect.Max.Y-18, fmt.Sprintf("%.0f", freq), color.NRGBA{R: 180, G: 186, B: 194, A: 255})
	}
	for db := -60.0; db <= -20; db += 10 {
		y := mapY(db, rect)
		paintLabel(gtx, th, rect.Min.X+4, y-8, fmt.Sprintf("%.0f", db), color.NRGBA{R: 180, G: 186, B: 194, A: 255})
	}
}

func drawSpectrumAxes(gtx layout.Context, th *material.Theme, rect image.Rectangle) {
	paintLabel(gtx, th, rect.Min.X+12, rect.Max.Y-18, "Hz", color.NRGBA{R: 210, G: 214, B: 220, A: 255})
	paintLabel(gtx, th, rect.Min.X+12, rect.Min.Y+34, "dBFS", color.NRGBA{R: 210, G: 214, B: 220, A: 255})
	for _, freq := range profile.DefaultFrequenciesHz {
		x := mapX(freq, rect)
		paintLabel(gtx, th, x-16, rect.Max.Y-18, fmt.Sprintf("%.0f", freq), color.NRGBA{R: 180, G: 186, B: 194, A: 255})
	}
	for _, db := range []float64{-96, -72, -48, -24, 0} {
		y := spectrumY(db, rect)
		paintLabel(gtx, th, rect.Min.X+4, y-8, fmt.Sprintf("%.0f", db), color.NRGBA{R: 180, G: 186, B: 194, A: 255})
	}
}

func paintLabel(gtx layout.Context, th *material.Theme, x, y int, text string, c color.NRGBA) {
	stack := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	defer stack.Pop()
	label := material.Label(th, unit.Sp(12), text)
	label.Color = c
	label.Layout(gtx)
}

func drawSpectrumGrid(gtx layout.Context, rect image.Rectangle) {
	for i := 0; i <= 4; i++ {
		y := rect.Min.Y + i*rect.Dy()/4
		path := clip.Path{}
		path.Begin(gtx.Ops)
		path.MoveTo(f32.Pt(float32(rect.Min.X), float32(y)))
		path.LineTo(f32.Pt(float32(rect.Max.X), float32(y)))
		paint.FillShape(gtx.Ops, color.NRGBA{R: 46, G: 52, B: 60, A: 255}, clip.Stroke{Path: path.End(), Width: 1}.Op())
	}
	for _, freq := range profile.DefaultFrequenciesHz {
		x := mapX(freq, rect)
		path := clip.Path{}
		path.Begin(gtx.Ops)
		path.MoveTo(f32.Pt(float32(x), float32(rect.Min.Y)))
		path.LineTo(f32.Pt(float32(x), float32(rect.Max.Y)))
		paint.FillShape(gtx.Ops, color.NRGBA{R: 58, G: 66, B: 78, A: 255}, clip.Stroke{Path: path.End(), Width: 1}.Op())
	}
}

func drawSpectrumBars(gtx layout.Context, rect image.Rectangle, snap spectrum.Snapshot) {
	if len(snap.FrequenciesHz) == 0 {
		return
	}
	bandWidth := max(18, (rect.Dx()-20)/len(profile.DefaultFrequenciesHz)/2)
	barWidth := max(4, bandWidth/4)
	leftBound := rect.Min.X + 10
	rightBound := rect.Max.X - 10
	for _, anchorFreq := range profile.DefaultFrequenciesHz {
		centerX := clamp(mapX(anchorFreq, rect), leftBound+barWidth*2, rightBound-barWidth*2)
		leftDB, inputDB, rightDB := bandAggregate(anchorFreq, snap)
		drawFancyBar(gtx, centerX-barWidth*2, spectrumY(leftDB, rect), rect.Max.Y-8, barWidth, color.NRGBA{R: 100, G: 181, B: 246, A: 220}, color.NRGBA{R: 140, G: 208, B: 255, A: 255})
		drawFancyBar(gtx, centerX-barWidth/2, spectrumY(inputDB, rect), rect.Max.Y-8, barWidth, color.NRGBA{R: 190, G: 190, B: 198, A: 210}, color.NRGBA{R: 240, G: 240, B: 244, A: 255})
		drawFancyBar(gtx, centerX+barWidth, spectrumY(rightDB, rect), rect.Max.Y-8, barWidth, color.NRGBA{R: 239, G: 83, B: 80, A: 220}, color.NRGBA{R: 255, G: 138, B: 128, A: 255})
	}
}

func bandAggregate(anchorFreq float64, snap spectrum.Snapshot) (left, input, right float64) {
	idx := frequencyIndex(anchorFreq)
	if idx < 0 {
		return -96, -96, -96
	}
	lo, hi := bandBounds(idx)
	leftSum, inputSum, rightSum := 0.0, 0.0, 0.0
	count := 0.0
	for i, freq := range snap.FrequenciesHz {
		if freq < lo || freq > hi {
			continue
		}
		leftSum += dbToPower(snap.LeftDB[i])
		inputSum += dbToPower(snap.InputDB[i])
		rightSum += dbToPower(snap.RightDB[i])
		count++
	}
	if count == 0 {
		nearest := nearestSpectrumBin(anchorFreq, snap.FrequenciesHz)
		if nearest < 0 {
			return -96, -96, -96
		}
		return snap.LeftDB[nearest], snap.InputDB[nearest], snap.RightDB[nearest]
	}
	left = powerToDB(leftSum / count)
	input = powerToDB(inputSum / count)
	right = powerToDB(rightSum / count)
	return left, input, right
}

func bandBounds(idx int) (float64, float64) {
	anchor := profile.DefaultFrequenciesHz[idx]
	last := len(profile.DefaultFrequenciesHz) - 1
	switch {
	case idx == 0:
		ratio := math.Sqrt(profile.DefaultFrequenciesHz[1] / anchor)
		return anchor / ratio, anchor * ratio
	case idx == last:
		ratio := math.Sqrt(anchor / profile.DefaultFrequenciesHz[last-1])
		return anchor / ratio, anchor * ratio
	default:
		return math.Sqrt(profile.DefaultFrequenciesHz[idx-1] * anchor), math.Sqrt(anchor * profile.DefaultFrequenciesHz[idx+1])
	}
}

func nearestSpectrumBin(target float64, freqs []float64) int {
	if len(freqs) == 0 {
		return -1
	}
	best := 0
	bestDiff := math.Abs(freqs[0] - target)
	for i := 1; i < len(freqs); i++ {
		if diff := math.Abs(freqs[i] - target); diff < bestDiff {
			best = i
			bestDiff = diff
		}
	}
	return best
}

func dbToPower(db float64) float64 {
	return math.Pow(10, db/10)
}

func powerToDB(power float64) float64 {
	return 10 * math.Log10(math.Max(power, 1e-12))
}

func drawFancyBar(gtx layout.Context, x, topY, bottomY, width int, base, accent color.NRGBA) {
	if bottomY <= topY {
		return
	}
	rect := image.Rect(x, topY, x+width, bottomY)
	paint.FillShape(gtx.Ops, base, clip.UniformRRect(rect, min(width/2, 6)).Op(gtx.Ops))
	highlight := image.Rect(x+1, topY+1, x+max(2, width/3), bottomY-1)
	paint.FillShape(gtx.Ops, accent, clip.UniformRRect(highlight, min(width/3, 4)).Op(gtx.Ops))
}

func spectrumY(v float64, rect image.Rectangle) int {
	minDB := -96.0
	maxDB := 0.0
	clamped := math.Max(minDB, math.Min(maxDB, v))
	norm := (clamped - minDB) / (maxDB - minDB)
	return rect.Max.Y - int(norm*float64(rect.Dy()-16)) - 8
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func drawGrid(gtx layout.Context, rect image.Rectangle) {
	for _, f := range profile.DefaultFrequenciesHz {
		x := mapX(f, rect)
		path := clip.Path{}
		path.Begin(gtx.Ops)
		path.MoveTo(f32.Pt(float32(x), float32(rect.Min.Y)))
		path.LineTo(f32.Pt(float32(x), float32(rect.Max.Y)))
		paint.FillShape(gtx.Ops, color.NRGBA{R: 74, G: 82, B: 92, A: 255}, clip.Stroke{Path: path.End(), Width: 1}.Op())
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
	path := clip.Path{}
	path.Begin(gtx.Ops)
	path.MoveTo(points[0])
	for _, p := range points[1:] {
		path.LineTo(p)
	}
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: path.End(), Width: 2}.Op())
	for _, p := range points {
		r := image.Rect(int(p.X)-3, int(p.Y)-3, int(p.X)+3, int(p.Y)+3)
		paint.FillShape(gtx.Ops, c, clip.UniformRRect(r, 3).Op(gtx.Ops))
	}
}

func mapX(freq float64, rect image.Rectangle) int {
	if freq <= profile.DefaultFrequenciesHz[0] {
		return rect.Min.X + 10
	}
	last := len(profile.DefaultFrequenciesHz) - 1
	if freq >= profile.DefaultFrequenciesHz[last] {
		return rect.Max.X - 10
	}
	for i := 0; i < last; i++ {
		lo := profile.DefaultFrequenciesHz[i]
		hi := profile.DefaultFrequenciesHz[i+1]
		if freq < lo || freq > hi {
			continue
		}
		segmentStart := float64(i) / float64(last)
		segmentWidth := 1.0 / float64(last)
		frac := (math.Log10(freq) - math.Log10(lo)) / (math.Log10(hi) - math.Log10(lo))
		x := segmentStart + frac*segmentWidth
		return rect.Min.X + int(x*float64(rect.Dx()-20)) + 10
	}
	return rect.Min.X + 10
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
