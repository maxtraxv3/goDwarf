package main

import (
	"bytes"
	"encoding/binary"
	"sort"
)

const sentinel uint32 = 0xFFFFFFFF

// synthesizeInitialGameState builds a GameState payload that embeds a
// PictureTable followed by a Mobile/Descriptor table using the current
// in-memory draw state. This primes playback when recording starts mid-session.
func synthesizeInitialGameState(version uint16) []byte {
	stateMu.Lock()
	pics := append([]framePicture(nil), state.pictures...)
	// Collect union of descriptor/mobile indices for stable output
	idxSet := map[uint8]struct{}{}
	for k := range state.descriptors {
		idxSet[k] = struct{}{}
	}
	for k := range state.mobiles {
		idxSet[k] = struct{}{}
	}
	var indices []int
	for k := range idxSet {
		indices = append(indices, int(k))
	}
	sort.Ints(indices)

	// Snapshot maps
	descs := make(map[uint8]frameDescriptor, len(state.descriptors))
	for k, v := range state.descriptors {
		descs[k] = v
	}
	mobs := make(map[uint8]frameMobile, len(state.mobiles))
	for k, v := range state.mobiles {
		mobs[k] = v
	}
	stateMu.Unlock()

	var buf bytes.Buffer

	// PictureTable: count(uint16) + entries + 4-byte trailer
	if len(pics) > 0 {
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(pics)))
		for _, p := range pics {
			_ = binary.Write(&buf, binary.BigEndian, p.PictID)
			_ = binary.Write(&buf, binary.BigEndian, uint16(p.H))
			_ = binary.Write(&buf, binary.BigEndian, uint16(p.V))
		}
		// Trailer (observed 4 bytes); legacy client uses 0xFFFFFFFF
		_ = binary.Write(&buf, binary.BigEndian, sentinel)
	}

	// Mobile/Descriptor table:
	// Match parseMobileTable layouts for version breakpoints.
	type layout struct{ descSize, colorsOffset, nameOffset, numColorsOffset, bubbleCounterOffset int }
	var l layout
	switch {
	case version > 141:
		l = layout{descSize: 156, colorsOffset: 56, nameOffset: 86, numColorsOffset: 48, bubbleCounterOffset: 28}
	case version > 113:
		l = layout{descSize: 150, colorsOffset: 52, nameOffset: 82, numColorsOffset: 44, bubbleCounterOffset: 24}
	case version > 105:
		l = layout{descSize: 142, colorsOffset: 52, nameOffset: 82, numColorsOffset: 44, bubbleCounterOffset: 24}
	case version > 97:
		l = layout{descSize: 130, colorsOffset: 40, nameOffset: 70, numColorsOffset: 32, bubbleCounterOffset: 24}
	default:
		l = layout{descSize: 126, colorsOffset: 36, nameOffset: 66, numColorsOffset: 28, bubbleCounterOffset: 20}
	}

	const descTableSize = 266
	if len(indices) > 0 {
		for _, iv := range indices {
			idx := uint8(iv)
			// Write index: descriptor-only entries are encoded as idx+descTableSize
			m, hasMob := mobs[idx]
			encIdx := uint32(idx)
			if !hasMob {
				encIdx = uint32(int(idx) + descTableSize)
			}
			_ = binary.Write(&buf, binary.BigEndian, encIdx)
			if hasMob {
				// state, H, V, Colors as uint32
				_ = binary.Write(&buf, binary.BigEndian, uint32(m.State))
				_ = binary.Write(&buf, binary.BigEndian, uint32(uint16(m.H)))
				_ = binary.Write(&buf, binary.BigEndian, uint32(uint16(m.V)))
				_ = binary.Write(&buf, binary.BigEndian, uint32(m.Colors))
			} else {
				// No mobile: do not emit mobile slot bytes for descriptor-only entries
			}

			// Descriptor buffer
			d, ok := descs[idx]
			if !ok {
				d = frameDescriptor{Index: idx}
			}
			desc := make([]byte, l.descSize)
			// pict at 0:4
			binary.BigEndian.PutUint32(desc[0:4], uint32(d.PictID))
			// type at 16:20
			binary.BigEndian.PutUint32(desc[16:20], uint32(d.Type))
			// numColors and colors
			ncol := len(d.Colors)
			if ncol > 30 {
				ncol = 30
			}
			binary.BigEndian.PutUint32(desc[l.numColorsOffset:l.numColorsOffset+4], uint32(ncol))
			copy(desc[l.colorsOffset:], d.Colors[:ncol])
			// name (null-terminated, max 48 bytes)
			nameBytes := []byte(d.Name)
			if len(nameBytes) > 47 {
				nameBytes = nameBytes[:47]
			}
			copy(desc[l.nameOffset:], nameBytes)
			if l.nameOffset+len(nameBytes) < len(desc) {
				desc[l.nameOffset+len(nameBytes)] = 0
			}
			// bubbleCounter = 0 (no extra bubble text)
			binary.BigEndian.PutUint32(desc[l.bubbleCounterOffset:l.bubbleCounterOffset+4], 0)
			buf.Write(desc)
		}
		// Sentinel -1
		_ = binary.Write(&buf, binary.BigEndian, sentinel)
	}

	return buf.Bytes()
}

// synthesizePictureTableFromState builds only a PictureTable payload
// equivalent to the one embedded in GameState blocks.
func synthesizePictureTableFromState() []byte {
	stateMu.Lock()
	pics := append([]framePicture(nil), state.pictures...)
	stateMu.Unlock()
	if len(pics) == 0 {
		return nil
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(pics)))
	for _, p := range pics {
		_ = binary.Write(&buf, binary.BigEndian, p.PictID)
		_ = binary.Write(&buf, binary.BigEndian, uint16(p.H))
		_ = binary.Write(&buf, binary.BigEndian, uint16(p.V))
	}
	// Trailer matches synthesizeInitialGameState
	_ = binary.Write(&buf, binary.BigEndian, sentinel)
	return buf.Bytes()
}

// synthesizeMobileDataFromState builds only the Mobile/Descriptor table
// payload expected by parseMobileTable.
func synthesizeMobileDataFromState(version uint16) []byte {
	// Collect union of indices
	stateMu.Lock()
	idxSet := map[uint8]struct{}{}
	for k := range state.descriptors {
		idxSet[k] = struct{}{}
	}
	for k := range state.mobiles {
		idxSet[k] = struct{}{}
	}
	var indices []int
	for k := range idxSet {
		indices = append(indices, int(k))
	}
	sort.Ints(indices)
	descs := make(map[uint8]frameDescriptor, len(state.descriptors))
	for k, v := range state.descriptors {
		descs[k] = v
	}
	mobs := make(map[uint8]frameMobile, len(state.mobiles))
	for k, v := range state.mobiles {
		mobs[k] = v
	}
	stateMu.Unlock()
	if len(indices) == 0 {
		return nil
	}

	type layout struct{ descSize, colorsOffset, nameOffset, numColorsOffset, bubbleCounterOffset int }
	var l layout
	switch {
	case version > 141:
		l = layout{descSize: 156, colorsOffset: 56, nameOffset: 86, numColorsOffset: 48, bubbleCounterOffset: 28}
	case version > 113:
		l = layout{descSize: 150, colorsOffset: 52, nameOffset: 82, numColorsOffset: 44, bubbleCounterOffset: 24}
	case version > 105:
		l = layout{descSize: 142, colorsOffset: 52, nameOffset: 82, numColorsOffset: 44, bubbleCounterOffset: 24}
	case version > 97:
		l = layout{descSize: 130, colorsOffset: 40, nameOffset: 70, numColorsOffset: 32, bubbleCounterOffset: 24}
	default:
		l = layout{descSize: 126, colorsOffset: 36, nameOffset: 66, numColorsOffset: 28, bubbleCounterOffset: 20}
	}
	const descTableSize = 266

	var buf bytes.Buffer
	for _, iv := range indices {
		idx := uint8(iv)
		if m, ok := mobs[idx]; ok {
			_ = binary.Write(&buf, binary.BigEndian, uint32(idx))
			_ = binary.Write(&buf, binary.BigEndian, uint32(m.State))
			_ = binary.Write(&buf, binary.BigEndian, uint32(uint16(m.H)))
			_ = binary.Write(&buf, binary.BigEndian, uint32(uint16(m.V)))
			_ = binary.Write(&buf, binary.BigEndian, uint32(m.Colors))
		} else {
			_ = binary.Write(&buf, binary.BigEndian, uint32(int(idx)+descTableSize))
		}
		d := descs[idx]
		desc := make([]byte, l.descSize)
		binary.BigEndian.PutUint32(desc[0:4], uint32(d.PictID))
		binary.BigEndian.PutUint32(desc[16:20], uint32(d.Type))
		ncol := len(d.Colors)
		if ncol > 30 {
			ncol = 30
		}
		binary.BigEndian.PutUint32(desc[l.numColorsOffset:l.numColorsOffset+4], uint32(ncol))
		copy(desc[l.colorsOffset:], d.Colors[:ncol])
		nameBytes := []byte(d.Name)
		if len(nameBytes) > 47 {
			nameBytes = nameBytes[:47]
		}
		copy(desc[l.nameOffset:], nameBytes)
		if l.nameOffset+len(nameBytes) < len(desc) {
			desc[l.nameOffset+len(nameBytes)] = 0
		}
		binary.BigEndian.PutUint32(desc[l.bubbleCounterOffset:l.bubbleCounterOffset+4], 0)
		buf.Write(desc)
	}
	_ = binary.Write(&buf, binary.BigEndian, uint32(0xFFFFFFFF))
	return buf.Bytes()
}
