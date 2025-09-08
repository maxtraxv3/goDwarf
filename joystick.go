package main

import (
	"fmt"
	"strconv"
	"strings"

	"gothoom/eui"

	"github.com/hajimehoshi/ebiten/v2"
)

var joystickWin *eui.WindowData

func makeJoystickWindow() {
	if joystickWin != nil {
		return
	}
	joystickWin = eui.NewWindow()
	joystickWin.Title = "Joystick"
	joystickWin.Closable = true
	joystickWin.Movable = true
	joystickWin.Resizable = true
	joystickWin.AutoSize = true
	joystickWin.NoScroll = true
	joystickWin.SetZone(eui.HZoneCenter, eui.VZoneMiddleTop)

	root := &eui.ItemData{ItemType: eui.ITEM_FLOW, FlowType: eui.FLOW_VERTICAL, Fixed: true}
	root.Size = eui.Point{X: 300, Y: 200}
	joystickWin.AddItem(root)

	ids := ebiten.AppendGamepadIDs(nil)
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = ebiten.GamepadName(id)
	}

	controllerDD, controllerEvents := eui.NewDropdown()
	controllerDD.Options = names
	controllerDD.Size = eui.Point{X: 260, Y: 24}
	root.AddItem(controllerDD)

	axesText, _ := eui.NewText()
	axesText.FontSize = 12
	axesText.Size = eui.Point{X: 260, Y: 24}
	buttonsText, _ := eui.NewText()
	buttonsText.FontSize = 12
	buttonsText.Size = eui.Point{X: 260, Y: 24}

	updateInfo := func(idx int) {
		if idx < 0 || idx >= len(ids) {
			axesText.Text = ""
			buttonsText.Text = ""
			return
		}
		id := ids[idx]
		axesText.Text = fmt.Sprintf("Axes: %d", ebiten.GamepadAxisCount(id))
		buttonsText.Text = fmt.Sprintf("Buttons: %d", ebiten.GamepadButtonCount(id))
	}
	updateInfo(0)
	controllerEvents.Handle = func(ev eui.UIEvent) {
		if ev.Type == eui.EventDropdownSelected {
			updateInfo(ev.Index)
		}
	}

	root.AddItem(axesText)
	root.AddItem(buttonsText)

	enableCB, enableEvents := eui.NewCheckbox()
	enableCB.Text = "Enable Joystick"
	enableCB.Checked = gs.JoystickEnabled
	enableEvents.Handle = func(ev eui.UIEvent) {
		if ev.Type == eui.EventCheckboxChanged {
			gs.JoystickEnabled = ev.Checked
			settingsDirty = true
		}
	}
	root.AddItem(enableCB)

	bindInput, bindEvents := eui.NewInput()
	bindInput.Label = "Binding (action:button)"
	bindInput.Size = eui.Point{X: 260, Y: 24}
	bindEvents.Handle = func(ev eui.UIEvent) {
		if ev.Type == eui.EventInputChanged {
			parts := strings.SplitN(ev.Text, ":", 2)
			if len(parts) == 2 {
				if b, err := strconv.Atoi(parts[1]); err == nil {
					if gs.JoystickBindings == nil {
						gs.JoystickBindings = make(map[string]ebiten.GamepadButton)
					}
					gs.JoystickBindings[parts[0]] = ebiten.GamepadButton(b)
					settingsDirty = true
				}
			}
		}
	}
	root.AddItem(bindInput)

	joystickWin.AddWindow(false)
}
