package godwarf

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var scrambleKey = "3k6XsAgldtz1vRw3e9WpfUtXQdKQO4P7"

type Character struct {
	Name      string `json:"name"`
	Key       string `json:"key"`
	PictID    uint16 `json:"pict,omitempty"`
	ColorsHex string `json:"colors,omitempty"`
	Colors    []byte `json:"-"`
}

type charactersFile struct {
	Version    int         `json:"version"`
	Characters []Character `json:"characters"`
}

var (
	charactersMu sync.Mutex
	characters   []Character
)

func scrambleStr(name, s string) string {
	b := []byte(s)
	k := []byte(scrambleKey + name)
	for i := range b {
		b[i] ^= k[i%len(k)]
	}
	return hex.EncodeToString(b)
}

func unscrambleStr(name, h string) string {
	b, err := hex.DecodeString(h)
	if err != nil {
		return ""
	}
	k := []byte(scrambleKey + name)
	for i := range b {
		b[i] ^= k[i%len(k)]
	}
	return string(b)
}

func loadCharacters() {
	path := filepath.Join(dataDir(), "characters.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cf charactersFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return
	}
	if cf.Version < 1 {
		return
	}
	charactersMu.Lock()
	defer charactersMu.Unlock()
	for _, c := range cf.Characters {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" {
			continue
		}
		c.Colors = decodeHexColors(c.ColorsHex)
		characters = append(characters, c)
	}
}

func saveCharactersFile() {
	persisted := make([]Character, 0, len(characters))
	for _, c := range characters {
		if c.Name == "" {
			continue
		}
		pc := c
		pc.ColorsHex = encodeHexColors(c.Colors)
		persisted = append(persisted, pc)
	}
	cf := charactersFile{Version: 1, Characters: persisted}
	data, _ := json.MarshalIndent(cf, "", "  ")
	os.WriteFile(filepath.Join(dataDir(), "characters.json"), data, 0644)
}

func saveCharacter(name, pass string, pictID uint16, colors []byte) {
	charactersMu.Lock()
	defer charactersMu.Unlock()

	found := false
	for i := range characters {
		if strings.EqualFold(characters[i].Name, name) {
			characters[i].PictID = pictID
			characters[i].Colors = colors
			characters[i].Key = scrambleStr(name, pass)
			found = true
			break
		}
	}
	if !found {
		characters = append(characters, Character{
			Name:   name,
			Key:    scrambleStr(name, pass),
			PictID: pictID,
			Colors: colors,
		})
	}
	saveCharactersFile()
}

func updateCharacterAppearance(name string, pictID uint16, colors []byte) {
	charactersMu.Lock()
	defer charactersMu.Unlock()
	found := false
	for i := range characters {
		if strings.EqualFold(characters[i].Name, name) {
			changed := false
			if pictID != 0 && characters[i].PictID != pictID {
				characters[i].PictID = pictID
				changed = true
			}
			if len(colors) > 0 && !bytes.Equal(characters[i].Colors, colors) {
				characters[i].Colors = colors
				changed = true
			}
			found = true
			if changed {
				saveCharactersFile()
			}
			return
		}
	}
	if !found {
		characters = append(characters, Character{
			Name:   name,
			PictID: pictID,
			Colors: colors,
		})
	}
	saveCharactersFile()
}

func removeCharacter(name string) {
	charactersMu.Lock()
	defer charactersMu.Unlock()
	for i := range characters {
		if strings.EqualFold(characters[i].Name, name) {
			characters = append(characters[:i], characters[i+1:]...)
			break
		}
	}
	saveCharactersFile()
}

func getCharacterPass(name string) string {
	charactersMu.Lock()
	defer charactersMu.Unlock()
	for _, c := range characters {
		if strings.EqualFold(c.Name, name) {
			return unscrambleStr(c.Name, c.Key)
		}
	}
	return ""
}

func getCharacters() []Character {
	charactersMu.Lock()
	defer charactersMu.Unlock()
	out := make([]Character, len(characters))
	copy(out, characters)
	return out
}

func encodeHexColors(colors []byte) string {
	if len(colors) == 0 {
		return ""
	}
	buf := make([]byte, 1+len(colors))
	buf[0] = byte(len(colors))
	copy(buf[1:], colors)
	return hex.EncodeToString(buf)
}

func decodeHexColors(h string) []byte {
	if h == "" {
		return nil
	}
	b, err := hex.DecodeString(h)
	if err != nil || len(b) < 2 {
		return nil
	}
	cnt := int(b[0])
	if cnt > len(b)-1 {
		return nil
	}
	return b[1 : 1+cnt]
}
