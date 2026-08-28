package godwarf

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

var settings struct {
	Name         string  `json:"name"`
	MusicVol     int     `json:"music_vol"`
	SoundVol     int     `json:"sound_vol"`
	Mute         bool    `json:"mute"`
	JoySpeed     float64 `json:"joy_speed"`
	JoyDeadZone  float64 `json:"joy_dead_zone"`
	StatBarScale float64 `json:"statbar_scale"`
	ChatScale    float64 `json:"chat_scale"`
	PlayerScale  float64 `json:"player_scale"`
	InvScale     float64 `json:"inv_scale"`
}

func settingsPath() string {
	return filepath.Join(dataDir(), "settings.json")
}

func loadSettingsMobile() {
	settings.MusicVol = 50
	settings.SoundVol = 80
	settings.Mute = false
	settings.JoySpeed = 1.0
	settings.JoyDeadZone = 0.15
	settings.StatBarScale = 1.0
	settings.ChatScale = 1.0
	settings.PlayerScale = 1.0
	settings.InvScale = 1.0

	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return
	}
	var loaded struct {
		MusicVol     int     `json:"music_vol"`
		SoundVol     int     `json:"sound_vol"`
		Mute         bool    `json:"mute"`
		JoySpeed     float64 `json:"joy_speed"`
		JoyDeadZone  float64 `json:"joy_dead_zone"`
		StatBarScale float64 `json:"statbar_scale"`
		ChatScale    float64 `json:"chat_scale"`
		PlayerScale  float64 `json:"player_scale"`
		InvScale     float64 `json:"inv_scale"`
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		flog(fmt.Sprintf("settings load error: %v", err))
		return
	}
	if loaded.MusicVol != 0 {
		settings.MusicVol = loaded.MusicVol
	}
	if loaded.SoundVol != 0 {
		settings.SoundVol = loaded.SoundVol
	}
	settings.Mute = loaded.Mute
	if loaded.JoySpeed != 0 {
		settings.JoySpeed = loaded.JoySpeed
	}
	if loaded.JoyDeadZone != 0 {
		settings.JoyDeadZone = loaded.JoyDeadZone
	}
	if loaded.StatBarScale != 0 {
		settings.StatBarScale = loaded.StatBarScale
	}
	if loaded.ChatScale != 0 {
		settings.ChatScale = loaded.ChatScale
	}
	if loaded.PlayerScale != 0 {
		settings.PlayerScale = loaded.PlayerScale
	}
	if loaded.InvScale != 0 {
		settings.InvScale = loaded.InvScale
	}
	flog(fmt.Sprintf("settings loaded: joySpeed=%.2f deadZone=%.2f soundVol=%d musicVol=%d",
		settings.JoySpeed, settings.JoyDeadZone, settings.SoundVol, settings.MusicVol))
}

func saveSettingsMobile() {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		flog(fmt.Sprintf("settings save error: %v", err))
		return
	}
	if err := os.WriteFile(settingsPath(), data, 0644); err != nil {
		flog(fmt.Sprintf("settings save error: %v", err))
		return
	}
	flog("settings saved")
}

func dataDir() string {
	if runtime.GOOS == "android" {
		for _, dir := range []string{
			"/data/data/xyz.m45sci.godwarf/files",
			"/data/user/0/xyz.m45sci.godwarf/files",
				filepath.Join(os.TempDir(), "godwarf-data"),
		} {
			if err := os.MkdirAll(dir, 0755); err == nil {
				test := filepath.Join(dir, ".write-test")
				if f, err := os.Create(test); err == nil {
					f.Close()
					os.Remove(test)
					log.Printf("goDwarf: using data dir: %s", dir)
					return dir
				}
			}
		}
		return filepath.Join(os.TempDir(), "godwarf-data")
	}
	exe, err := os.Executable()
	if err != nil {
		return "data"
	}
	return filepath.Join(filepath.Dir(exe), "data")
}
