package main

import (
	"fmt"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// The chat popup: the pet's real-time conversation surface.
//
// It is a THIRD occupant of the window, alongside the panel and the speech
// bubble, and it obeys the same two rules they do:
//
//  1. It is drawn entirely inside the window's existing rectangle. The window is
//     never resized for it, so the webview's viewport never changes and the pet
//     cannot shift (README 10(c)).
//  2. What the window region is cropped to must be exactly what the page paints.
//     Every rectangle below is therefore also handed to
//     desktop.SetWindowRegion, and the thought puffs get true elliptical regions
//     because the page draws them with border-radius: 50%.
//
// It HANGS OFF THE PET rather than sitting in the panel's slot:
//
//	         +----------------------+   <- card: the pet's reply, and nothing else
//	         | 喵~ 我在桌上打滚…    |
//	         +----------------------+
//	              (o)                    <- thought trail, drifting out of the pet
//	            (o)
//	           (o)      [ pet ]
//	         +----------------------+   <- bar: the input box, right under the pet
//	         | 说点什么…            |
//	         +----------------------+
//
// The column is centred on the pet (pulled back inside the window when the pet
// sits against an edge), the card is directly above it and the input box directly
// below it. An earlier version put both in the panel's slot, which is the far
// side of the window from the pet - the reply card ended up ~300px away from the
// cat it was supposed to be coming out of.
//
// It shares the slot with the panel and the bubble, so exactly one of the three
// is ever visible (docs/reminder-pomodoro.md 8.2). The panel is the permanent
// record; the popup is the live conversation. Opening either folds the other
// away.

// Overlay sizes in logical pixels.
//
// Like the bubble's sizes these are FIXED rather than content-driven, because
// the region crop is derived from them: a region larger than the painted card
// leaves glass-composited pixels that read as a pale blue band, and one smaller
// than it clips the card (docs/reminder-pomodoro.md 8.1). The card scrolls
// internally instead of growing.
//
// They are logical, and so is every proportion below, because the window scales
// with the pet: at 200% the window is 1080x1040 and the pet is 300x400, but the
// pet is still 200/520 of the window's height and still centred. The layout is
// therefore identical at every scale - only the conversion to physical pixels
// changes - which is why there is no scale-dependent fallback anywhere below.
const (
	liveMargin = 8
	// liveCardHeight is the card's height when there is room for it. It is a CAP,
	// not a guarantee: the card takes what the gap above the pet allows.
	liveCardHeight = 112
	// liveCardMinHeight is the shortest card worth drawing. Room for less than
	// this means the trail is dropped to make space (see computeLiveOverlay).
	liveCardMinHeight = 56
	liveBarHeight     = 56
	// liveColWidth is how wide the reply card and the input box are.
	liveColWidth = 280
	// liveGap is the space between the pet and the surface beside it.
	liveGap = 8
	// liveTrailGap is the space between the card's bottom edge and the first puff.
	liveTrailGap = 4
	// liveTrailOverlap is how far the trail dips INTO the pet's box.
	//
	// The top of the pet artwork is transparent - only the tips of the ears are
	// there - so letting the trail overlap it is what makes the bubbles read as
	// coming out of the pet instead of stopping short above it. Without this the
	// trail plus the card did not fit in the gap above the pet at all.
	liveTrailOverlap = 20
)

// livePuff is one thought-trail ellipse, from the largest (highest, nearest the
// card) to the smallest (lowest, nearest the pet).
type livePuff struct {
	w, h int
	// dy is the gap between this puff's top and the previous puff's bottom.
	dy int
}

// livePuffs is the trail, ordered card -> pet.
//
// The sizes shrink as the trail approaches the pet, which is what makes it read
// as receding into the distance rather than as three dots in a column.
var livePuffs = []livePuff{
	{w: 34, h: 20},
	{w: 24, h: 14, dy: 5},
	{w: 15, h: 9, dy: 7},
}

// LiveRect is one rectangle of the live overlay, in CSS pixels relative to the
// window's top-left corner.
type LiveRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// LiveView is the live overlay's geometry.
//
// It travels with the layout for the same reason the bubble's does: the region
// crop is derived from these rectangles, so a frame in which the frontend has
// the content but not the geometry (or the reverse) would paint a card that is
// not visible, or expose a region the page does not fill.
type LiveView struct {
	// Card is the reply surface above the pet.
	Card LiveRect `json:"card"`
	// Bar is the input box below the pet.
	Bar LiveRect `json:"bar"`
	// Puffs are the thought-trail ellipses, card-first.
	Puffs []LiveRect `json:"puffs"`
	// TailX is where the card's little pointer at the pet sits, as an offset from
	// the card's LEFT edge in CSS pixels.
	//
	// It is computed here rather than derived on the frontend from OnRight,
	// because the card is centred on the pet and then clamped inside the window:
	// near a screen edge the pet is no longer under the card's centre, and a
	// "which side is it on" guess would put the tail on the wrong half. The
	// offset is already clamped so the tail stays inside the card's rectangle -
	// outside that rectangle is outside the window region.
	TailX float64 `json:"tailX"`
	// OnRight is true when the card's centre sits to the right of the pet's.
	OnRight bool `json:"onRight"`
}

// setLiveLocked opens or closes the overlay and keeps the layout in step.
//
// This is the counterpart of setBubbleLocked, and it exists for the same reason:
// the frontend renders the overlay FROM THE LAYOUT, so a layout that still
// advertises a closed overlay makes it spring back on the next GetLayout - the
// exact bug that once made the summary bubble impossible to dismiss.
//
// Opening only flips the flag; the rectangles are filled in by
// applyWindowGeometry, which is the one place that knows the window's geometry.
//
// Callers must hold a.mu.
func (a *App) setLiveLocked(open bool) {
	a.liveOpen = open
	if !open {
		a.layout.Live = nil
	}
}

// computeLiveOverlay lays the overlay out inside the window, hanging it off the
// pet.
//
// All inputs and outputs are physical pixels except the returned view, which is
// converted to CSS pixels at the end so the frontend and the region agree by
// construction rather than by two matching calculations.
//
// The pet is the fixed point of the layout: the input box is pinned directly
// below it and the reply card directly above it, with the thought trail between
// them. Everything is clamped inside the window, because a rectangle outside the
// window cannot be cropped to and would simply not be visible.
func computeLiveOverlay(petX, petY, petW, petH, totalW, totalH int, s float64) ([]desktop.RegionRect, *LiveView) {
	round := func(v float64) int { return int(v + 0.5) }
	sc := func(v int) int { return round(float64(v) * s) }

	margin := sc(liveMargin)
	gap := sc(liveGap)
	colW := sc(liveColWidth)
	barH := sc(liveBarHeight)
	cardMinH := sc(liveCardMinHeight)

	// The column is centred on the pet, then pulled back inside the window. It is
	// deliberately NOT snapped to the pet's own edges: the pet is only 150 wide
	// and the card needs 280, so aligning them would push the card off the window
	// whenever the pet sits against a screen edge.
	colX := petX + petW/2 - colW/2
	if colX < margin {
		colX = margin
	}
	if maxX := totalW - margin - colW; colX > maxX {
		colX = maxX
	}
	if colX < 0 {
		colX = 0 // window narrower than the column; the region crop still applies
	}

	// The input box hangs directly under the pet. This is the anchor the user
	// asked for: "the input box is right below the pet".
	bar := desktop.Bounds{X: colX, Y: petY + petH + gap, Width: colW, Height: barH}

	// The trail is decoration and the card is content, so the trail is what gives
	// way when the gap above the pet cannot hold both.
	trailH := 0
	for i, p := range livePuffs {
		if i > 0 {
			trailH += sc(p.dy)
		}
		trailH += sc(p.h)
	}
	trailGap := sc(liveTrailGap)
	overlap := sc(liveTrailOverlap)
	// How far above the pet's top edge the trail reaches. The trail is allowed to
	// dip `overlap` into the pet's box, whose top is transparent (only the ear
	// tips are there), which is what lets the bubbles read as emerging from the
	// pet rather than stopping short above it.
	trailRise := trailH + trailGap - overlap

	spaceAbove := petY - margin
	keepTrail := trailRise >= 0 && spaceAbove-trailRise >= cardMinH
	if !keepTrail {
		trailRise = 0
	}

	// The card's bottom edge: above the trail when there is one, just above the
	// pet when there is not.
	cardBottom := petY - gap
	if keepTrail {
		cardBottom = petY + overlap - trailH - trailGap
	}
	cardH := sc(liveCardHeight)
	if maxH := cardBottom - margin; cardH > maxH {
		cardH = maxH
	}
	if cardH < cardMinH {
		// Only reachable at a scale where the window is barely taller than the
		// pet. A card too short for a sentence is still better than none, and the
		// alternative - drawing outside the window - would be cropped away.
		cardH = cardMinH
	}
	cardTop := cardBottom - cardH
	if cardTop < margin {
		cardTop = margin
		cardBottom = cardTop + cardH
	}
	card := desktop.Bounds{X: colX, Y: cardTop, Width: colW, Height: cardH}

	// The puffs drift from the card's centre toward the pet's centre, so the trail
	// always points at the pet whatever side of the window either of them is on.
	//
	// The gap before puff i is puff i's OWN dy ("the gap between this puff's top
	// and the previous puff's bottom") - the same field the trailH sum above uses.
	// Advancing with the PREVIOUS puff's dy instead would silently drop or double
	// every gap, and the trail would stop short of the pet or run into it.
	puffs := make([]desktop.Bounds, 0, len(livePuffs))
	if keepTrail {
		cardCX := colX + colW/2
		petCX := petX + petW/2
		top := cardBottom + trailGap
		total := trailH
		// Cumulative height down the trail, used to place each puff along the line.
		cum := 0
		for i, p := range livePuffs {
			w, h := sc(p.w), sc(p.h)
			// The fraction along the trail, measured at this puff's centre.
			frac := 0.0
			if total > 0 {
				frac = float64(cum+h/2) / float64(total)
			}
			cx := cardCX + int(float64(petCX-cardCX)*frac)
			x := cx - w/2
			if x < 0 {
				x = 0
			}
			if x+w > totalW {
				x = totalW - w
			}
			y := top
			if y+h > totalH {
				y = totalH - h
			}
			if y < 0 {
				y = 0
			}
			puffs = append(puffs, desktop.Bounds{X: x, Y: y, Width: w, Height: h})
			cum += h
			top += h
			// Advance by the NEXT puff's gap, which is what trailH summed.
			if i+1 < len(livePuffs) {
				gap := sc(livePuffs[i+1].dy)
				cum += gap
				top += gap
			}
		}
	}

	regions := make([]desktop.RegionRect, 0, len(puffs)+3)
	radius := round(float64(panelRadius) * s)
	regions = append(regions,
		desktop.RegionRect{Bounds: card, Radius: radius},
		desktop.RegionRect{Bounds: bar, Radius: radius},
	)
	for _, p := range puffs {
		// A true ellipse, matching border-radius: 50%.
		regions = append(regions, desktop.RegionRect{Bounds: p, Ellipse: true})
	}

	toRect := func(b desktop.Bounds) LiveRect {
		return LiveRect{
			X: float64(b.X) / s,
			Y: float64(b.Y) / s,
			W: float64(b.Width) / s,
			H: float64(b.Height) / s,
		}
	}
	view := &LiveView{
		Card: toRect(card),
		Bar:  toRect(bar),
		// OnRight describes where the CARD sits relative to the pet.
		OnRight: colX+colW/2 > petX+petW/2,
	}
	// The tail points back at the pet's centre, clamped so the triangle stays
	// wholly inside the card (a 7px half-width plus a little margin).
	petCX := petX + petW/2
	tailX := petCX - card.X - sc(9)
	if minX := sc(4); tailX < minX {
		tailX = minX
	}
	if maxX := card.Width - sc(9) - sc(14); tailX > maxX {
		tailX = maxX
	}
	view.TailX = float64(tailX) / s
	for _, p := range puffs {
		view.Puffs = append(view.Puffs, toRect(p))
	}
	return regions, view
}

// --- proactive speech ------------------------------------------------------

// Proactive speech is the pet speaking up on its own (docs/llm-chat.md 9).
//
// The decision is a pure function for the same reason decideReminder is: the
// whole gate list can then be tested without a clock, a window or a running pet.
// Every gate exists to answer "would this be an interruption the user did not
// ask for?", so the default answer is no.

// proactiveCheckEvery is how often the gate is evaluated. It matches the
// reminder's tick: the minimum gap is measured in minutes, so half a minute of
// slop is invisible.
const proactiveCheckEvery = 30 * time.Second

// proactiveMinGapFallback is used when the configured gap is nonsense (zero or
// negative), so a corrupt config cannot turn the pet into a metronome.
const proactiveMinGapFallback = 60 * time.Minute

// proactiveDecision is the gate's verdict for one tick.
type proactiveDecision struct {
	// Fire is true when the pet should speak now.
	Fire bool
	// Reason explains the verdict, for the debug log. It is the answer to "why
	// didn't it say anything?".
	Reason string
	// NextIn is how long to wait before evaluating again.
	NextIn time.Duration
}

// decideProactive evaluates one tick.
//
// now is the current time, lastSpoke is when the pet last spoke unprompted
// (zero when it never has), dueAt is when the next unprompted remark is
// scheduled (zero when none is pending) and material reports whether there is
// anything worth talking about at all.
func decideProactive(
	cfg store.Config,
	st store.PomodoroState,
	now, lastSpoke, dueAt time.Time,
	fullscreen, overBudget, panelOpen, material, awaitingAnswer, petHidden bool,
) proactiveDecision {
	if !cfg.LLMEnabled {
		return proactiveDecision{Reason: "the LLM chat is off"}
	}
	if !cfg.LLMProactive {
		return proactiveDecision{Reason: "proactive speech is off"}
	}
	if overBudget {
		return proactiveDecision{Reason: "today's request budget is spent"}
	}
	// Never interrupt a focus phase. This is the same rule the reminder obeys,
	// and it is the one the user cares about most: the pet's own opinion is
	// worth less than the session they are in the middle of.
	if st.IsFocusing() {
		return proactiveDecision{Reason: "a focus phase is running"}
	}
	if fullscreen {
		return proactiveDecision{Reason: "a full-screen window is in front"}
	}
	if panelOpen {
		return proactiveDecision{Reason: "the panel is open"}
	}
	// The pet is hidden, so the overlay it would open has nowhere to appear -
	// and the whole point of hiding is to be left alone. Speaking anyway would
	// also record a remark in the transcript that the user never saw.
	if petHidden {
		return proactiveDecision{Reason: "the pet is hidden"}
	}
	// A bubble is waiting for the user to answer - the picker or the summary
	// prompt. Saying something unprompted would open the overlay, which takes the
	// slot the question is holding and would destroy it; and even if it could
	// share, talking over a question the user has not answered is the definition
	// of an interruption they did not ask for.
	if awaitingAnswer {
		return proactiveDecision{Reason: "a bubble is waiting for an answer"}
	}
	// Quiet hours reuse the reminder's active window, so the user has exactly one
	// place to say when the pet may interrupt them.
	rem := cfg.Reminder()
	if !store.WithinActiveWindow(rem.From, rem.To, now) {
		return proactiveDecision{Reason: fmt.Sprintf("outside the active window %s-%s", rem.From, rem.To)}
	}
	// Nothing to say is a reason not to speak. Without this the pet would
	// manufacture small talk to fill an empty day.
	if !material {
		return proactiveDecision{Reason: "nothing worth bringing up"}
	}

	gap := time.Duration(cfg.LLMProactiveMinGapMin) * time.Minute
	if gap <= 0 {
		gap = proactiveMinGapFallback
	}
	if !lastSpoke.IsZero() && now.Sub(lastSpoke) < gap {
		return proactiveDecision{
			Reason: "inside the minimum gap",
			NextIn: gap - now.Sub(lastSpoke),
		}
	}
	if dueAt.IsZero() {
		// Nothing scheduled yet: schedule the first remark rather than speaking
		// immediately, so switching the feature on does not interrupt at once.
		return proactiveDecision{Reason: "scheduled", NextIn: gap}
	}
	if now.Before(dueAt) {
		return proactiveDecision{Reason: "waiting", NextIn: dueAt.Sub(now)}
	}
	return proactiveDecision{Fire: true, Reason: "due"}
}

// proactiveMaterial reports whether the pet has anything to talk about: an open
// todo, a timer that is on, or a session finished today.
func (a *App) proactiveMaterial() bool {
	if a.store.Stats().Open > 0 {
		return true
	}
	if a.store.Pomodoro().Enabled {
		return true
	}
	return a.todayFocusCount(time.Now()) > 0
}

// initProactive schedules the first unprompted remark if the feature is on.
//
// It is deliberately lazy: with the feature off (the default) this touches
// nothing, so a build that never uses it never schedules a timer.
func (a *App) initProactive() {
	cfg := a.store.Config()
	if !cfg.LLMProactive {
		return
	}
	a.setNextProactive(time.Now().Add(a.proactiveGap(cfg)))
}

// proactiveGap is the configured minimum gap, with the same fallback the
// decision function applies.
func (a *App) proactiveGap(cfg store.Config) time.Duration {
	gap := time.Duration(cfg.LLMProactiveMinGapMin) * time.Minute
	if gap <= 0 {
		return proactiveMinGapFallback
	}
	return gap
}

// startProactiveLoop drives the gate.
//
// The stop channel is captured once for the same reason the pomodoro loop does:
// a field read inside the loop would race shutdown closing (and then clearing)
// it.
func (a *App) startProactiveLoop() {
	stop := a.remStop
	go func() {
		ticker := time.NewTicker(proactiveCheckEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				a.tickProactive()
			}
		}
	}()
}

// tickProactive evaluates the gate and, when it opens, has the pet speak.
func (a *App) tickProactive() {
	cfg := a.store.Config()
	now := time.Now()

	a.proMu.Lock()
	dueAt := a.proNextAt
	lastSpoke := a.proLastSpoke
	a.proMu.Unlock()

	view := a.llmView()
	decision := decideProactive(
		cfg,
		a.store.Pomodoro(),
		now,
		lastSpoke,
		dueAt,
		desktop.IsForegroundFullscreen(a.windowHandle()),
		view.OverBudget,
		a.panelIsOpen(),
		a.proactiveMaterial(),
		a.bubbleAwaitingAnswer(),
		a.petHidden(),
	)
	if decision.NextIn > 0 {
		a.setNextProactive(now.Add(decision.NextIn))
	}
	if !decision.Fire {
		// Logged every tick on purpose, exactly like the reminder: when the pet
		// says nothing, this line says which gate closed.
		a.DebugLog("proactive: no (" + decision.Reason + ")")
		return
	}
	// A request may already be in flight (the user asked something, or a previous
	// remark is still streaming). Speaking over it would supersede the answer
	// they are waiting for, so the pet waits for the next tick instead.
	a.llm.mu.Lock()
	busy := a.llm.requestID != ""
	a.llm.mu.Unlock()
	if busy {
		a.DebugLog("proactive: no (a request is already in flight)")
		a.setNextProactive(now.Add(proactiveCheckEvery))
		return
	}
	if _, err := a.speakProactive(); err != nil {
		a.DebugLog("proactive: could not start: " + err.Error())
		a.setNextProactive(now.Add(a.proactiveGap(cfg)))
	}
}

// setNextProactive records when the next unprompted remark is due.
func (a *App) setNextProactive(at time.Time) {
	a.proMu.Lock()
	a.proNextAt = at
	a.proMu.Unlock()
}

// noteSpoke records that the pet just said something unprompted.
func (a *App) noteSpoke(at time.Time) {
	a.proMu.Lock()
	a.proLastSpoke = at
	a.proMu.Unlock()
}

// proactivePrompt is the synthetic turn that asks the model to open the
// conversation by itself.
//
// It is sent as the LAST user message but is never persisted: the transcript
// stores what the pet said, not the instruction that made it say anything. That
// is what keeps the panel's record honest - an instruction sitting in the log
// would read as something the user typed.
const proactivePrompt = "（系统消息，不是用户说的）现在轮到你主动开口。请用一两句话自然地跟用户搭话，" +
	"可以关心一下他今天的待办、截止时间或番茄钟进度，也可以只是打个招呼。不要复述这条提示，" +
	"不要用列表，不要超过 60 个字。"

// ProactiveView describes the proactive feature for the settings panel.
type ProactiveView struct {
	Enabled bool `json:"enabled"`
	// NextAt is when the next unprompted remark is due (RFC3339, "" if none).
	NextAt string `json:"nextAt,omitempty"`
	// NextInSec is the same thing as a countdown, for display.
	NextInSec int `json:"nextInSec"`
	// LastSpokeAt is when the pet last spoke unprompted (RFC3339, "" if never).
	LastSpokeAt string `json:"lastSpokeAt,omitempty"`
	// Suppressed explains why the pet will not speak right now, "" when it will.
	Suppressed string `json:"suppressed,omitempty"`
}

// proactiveView renders the feature for the frontend, including the answer to
// "why hasn't it said anything?".
func (a *App) proactiveView() ProactiveView {
	cfg := a.store.Config()
	view := ProactiveView{Enabled: cfg.LLMProactive}
	if !cfg.LLMProactive {
		return view
	}

	a.proMu.Lock()
	nextAt := a.proNextAt
	lastSpoke := a.proLastSpoke
	a.proMu.Unlock()

	now := time.Now()
	if !lastSpoke.IsZero() {
		view.LastSpokeAt = lastSpoke.Format(time.RFC3339)
	}
	if !nextAt.IsZero() {
		view.NextAt = nextAt.Format(time.RFC3339)
		if d := nextAt.Sub(now); d > 0 {
			view.NextInSec = int(d.Seconds())
		}
	}

	st := a.store.Pomodoro()
	rem := cfg.Reminder()
	// llmReadinessNow, NOT llmView: llmView fills in its Proactive field by calling
	// proactiveView, so calling it from here is infinite recursion. With the
	// feature switched on that overflowed the stack on every GetState and killed
	// the process - the app simply disappeared at startup with no error.
	llm := a.llmReadinessNow()
	switch {
	case !cfg.LLMEnabled:
		view.Suppressed = "大模型对话未开启"
	case !llm.Ready:
		view.Suppressed = llm.NotReadyReason
	case llm.OverBudget:
		view.Suppressed = "已达今日上限"
	case st.IsFocusing():
		view.Suppressed = "专注中"
	case desktop.IsForegroundFullscreen(a.windowHandle()):
		view.Suppressed = "全屏中"
	case a.panelIsOpen():
		view.Suppressed = "面板打开中"
	case a.petHidden():
		view.Suppressed = "桌宠已隐藏"
	case a.bubbleAwaitingAnswer():
		view.Suppressed = "有个问题在等你回答"
	case !store.WithinActiveWindow(rem.From, rem.To, now):
		view.Suppressed = "不在活跃时段"
	case !a.proactiveMaterial():
		view.Suppressed = "没有可聊的内容"
	}
	return view
}
