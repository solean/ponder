package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/solean/ponder/internal/appstate"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type windowBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Only normal bounds are persisted, so minimising or entering fullscreen doesn't
// replace the size the user chose for the regular desktop window.
type mainWindowState struct {
	mu     sync.Mutex
	path   string
	bounds windowBounds
	timer  *time.Timer
}

func newMainWindowState() *mainWindowState {
	s := &mainWindowState{}
	dir, err := appstate.DefaultSupportDir()
	if err != nil {
		log.Printf("resolve window state directory: %v", err)
		return s
	}
	s.path = filepath.Join(dir, "window-state.json")
	payload, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(payload, &s.bounds); err != nil {
			s.bounds = windowBounds{}
			log.Printf("read window state: %v", err)
		}
	} else if !os.IsNotExist(err) {
		log.Printf("read window state: %v", err)
	}
	return s
}

func (s *mainWindowState) restore(app *application.App, window application.Window) {
	s.mu.Lock()
	saved := s.bounds
	s.mu.Unlock()
	bounds, minWidth, minHeight := fitMainWindow(saved, app.Screen.GetAll())
	window.SetMinSize(minWidth, minHeight)
	window.SetSize(bounds.Width, bounds.Height)
	if len(app.Screen.GetAll()) > 0 {
		window.SetPosition(bounds.X, bounds.Y)
	} else {
		window.Center()
	}
	s.capture(window)
	window.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) { s.capture(window) })
	window.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) { s.capture(window) })
}

// Choose the display with the greatest overlap; if the old display is gone,
// centre on the primary display. All coordinates use Wails' logical units.
func fitMainWindow(saved windowBounds, screens []*application.Screen) (windowBounds, int, int) {
	valid := saved.Width > 0 && saved.Height > 0
	bounds := saved
	if !valid {
		bounds = windowBounds{Width: 1600, Height: 1000}
	}
	var selected *application.Screen
	bestOverlap := int64(0)
	for _, screen := range screens {
		if screen == nil || screen.WorkArea.Width <= 0 || screen.WorkArea.Height <= 0 {
			continue
		}
		if selected == nil || (bestOverlap == 0 && screen.IsPrimary) {
			selected = screen
		}
		if valid {
			area := screen.WorkArea
			width := max(0, min(int64(saved.X)+int64(saved.Width), int64(area.X)+int64(area.Width))-max(int64(saved.X), int64(area.X)))
			height := max(0, min(int64(saved.Y)+int64(saved.Height), int64(area.Y)+int64(area.Height))-max(int64(saved.Y), int64(area.Y)))
			overlap := width * height
			if overlap > bestOverlap {
				selected, bestOverlap = screen, overlap
			}
		}
	}
	if selected == nil {
		return windowBounds{Width: max(1200, bounds.Width), Height: max(760, bounds.Height)}, 1200, 760
	}
	area := selected.WorkArea
	minWidth, minHeight := min(1200, max(1, area.Width*9/10)), min(760, max(1, area.Height*9/10))
	if !valid {
		bounds.Width = min(bounds.Width, area.Width*9/10)
		bounds.Height = min(bounds.Height, area.Height*9/10)
	}
	bounds.Width = min(area.Width, max(minWidth, bounds.Width))
	bounds.Height = min(area.Height, max(minHeight, bounds.Height))
	if !valid || bestOverlap == 0 {
		bounds.X = area.X + (area.Width-bounds.Width)/2
		bounds.Y = area.Y + (area.Height-bounds.Height)/2
	} else {
		bounds.X = max(area.X, min(bounds.X, area.X+area.Width-bounds.Width))
		bounds.Y = max(area.Y, min(bounds.Y, area.Y+area.Height-bounds.Height))
	}
	return bounds, minWidth, minHeight
}

func (s *mainWindowState) capture(window application.Window) {
	// Capture on the UI thread as one operation, rather than reading native
	// window state from a background persistence timer.
	application.InvokeSync(func() {
		if window.IsMinimised() || window.IsMaximised() || window.IsFullscreen() {
			return
		}
		x, y := window.Position()
		width, height := window.Size()
		if width <= 0 || height <= 0 {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.bounds = windowBounds{X: x, Y: y, Width: width, Height: height}
		if s.timer != nil {
			s.timer.Stop()
		}
		s.timer = time.AfterFunc(400*time.Millisecond, s.flush)
	})
}

func (s *mainWindowState) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.path == "" || s.bounds.Width <= 0 || s.bounds.Height <= 0 {
		return
	}
	if err := writeWindowBounds(s.path, s.bounds); err != nil {
		log.Printf("save window state: %v", err)
	}
}

func writeWindowBounds(path string, bounds windowBounds) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(bounds)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".window-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(payload, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
