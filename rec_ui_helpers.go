package main

import (
    "gothoom/eui"
)

// recordWhenConnected arms recording to begin on the first draw-state after
// connecting. This is toggled via the Record/STOP button when disconnected.
var recordWhenConnected bool

// recordBtnBaseTheme stores the initial theme for the record button so it can
// be restored when leaving STOP state.
var recordBtnBaseTheme *eui.Theme

// updateRecordButton updates the toolbar record button label and theme based on
// whether we're recording, armed to record, or playing back a movie.
func updateRecordButton() {
    if recordBtn == nil {
        return
    }
    // Capture the base theme once.
    if recordBtnBaseTheme == nil && recordBtn.Theme != nil {
        base := *recordBtn.Theme
        recordBtnBaseTheme = &base
    }
    // Always red to indicate recording control.
    if recordBtn.Theme != nil {
        th := *recordBtn.Theme
        th.Button.Color = eui.ColorDarkRed
        th.Button.HoverColor = eui.ColorRed
        th.Button.ClickColor = eui.ColorLightRed
        recordBtn.Theme = &th
    }
    if recorder != nil || playingMovie || recordWhenConnected {
        recordBtn.Text = "STOP"
    } else {
        recordBtn.Text = "Record"
    }
    // Force re-render of the button and toolbar window
    recordBtn.Dirty = true
    recordBtn.Render = nil
    if hudWin != nil {
        hudWin.Render = nil
        hudWin.Refresh()
    }
}
