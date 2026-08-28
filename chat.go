package godwarf

import (
	"bytes"
	_ "embed"
	"image/color"
	"log"
	"strings"

	text "github.com/hajimehoshi/ebiten/v2/text/v2"
)

//go:embed data/font/NotoSans-Regular.ttf
var notoSansRegular []byte

//go:embed data/font/NotoSans-Bold.ttf
var notoSansBold []byte

//go:embed data/font/NotoSans-Italic.ttf
var notoSansItalic []byte

//go:embed data/font/NotoSans-BoldItalic.ttf
var notoSansBoldItalic []byte

var bubbleLanguageNames = []string{
	"Halfling", "Sylvan", "People", "Thoom", "Dwarven", "Ghorak Zo",
	"Ancient", "Magic", "Common", "Thieves' Cant", "Mystic", "Monster",
	"unknown language", "Orga", "Sirrush", "Azcatl", "Lepori",
}

var languageWhisperVerb = []string{
	"squeaks softly", "chirps softly", "purrs softly", "hums softly",
	"mumbles", "murmurs", "chants softly", "utters softly",
	"whispers something", "gestures discreetly", "incants softly",
	"growls softly", "sounds softly", "grunts softly", "hisses softly",
	"clacks softly", "nibbles softly",
}

var languageYellVerb = []string{
	"shouts", "calls", "roars", "trumpets", "hollers", "bellows",
	"chants loudly", "utters loudly", "yells something",
	"gestures wildly", "incants loudly", "growls loudly", "shrieks",
	"grunts loudly", "hisses loudly", "rattles", "yelps",
}

func decodeBubble(data []byte) (verb, text, name, lang string, code uint8, bubbleType int, target int) {
	if len(data) < 2 {
		return "", "", "", "", kBubbleCodeKnown, kBubbleNormal, 0
	}
	typ := int(data[1])
	bubbleType = typ & kBubbleTypeMask
	p := 2
	code = kBubbleCodeKnown
	langIdx := -1
	if typ&kBubbleNotCommon != 0 {
		if len(data) < p+1 {
			return "", "", "", "", kBubbleCodeKnown, bubbleType, 0
		}
		b := data[p]
		langIdx = int(b & kBubbleLanguageMask)
		if langIdx >= 0 && langIdx < len(bubbleLanguageNames) {
			lang = bubbleLanguageNames[langIdx]
		}
		code = b & kBubbleCodeMask
		p++
	}
	if typ&kBubbleFar != 0 {
		if len(data) < p+4 {
			return "", "", "", lang, code, bubbleType, 0
		}
		p += 4
	}
	if len(data) <= p {
		return "", "", "", lang, code, bubbleType, 0
	}
	raw := data[p:]
	msgData := stripBEPPTags(append([]byte(nil), raw...))
	if i := bytes.IndexByte(msgData, 0); i >= 0 {
		msgData = msgData[:i]
	}
	lines := bytes.Split(msgData, []byte{'\r'})
	for _, ln := range lines {
		if len(ln) == 0 {
			continue
		}
		s := strings.TrimSpace(decodeMacRoman(ln))
		if s == "" {
			continue
		}
		if isNightCommand(s) {
			continue
		}
		if text == "" {
			text = s
		} else {
			text += " " + s
		}
	}
	if code != kBubbleCodeKnown && bubbleType != kBubbleYell {
		text = ""
	}
	if text == "" && code == kBubbleCodeKnown {
		return "", "", "", lang, code, bubbleType, 0
	}
	switch bubbleType {
	case kBubbleNormal:
		verb = "says"
	case kBubbleWhisper:
		verb = "whispers"
		if typ&kBubbleNotCommon != 0 && langIdx >= 0 && langIdx < len(languageWhisperVerb) && langIdx != kBubbleCommon {
			verb = languageWhisperVerb[langIdx]
		}
	case kBubbleYell:
		verb = "yells"
		if typ&kBubbleNotCommon != 0 && langIdx >= 0 && langIdx < len(languageYellVerb) && langIdx != kBubbleCommon {
			verb = languageYellVerb[langIdx]
		}
	case kBubbleThought:
		verb = "thinks"
		idx := strings.IndexByte(text, ':')
		if idx >= 0 {
			name = strings.TrimSpace(text[:idx])
			text = strings.TrimSpace(text[idx+1:])
		} else {
			name = "Someone"
		}
	case kBubbleRealAction:
		// no verb for real actions
	case kBubbleMonster:
		verb = "growls"
	case kBubblePlayerAction:
		// parenthesized action, no verb
	case kBubblePonder:
		verb = "ponders"
	case kBubbleNarrate:
		// narrate bubbles have no verb
	}
	return verb, text, name, lang, code, bubbleType, 0
}

// ChatMessage holds a text line with its display color.
type ChatMessage struct {
	Text  string
	Color color.RGBA
}

// CL message class colors — from TextStyles.plist (0-255 RGBA).
// Speech is white instead of the original black so it's visible on our dark
// chat panel background.
var (
	colDefault      = color.RGBA{R: 255, G: 255, B: 255, A: 255} // info/system — white
	colLogon        = color.RGBA{R: 120, G: 160, B: 255, A: 255} // bright blue
	colLogoff       = color.RGBA{R: 120, G: 160, B: 255, A: 255} // bright blue
	colShare        = color.RGBA{R: 0, G: 220, B: 220, A: 255}   // bright teal
	colHost         = color.RGBA{R: 255, G: 110, B: 110, A: 255} // light red
	colObit         = color.RGBA{R: 220, G: 130, B: 130, A: 255} // light maroon
	colSpeech       = color.RGBA{R: 255, G: 255, B: 255, A: 255} // white (was black in CL)
	colFriendSpeech = color.RGBA{R: 110, G: 255, B: 110, A: 255} // bright green
	colMySpeech     = color.RGBA{R: 255, G: 120, B: 200, A: 255} // light pink/purple
	colBubble       = color.RGBA{R: 200, G: 200, B: 200, A: 255} // light gray
	colFriendBubble = color.RGBA{R: 130, G: 150, B: 255, A: 255} // light navy
	colThought      = color.RGBA{R: 150, G: 255, B: 90, A: 255}  // light olive green
	colTimeStamp    = color.RGBA{R: 160, G: 160, B: 255, A: 255} // lavender
	colYouKilled    = color.RGBA{R: 220, G: 130, B: 130, A: 255} // light maroon (same as obit)
)

var (
	chatFace         text.Face
	chatFaceBold     text.Face
	chatFaceItalic   text.Face
	chatFaceBoldItalic text.Face
	chatFaceSize     float64
)

const chatFontSize = 12

func initChatFont() {
	if chatFace != nil {
		return
	}
	src, err := text.NewGoTextFaceSource(bytes.NewReader(notoSansRegular))
	if err != nil {
		log.Fatalf("chat font: %v", err)
	}
	chatFaceSize = chatFontSize
	chatFace = &text.GoTextFace{
		Source: src,
		Size:   chatFaceSize,
	}
	boldSrc, err := text.NewGoTextFaceSource(bytes.NewReader(notoSansBold))
	if err != nil {
		log.Fatalf("bold font: %v", err)
	}
	chatFaceBold = &text.GoTextFace{
		Source: boldSrc,
		Size:   chatFaceSize,
	}
	italicSrc, err := text.NewGoTextFaceSource(bytes.NewReader(notoSansItalic))
	if err != nil {
		log.Fatalf("italic font: %v", err)
	}
	chatFaceItalic = &text.GoTextFace{
		Source: italicSrc,
		Size:   chatFaceSize,
	}
	biSrc, err := text.NewGoTextFaceSource(bytes.NewReader(notoSansBoldItalic))
	if err != nil {
		log.Fatalf("bold italic font: %v", err)
	}
	chatFaceBoldItalic = &text.GoTextFace{
		Source: biSrc,
		Size:   chatFaceSize,
	}
}

// classifyBEPP extracts a 2-char BEPP tag from the start of raw text,
// returns the cleaned text and the corresponding color.
// The tag is stripped along with any other inline BEPP tags.
func classifyBEPP(raw []byte) (string, color.RGBA) {
	if len(raw) == 0 {
		return "", colDefault
	}
	// Check for host/announce prefixes before BEPP
	if len(raw) > 0 && (raw[0] == '[' || raw[0] == '*' || raw[0] == 0xA5) {
		cleaned := stripBEPPTags(append([]byte(nil), raw...))
		return strings.TrimSpace(decodeMacRoman(cleaned)), colHost
	}
	// Check for \xC2 tag prefix
	if raw[0] == 0xC2 && len(raw) >= 3 {
		tag := string(raw[1:3])
		rest := raw[3:]
		if i := bytes.IndexByte(rest, 0); i >= 0 {
			rest = rest[:i]
		}
		cleaned := stripBEPPTags(append([]byte(nil), rest...))
		text := strings.TrimSpace(decodeMacRoman(cleaned))
		return text, colorForBEPPTag(tag)
	}
	// No tag — plain info text
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	cleaned := stripBEPPTags(append([]byte(nil), raw...))
	return strings.TrimSpace(decodeMacRoman(cleaned)), colDefault
}

// colorForBEPPTag maps a 2-char BEPP tag to the CL message class color.
func colorForBEPPTag(tag string) color.RGBA {
	switch tag {
	case "lo", "lg": // logon
		return colLogon
	case "lf": // logoff
		return colLogoff
	case "er": // error/not in lands (treated as logoff)
		return colLogoff
	case "sh", "su": // share/unshare
		return colShare
	case "hf", "nf": // fallen
		return colObit
	case "wh": // who
		return colDefault
	case "yk": // you killed
		return colYouKilled
	case "ka", "kr": // karma
		return colDefault
	case "ba": // bard level
		return colDefault
	case "in", "hp": // info/help
		return colDefault
	case "th": // thinkto (my speech)
		return colMySpeech
	case "tt": // mono style
		return colDefault
	default:
		return colDefault
	}
}

// colorForBubbleType maps a bubble type code to the CL chat color.
func colorForBubbleType(bubbleType int) color.RGBA {
	switch bubbleType {
	case 3: // thought
		return colThought
	default:
		return colSpeech
	}
}

// classifyInfoText splits raw info text on \r, classifies each line by its
// BEPP prefix, and returns regular chat messages separately from raw \xC2be
// backend responses (which must be processed by handleBackendResponse).
func classifyInfoText(raw []byte) ([]ChatMessage, [][]byte) {
	if len(raw) == 0 {
		return nil, nil
	}
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	lines := strings.Split(string(raw), "\r")
	var msgs []ChatMessage
	var backend [][]byte
	for _, line := range lines {
		lineBytes := []byte(line)
		if len(lineBytes) == 0 {
			continue
		}
		// Route \xC2be backend responses to backend handler
		if len(lineBytes) >= 3 && lineBytes[0] == 0xC2 && lineBytes[1] == 'b' && lineBytes[2] == 'e' {
			backend = append(backend, lineBytes)
			continue
		}
		text, c := classifyBEPP(lineBytes)
		if text == "" {
			continue
		}
		if isNightCommand(text) {
			continue
		}
		msgs = append(msgs, ChatMessage{Text: text, Color: c})
	}
	return msgs, backend
}

// cleanInfoTextClassified splits raw info text on \r, classifies each line
// by its BEPP prefix, strips tags, and returns typed chat messages.
// Lines that are night commands or empty are skipped.
func cleanInfoTextClassified(raw []byte) []ChatMessage {
	if len(raw) == 0 {
		return nil
	}
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	lines := strings.Split(string(raw), "\r")
	var out []ChatMessage
	for _, line := range lines {
		lineBytes := []byte(line)
		if len(lineBytes) == 0 {
			continue
		}
		text, c := classifyBEPP(lineBytes)
		if text == "" {
			continue
		}
		if isNightCommand(text) {
			continue
		}
		out = append(out, ChatMessage{Text: text, Color: c})
	}
	return out
}
