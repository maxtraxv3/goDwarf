//go:build !test

package main

import (
    _ "embed"
    "math"
    "strings"

    "gothoom/eui"

    text "github.com/hajimehoshi/ebiten/v2/text/v2"
)

//go:embed data/help.txt
var helpText string

var helpWin *eui.WindowData
var helpList *eui.ItemData
var helpCols [3]*eui.ItemData
var helpLines []string

func initHelpUI() {
	if helpWin != nil {
		return
	}
    helpWin, helpList, _ = makeTextWindow("Help", eui.HZoneCenter, eui.VZoneMiddleTop, false)
    helpWin.AutoSize = true
    helpLines = strings.Split(strings.ReplaceAll(helpText, "\r\n", "\n"), "\n")
    helpList.FlowType = eui.FLOW_HORIZONTAL
    for i := 0; i < 3; i++ {
        col := &eui.ItemData{ItemType: eui.ITEM_FLOW, FlowType: eui.FLOW_VERTICAL, Fixed: true}
        helpList.AddItem(col)
        helpCols[i] = col
    }
    helpWin.OnResize = func() { rebuildHelpColumns() }
}

func openHelpWindow(anchor *eui.ItemData) {
	if helpWin == nil {
		return
	}
    rebuildHelpColumns()
	if anchor != nil {
		helpWin.MarkOpenNear(anchor)
	} else {
		helpWin.MarkOpen()
	}
    rebuildHelpColumns()
    helpWin.Refresh()
}

func toggleHelpWindow(anchor *eui.ItemData) {
	if helpWin == nil {
		return
	}
	if helpWin.IsOpen() {
		helpWin.Close()
		return
	}
	openHelpWindow(anchor)
}

// rebuildHelpColumns lays out help text into three columns sharing the same scroll.
func rebuildHelpColumns() {
    if helpWin == nil || helpList == nil || helpCols[0] == nil {
        return
    }
    clientW := helpWin.GetSize().X
    clientH := helpWin.GetSize().Y - helpWin.GetTitleSize()
    s := eui.UIScale()
    if helpWin.NoScale { s = 1 }
    pad := (helpWin.Padding + helpWin.BorderPad) * s
    clientWAvail := clientW - 2*pad
    if clientWAvail < 0 { clientWAvail = 0 }
    clientHAvail := clientH - 2*pad
    if clientHAvail < 0 { clientHAvail = 0 }

    helpList.Scrollable = true
    colW := clientWAvail / 3
    for i := 0; i < 3; i++ {
        helpCols[i].Size.X = colW
        helpCols[i].Size.Y = clientHAvail
        helpCols[i].Scrollable = false
        helpCols[i].Contents = helpCols[i].Contents[:0]
    }

    // Font face for sizing
    ui := eui.UIScale()
    facePx := float64(float32(15)*ui) + 2
    var face text.Face
    if monoFaceSource != nil {
        face = &text.GoTextFace{Source: monoFaceSource, Size: facePx}
    } else if src := eui.FontSource(); src != nil {
        face = &text.GoTextFace{Source: src, Size: facePx}
    } else {
        face = &text.GoTextFace{Size: facePx}
    }
    metrics := face.Metrics()
    linePx := math.Ceil(metrics.HAscent + metrics.HDescent + 2)
    rowUnits := float32(linePx) / ui

    sb := eui.ScrollbarWidth()
    contentW := colW - sb
    if contentW < 0 { contentW = 0 }
    wrapW := float64(contentW - 3*pad)

    type section struct{ lines []string; rows int }
    var sections []section
    var cur []string
    for i, ln := range helpLines {
        cur = append(cur, ln)
        if i+1 < len(helpLines) && strings.TrimSpace(helpLines[i+1]) == strings.Repeat("-", len(strings.TrimSpace(ln))) {
            cur = append(cur, helpLines[i+1])
            sections = append(sections, section{lines: cur})
            cur = nil
            continue
        }
        if ln == "" && len(cur) > 0 {
            sections = append(sections, section{lines: cur})
            cur = nil
        }
    }
    if len(cur) > 0 { sections = append(sections, section{lines: cur}) }

    for i := range sections {
        rows := 0
        for _, l := range sections[i].lines {
            _, ls := wrapText(l, face, wrapW)
            if n := len(ls); n > 0 { rows += n } else { rows++ }
        }
        sections[i].rows = rows
    }

    heights := [3]int{}
    for _, sec := range sections {
        idx := 0
        if heights[1] < heights[idx] { idx = 1 }
        if heights[2] < heights[idx] { idx = 2 }
        for _, l := range sec.lines {
            t, _ := eui.NewText()
            _, ls := wrapText(l, face, wrapW)
            wrapped := strings.Join(ls, "\n")
            t.Text = wrapped
            t.FontSize = 15
            t.Face = face
            linesN := len(ls)
            if linesN < 1 { linesN = 1 }
            t.Size = eui.Point{X: contentW, Y: rowUnits * float32(linesN)}
            helpCols[idx].AddItem(t)
        }
        heights[idx] += sec.rows + 1
        sp, _ := eui.NewText()
        sp.Text = ""
        sp.Size = eui.Point{X: contentW, Y: rowUnits * 0.5}
        helpCols[idx].AddItem(sp)
    }

    helpList.Size.X = clientWAvail
    helpList.Size.Y = clientHAvail
}
