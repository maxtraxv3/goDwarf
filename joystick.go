package main

import (
	"fmt"
	"strconv"
	"strings"

	"gothoom/eui"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

var (
	joystickWin           *eui.WindowData
	controllerDD          *eui.ItemData
	axesText, buttonsText *eui.ItemData
	bindInput             *eui.ItemData
	joystickIDs           []ebiten.GamepadID
	joystickNames         []string
	selectedJoystick      int
)

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

	joystickIDs = ebiten.AppendGamepadIDs(joystickIDs[:0])
	joystickNames = joystickNames[:0]
	for _, id := range joystickIDs {
		joystickNames = append(joystickNames, ebiten.GamepadName(id))
	}

	controllerDD, controllerEvents := eui.NewDropdown()
	controllerDD.Options = joystickNames
	controllerDD.Size = eui.Point{X: 260, Y: 24}
	controllerEvents.Handle = func(ev eui.UIEvent) {
		if ev.Type == eui.EventDropdownSelected {
			selectedJoystick = ev.Index
		}
	}
	root.AddItem(controllerDD)

	axesText, _ = eui.NewText()
	axesText.FontSize = 12
	axesText.Size = eui.Point{X: 260, Y: 24}
	root.AddItem(axesText)

	buttonsText, _ = eui.NewText()
	buttonsText.FontSize = 12
	buttonsText.Size = eui.Point{X: 260, Y: 24}
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

	bindInput, _ = eui.NewInput()
	bindInput.Label = "Bind Action"
	bindInput.Size = eui.Point{X: 260, Y: 24}
	root.AddItem(bindInput)

	joystickWin.AddWindow(false)
}

func updateJoystickWindow() {
	newIDs := inpututil.AppendJustConnectedGamepadIDs(nil)
	if len(newIDs) > 0 {
		for _, id := range newIDs {
			joystickIDs = append(joystickIDs, id)
			joystickNames = append(joystickNames, ebiten.GamepadName(id))
		}
		if controllerDD != nil {
			controllerDD.Options = joystickNames
			controllerDD.Dirty = true
			joystickWin.Refresh()
		}
	}
	for i := 0; i < len(joystickIDs); {
		id := joystickIDs[i]
		if inpututil.IsGamepadJustDisconnected(id) {
			joystickIDs = append(joystickIDs[:i], joystickIDs[i+1:]...)
			joystickNames = append(joystickNames[:i], joystickNames[i+1:]...)
			if controllerDD != nil {
				controllerDD.Options = joystickNames
				controllerDD.Dirty = true
				if selectedJoystick >= len(joystickIDs) {
					selectedJoystick = len(joystickIDs) - 1
					controllerDD.Selected = selectedJoystick
				}
				joystickWin.Refresh()
			}
			continue
		}
		i++
	}

	if selectedJoystick < 0 || selectedJoystick >= len(joystickIDs) {
		axesText.Text = ""
		buttonsText.Text = ""
		axesText.Dirty = true
		buttonsText.Dirty = true
		return
	}

	id := joystickIDs[selectedJoystick]

	axisCount := ebiten.GamepadAxisCount(id)
	axes := make([]string, axisCount)
	for a := 0; a < axisCount; a++ {
		axes[a] = fmt.Sprintf("%d:%.2f", a, ebiten.GamepadAxisValue(id, a))
	}
	axesText.Text = "Axes: " + strings.Join(axes, " ")
	axesText.Dirty = true

	buttonCount := ebiten.GamepadButtonCount(id)
	pressed := []string{}
	for b := 0; b < buttonCount; b++ {
		if ebiten.IsGamepadButtonPressed(id, ebiten.GamepadButton(b)) {
			pressed = append(pressed, strconv.Itoa(b))
		}
	}
	buttonsText.Text = "Pressed: " + strings.Join(pressed, " ")
	buttonsText.Dirty = true

	if bindInput != nil && bindInput.Focused {
		if action := strings.TrimSpace(bindInput.Text); action != "" {
			btns := inpututil.AppendJustPressedGamepadButtons(id, nil)
			if len(btns) > 0 {
				if gs.JoystickBindings == nil {
					gs.JoystickBindings = make(map[string]ebiten.GamepadButton)
				}
				gs.JoystickBindings[action] = btns[len(btns)-1]
				settingsDirty = true
				bindInput.Text = ""
				bindInput.Dirty = true
				joystickWin.Refresh()
			}
		}
	}
}
