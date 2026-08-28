package godwarf

import (
	"math"
	"strings"

	text "github.com/hajimehoshi/ebiten/v2/text/v2"
)

func measureBubbleWidth(s string, face text.Face) float64 {
	if gf, ok := face.(*text.GoTextFace); ok {
		if gf.Source == nil {
			return float64(len(s)) * (gf.Size * 0.6)
		}
	}
	w, _ := text.Measure(s, face, 0)
	return w
}

func wrapBubbleText(s string, face text.Face, maxWidth float64) (int, []string) {
	var (
		lines   []string
		maxUsed float64
	)
	for _, para := range strings.Split(s, "\n") {
		tokens := strings.SplitAfter(para, " ")
		var builder strings.Builder
		curWidth := 0.0
		for _, tok := range tokens {
			if tok == "" {
				continue
			}
			w := measureBubbleWidth(tok, face)
			if curWidth+w <= maxWidth {
				builder.WriteString(tok)
				curWidth += w
				continue
			}
			if builder.Len() > 0 {
				if curWidth > maxUsed {
					maxUsed = curWidth
				}
				lines = append(lines, builder.String())
				builder.Reset()
				curWidth = 0
			}
			if w <= maxWidth {
				builder.WriteString(tok)
				curWidth = w
				continue
			}
			for _, r := range tok {
				rw := measureBubbleWidth(string(r), face)
				if curWidth+rw > maxWidth && builder.Len() > 0 {
					if curWidth > maxUsed {
						maxUsed = curWidth
					}
					lines = append(lines, builder.String())
					builder.Reset()
					curWidth = 0
				}
				builder.WriteRune(r)
				curWidth += rw
			}
		}
		if curWidth > maxUsed {
			maxUsed = curWidth
		}
		lines = append(lines, builder.String())
	}
	return int(math.Ceil(maxUsed)), lines
}
