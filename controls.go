package godwarf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type ControlType string

const (
	CtrlJoystick     ControlType = "joystick"
	CtrlButton       ControlType = "button"
	CtrlChatPanel    ControlType = "chat_panel"
	CtrlStatusBar    ControlType = "status_bar"
	CtrlDisconnect   ControlType = "disconnect"
	CtrlPlayersList  ControlType = "players_list"
	CtrlInventory    ControlType = "inventory"
	CtrlLabel        ControlType = "label"
)

type ControlElement struct {
	Type     ControlType `json:"type"`
	X        float64     `json:"x"`
	Y        float64     `json:"y"`
	Scale    float64     `json:"scale"`
	Label    string      `json:"label,omitempty"`
	Command  string      `json:"command,omitempty"`
	Visible  *bool       `json:"visible,omitempty"`
	Color    string      `json:"color,omitempty"`
	Width    float64     `json:"width,omitempty"`
	Height   float64     `json:"height,omitempty"`
	Toggle   bool        `json:"toggle,omitempty"`
}

func (e *ControlElement) visible() bool {
	if e.Visible == nil {
		return true
	}
	return *e.Visible
}

type ControlsProfile struct {
	Name         string           `json:"name"`
	Elements     []ControlElement `json:"elements"`
	JoystickX    float64          `json:"joystick_x,omitempty"`
	JoystickY    float64          `json:"joystick_y,omitempty"`
	StatBarX     int              `json:"statbar_x,omitempty"`
	StatBarY     int              `json:"statbar_y,omitempty"`
	StatBarScale float64          `json:"statbar_scale,omitempty"`
	JoySpeed     float64          `json:"joy_speed,omitempty"`
}

type ControlsState struct {
	mu      sync.Mutex
	profile *ControlsProfile
}

func NewControlsState() *ControlsState {
	cs := &ControlsState{}
	cs.profile = DefaultProfile()
	return cs
}

func (cs *ControlsState) Profile() *ControlsProfile {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.profile
}

func (cs *ControlsState) SetProfile(p *ControlsProfile) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.profile = p
}

func (cs *ControlsState) Save(path string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	data, err := json.MarshalIndent(cs.profile, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (cs *ControlsState) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p ControlsProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.profile = &p
	return nil
}

func ControlsPath(dir string) string {
	return filepath.Join(dir, "controls.json")
}

func DefaultProfile() *ControlsProfile {
	return &ControlsProfile{
		Name:      "Default",
		Elements:  []ControlElement{},
		JoystickX: 0.12,
		JoystickY: 0.55,
		StatBarX:  82,
		StatBarY:  6,
	}
}
