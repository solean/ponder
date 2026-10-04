package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestFitMainWindow(t *testing.T) {
	primary := &application.Screen{IsPrimary: true, WorkArea: application.Rect{X: 0, Y: 25, Width: 1920, Height: 1055}}
	secondary := &application.Screen{WorkArea: application.Rect{X: -2560, Y: 0, Width: 2560, Height: 1400}}
	tests := []struct {
		name    string
		saved   windowBounds
		screens []*application.Screen
		want    windowBounds
		minW    int
		minH    int
	}{
		{"first launch", windowBounds{}, []*application.Screen{primary}, windowBounds{160, 78, 1600, 949}, 1200, 760},
		{"restore second display", windowBounds{-2400, 100, 1800, 1100}, []*application.Screen{primary, secondary}, windowBounds{-2400, 100, 1800, 1100}, 1200, 760},
		{"disconnected display", windowBounds{-2400, 100, 1800, 1100}, []*application.Screen{primary}, windowBounds{60, 25, 1800, 1055}, 1200, 760},
		{"partially offscreen", windowBounds{1700, 900, 1400, 800}, []*application.Screen{primary}, windowBounds{520, 280, 1400, 800}, 1200, 760},
		{"small display", windowBounds{}, []*application.Screen{{IsPrimary: true, WorkArea: application.Rect{Width: 1024, Height: 700}}}, windowBounds{51, 35, 921, 630}, 921, 630},
		{"invalid saved size", windowBounds{100, 100, -1, 900}, []*application.Screen{primary}, windowBounds{160, 78, 1600, 949}, 1200, 760},
		{"no displays", windowBounds{}, nil, windowBounds{0, 0, 1600, 1000}, 1200, 760},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, minW, minH := fitMainWindow(tt.saved, tt.screens)
			if got != tt.want || minW != tt.minW || minH != tt.minH {
				t.Fatalf("got %+v, minimum %dx%d; want %+v, minimum %dx%d", got, minW, minH, tt.want, tt.minW, tt.minH)
			}
		})
	}
}

func TestWriteWindowBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "support", "window-state.json")
	for _, bounds := range []windowBounds{{100, 150, 1600, 1000}, {-2000, 50, 1800, 1100}} {
		if err := writeWindowBounds(path, bounds); err != nil {
			t.Fatal(err)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got windowBounds
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatal(err)
		}
		if got != bounds {
			t.Fatalf("got %+v; want %+v", got, bounds)
		}
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary state files remain: %v, %v", files, err)
	}
}
