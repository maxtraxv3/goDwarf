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
    if recorder != nil || playingMovie || recordWhenConnected {
        recordBtn.Text = "STOP"
        // Red theme when actively recording or armed to record.
        if recorder != nil || recordWhenConnected {
            if recordBtn.Theme != nil {
                th := *recordBtn.Theme
                th.Button.Color = eui.ColorDarkRed
                th.Button.HoverColor = eui.ColorRed
                th.Button.ClickColor = eui.ColorLightRed
                recordBtn.Theme = &th
            }
        } else if recordBtnBaseTheme != nil {
            base := *recordBtnBaseTheme
            recordBtn.Theme = &base
        }
    } else {
        recordBtn.Text = "Record"
        if recordBtnBaseTheme != nil {
            base := *recordBtnBaseTheme
            recordBtn.Theme = &base
        }
    }
    recordBtn.Dirty = true
    if hudWin != nil {
        hudWin.Refresh()
    }
}

