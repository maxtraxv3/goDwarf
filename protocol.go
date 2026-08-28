package godwarf

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
	"godwarf/internal/twofish"
)

const (
	kPIMDownField = 0x0001
)

func simpleEncrypt(data []byte) {
	key := []byte{0x3c, 0x5a, 0x69, 0x93, 0xa5, 0xc6}
	j := 0
	for i := range data {
		data[i] ^= key[j]
		j++
		if j == len(key) {
			j = 0
		}
	}
}

func encodeMacRoman(s string) []byte {
	b, err := charmap.Macintosh.NewEncoder().Bytes([]byte(s))
	if err != nil {
		return []byte(s)
	}
	return b
}

func decodeMacRoman(b []byte) string {
	s, err := charmap.Macintosh.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(s)
}

func encodeFullVersion(v int) uint32 { return uint32(v) << 8 }

func answerChallenge(password string, challenge []byte) ([]byte, error) {
	digest := md5.Sum([]byte(password))
	key := make([]byte, len(digest))
	copy(key, digest[:])
	swapped := make([]byte, len(key))
	for i := 0; i < len(key); i += 4 {
		v := binary.BigEndian.Uint32(key[i : i+4])
		binary.LittleEndian.PutUint32(swapped[i:i+4], v)
	}
	block, err := twofish.NewCipher(swapped)
	if err != nil {
		return nil, err
	}
	if len(challenge)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid challenge length")
	}
	plain := make([]byte, len(challenge))
	for i := 0; i < len(challenge); i += block.BlockSize() {
		block.Decrypt(plain[i:i+block.BlockSize()], challenge[i:i+block.BlockSize()])
	}
	h := md5.Sum(plain)
	encoded := make([]byte, len(h))
	for i := 0; i < len(h); i += block.BlockSize() {
		block.Encrypt(encoded[i:i+block.BlockSize()], h[i:i+block.BlockSize()])
	}
	return encoded, nil
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func sendTCPMessage(connection net.Conn, payload []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(payload)))
	if err := writeAll(connection, size[:]); err != nil {
		return err
	}
	return writeAll(connection, payload)
}

func readTCPMessage(connection net.Conn) ([]byte, error) {
	var sizeBuf [2]byte
	if _, err := io.ReadFull(connection, sizeBuf[:]); err != nil {
		return nil, err
	}
	sz := binary.BigEndian.Uint16(sizeBuf[:])
	buf := make([]byte, sz)
	if _, err := io.ReadFull(connection, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func sendUDPMessage(connection net.Conn, payload []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(payload)))
	totalLen := 2 + len(payload)
	frame := make([]byte, totalLen)
	frame[0] = size[0]
	frame[1] = size[1]
	copy(frame[2:], payload)
	return writeAll(connection, frame)
}

var udpBuffer []byte
var udpReadBuf = make([]byte, 65535)

func readUDPMessage(connection net.Conn) ([]byte, error) {
	for {
		if len(udpBuffer) >= 2 {
			sz := int(binary.BigEndian.Uint16(udpBuffer[:2]))
			if len(udpBuffer) >= 2+sz {
				msg := append([]byte(nil), udpBuffer[2:2+sz]...)
				udpBuffer = udpBuffer[2+sz:]
				return msg, nil
			}
		}
		n, err := connection.Read(udpReadBuf)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, fmt.Errorf("short udp packet")
		}
		udpBuffer = append(udpBuffer, udpReadBuf[:n]...)
	}
}

func sendClientIdentifiers(connection net.Conn, clVersion, imagesVersion, soundsVersion uint32) error {
	const kMsgIdentifiers = 19
	uname := "godwarf"
	hname := "mobile"
	boot := "/"
	unameBytes := encodeMacRoman(uname)
	hnameBytes := encodeMacRoman(hname)
	bootBytes := encodeMacRoman(boot)

	payloadLen := 16 + 8 + 6 + len(unameBytes) + 1 + len(hnameBytes) + 1 + len(bootBytes) + 1 + 1
	packet := make([]byte, payloadLen)

	binary.BigEndian.PutUint16(packet[0:2], kMsgIdentifiers)
	binary.BigEndian.PutUint16(packet[2:4], 0)
	binary.BigEndian.PutUint32(packet[4:8], clVersion)
	binary.BigEndian.PutUint32(packet[8:12], imagesVersion)
	binary.BigEndian.PutUint32(packet[12:16], soundsVersion)
	offset := 16
	offset += 14
	copy(packet[offset:], unameBytes)
	offset += len(unameBytes)
	packet[offset] = 0
	offset++
	copy(packet[offset:], hnameBytes)
	offset += len(hnameBytes)
	packet[offset] = 0
	offset++
	copy(packet[offset:], bootBytes)
	offset += len(bootBytes)
	packet[offset] = 0
	offset++
	packet[offset] = 0

	simpleEncrypt(packet[16:])
	return sendTCPMessage(connection, packet)
}

func sendPlayerInput(connection net.Conn, mouseX, mouseY int16, mouseDown bool, command string, ackFrame, resendFrame, commandNum uint32) error {
	const kMsgPlayerInput = 3
	flags := uint16(0)
	if mouseDown {
		flags = kPIMDownField
	}
	var cmdBytes []byte
	if command != "" {
		cmdBytes = encodeMacRoman(command)
	}
	packetLen := 20 + len(cmdBytes) + 1
	packet := make([]byte, packetLen)
	binary.BigEndian.PutUint16(packet[0:2], kMsgPlayerInput)
	binary.BigEndian.PutUint16(packet[2:4], uint16(mouseX))
	binary.BigEndian.PutUint16(packet[4:6], uint16(mouseY))
	binary.BigEndian.PutUint16(packet[6:8], flags)
	binary.BigEndian.PutUint32(packet[8:12], ackFrame)
	binary.BigEndian.PutUint32(packet[12:16], resendFrame)
	binary.BigEndian.PutUint32(packet[16:20], commandNum)
	copy(packet[20:], cmdBytes)
	packet[20+len(cmdBytes)] = 0
	if command != "" {
		flog(fmt.Sprintf("sendPlayerInput: hex %x cmd=%q cNum=%d", packet, command, commandNum))
	}
	return sendUDPMessage(connection, packet)
}

// --- Draw state parsing ---

type frameDescriptor struct {
	Index  uint8
	Type   uint8
	PictID uint16
	Name   string
	Colors []byte
}

type framePicture struct {
	PictID uint16
	H      int16
	V      int16
	Plane  int
}

type frameMobile struct {
	Index  uint8
	State  uint8
	H      int16
	V      int16
	Colors byte
}

type PlayerInfo struct {
	Name   string
	PictID uint16
	Colors []byte
	State  uint8
}

// Bubble represents an active speech/thought bubble above a mobile.
type Bubble struct {
	DescIndex uint8
	H, V      int16
	Far       bool
	NoArrow   bool
	Type      int
	Text      string
	Expires   time.Time
	WordCount int
}

type drawState struct {
	ackCmdNum   uint8
	ack         uint32
	resend      uint32
	descriptors []frameDescriptor
	descMap     map[uint8]frameDescriptor
	pictures    []framePicture
	mobiles     []frameMobile
	sounds      []uint16
	inventory   []inventoryItem
	chatMsgs    []ChatMessage
	bubbles     []Bubble
	backendInfo [][]byte // raw \xC2be backend responses extracted from info text
	hp, hpMax   int
	sp, spMax   int
	bal, balMax int
	lighting    byte
}

type inventoryItem struct {
	id       uint16
	pictID   uint16
	name     string
	base     string
	extra    string
	equipped bool
	quantity int
	slot     int
	idIndex  int // per-ID index from server (-1 if none)
}

func signExtend(val int, bits int) int16 {
	mask := int16(1) << uint(bits-1)
	return int16((val &^ int(mask)) - (val & int(mask)))
}

type bitReader struct {
	data  []byte
	bitPos int
}

func (br *bitReader) readBits(n int) (int, bool) {
	result := 0
	for i := 0; i < n; i++ {
		byteIdx := br.bitPos / 8
		if byteIdx >= len(br.data) {
			return 0, false
		}
		bitIdx := 7 - (br.bitPos % 8)
		result = (result << 1) | int((br.data[byteIdx]>>uint(bitIdx))&1)
		br.bitPos++
	}
	return result, true
}

func parseDrawStateData(data []byte, existingInv []inventoryItem) (*drawState, int, error) {
	if len(data) < 9 {
		return nil, 0, fmt.Errorf("header too short")
	}
	ds := &drawState{}
	ds.ackCmdNum = data[0]
	ds.ack = binary.BigEndian.Uint32(data[1:5])
	ds.resend = binary.BigEndian.Uint32(data[5:9])
	p := 9
	pictAgain := 0

	if len(data) <= p {
		return ds, 0, nil
	}
	descCount := int(data[p])
	p++

	for i := 0; i < descCount && p < len(data); i++ {
		if p+4 > len(data) {
			flog(fmt.Sprintf("parseDrawState: descriptor %d/%d truncated (p=%d len=%d)", i, descCount, p, len(data)))
			break
		}
		d := frameDescriptor{}
		d.Index = data[p]
		d.Type = data[p+1]
		d.PictID = binary.BigEndian.Uint16(data[p+2:])
		p += 4
		if idx := bytes.IndexByte(data[p:], 0); idx >= 0 {
			d.Name = string(decodeMacRoman(data[p : p+idx]))
			p += idx + 1
		} else {
			flog(fmt.Sprintf("parseDrawState: descriptor %d name missing null (p=%d len=%d)", i, p, len(data)))
			break
		}
		if p >= len(data) {
			flog(fmt.Sprintf("parseDrawState: descriptor %d colors missing (p=%d len=%d)", i, p, len(data)))
			break
		}
		cnt := int(data[p])
		p++
		if p+cnt > len(data) {
			flog(fmt.Sprintf("parseDrawState: descriptor %d colors truncated (p=%d cnt=%d len=%d)", i, p, cnt, len(data)))
			break
		}
		d.Colors = make([]byte, cnt)
		copy(d.Colors, data[p:p+cnt])
		p += cnt
		ds.descriptors = append(ds.descriptors, d)
	}

	if len(data) < p+7 {
		return ds, 0, nil
	}
	ds.hp = int(data[p])
	ds.hpMax = int(data[p+1])
	ds.sp = int(data[p+2])
	ds.spMax = int(data[p+3])
	ds.bal = int(data[p+4])
	ds.balMax = int(data[p+5])
	ds.lighting = data[p+6]
	p += 7

	if len(data) <= p {
		return ds, 0, nil
	}
	pictCount := int(data[p])
	p++
	if pictCount == 255 {
		if len(data) < p+2 {
			return ds, 0, nil
		}
		pictAgain = int(data[p])
		p++
		pictCount = int(data[p])
		p++
	}

	br := bitReader{data: data[p:]}
	for i := 0; i < pictCount; i++ {
		idBits, ok := br.readBits(14)
		if !ok {
			break
		}
		hBits, ok := br.readBits(11)
		if !ok {
			break
		}
		vBits, ok := br.readBits(11)
		if !ok {
			break
		}
		ds.pictures = append(ds.pictures, framePicture{
			PictID: uint16(idBits),
			H:      signExtend(hBits, 11),
			V:      signExtend(vBits, 11),
		})
	}
	p += br.bitPos / 8
	if br.bitPos%8 != 0 {
		p++
	}

	if len(data) <= p {
		return ds, 0, nil
	}
	mobileCount := int(data[p])
	p++
	for i := 0; i < mobileCount && p+7 <= len(data); i++ {
		m := frameMobile{}
		m.Index = data[p]
		m.State = data[p+1]
		m.H = int16(binary.BigEndian.Uint16(data[p+2:]))
		m.V = int16(binary.BigEndian.Uint16(data[p+4:]))
		m.Colors = data[p+6]
		p += 7
		ds.mobiles = append(ds.mobiles, m)
	}

	if len(data) <= p {
		return ds, pictAgain, nil
	}
	// State data: 2-byte length prefix, then that many bytes of
	// info text + bubbles + sounds + inventory.
	if len(data) < p+2 {
		return ds, pictAgain, nil
	}
	stateLen := int(binary.BigEndian.Uint16(data[p:]))
	p += 2
	if len(data) < p+stateLen {
		stateLen = len(data) - p
	}
	sd := data[p : p+stateLen]

	// Parse info text (null-terminated string) with BEPP classification.
	// \xC2be backend responses (like be-who) are routed separately.
	if len(sd) > 0 {
		if idx := bytes.IndexByte(sd, 0); idx >= 0 {
			if idx > 0 {
				msgs, backend := classifyInfoText(sd[:idx])
				ds.chatMsgs = append(ds.chatMsgs, msgs...)
				ds.backendInfo = append(ds.backendInfo, backend...)
			}
			sd = sd[idx+1:]
		}
	}

	// Skip additional stray info text strings until we hit a valid bubble count
	for len(sd) > 0 {
		if int(sd[0]) <= 20 {
			break
		}
		if idx := bytes.IndexByte(sd, 0); idx >= 0 {
			if idx > 0 {
				msgs, backend := classifyInfoText(sd[:idx])
				ds.chatMsgs = append(ds.chatMsgs, msgs...)
				ds.backendInfo = append(ds.backendInfo, backend...)
			}
			sd = sd[idx+1:]
		} else {
			break
		}
	}

	// Parse bubbles (chat messages)
	if len(sd) > 0 {
		bubbleCount := int(sd[0])
		sd = sd[1:]
		if bubbleCount > 128 {
			bubbleCount = 128
		}
		for i := 0; i < bubbleCount && len(sd) > 1; i++ {
			descIdx := sd[0]
			typ := int(sd[1])
			cp := 2
			var h, v int16
			far := typ&kBubbleFar != 0
			if typ&kBubbleNotCommon != 0 {
				cp++
			}
			if far {
				if cp+4 > len(sd) {
					break
				}
				h = int16(binary.BigEndian.Uint16(sd[cp:]))
				v = int16(binary.BigEndian.Uint16(sd[cp+2:]))
				cp += 4
			}
			if cp >= len(sd) {
				break
			}
			end := bytes.IndexByte(sd[cp:], 0)
			if end < 0 {
				break
			}
			bubbleType := typ & kBubbleTypeMask
			msgData := stripBEPPTags(append([]byte(nil), sd[cp:cp+end]...))
			bubbleText := strings.TrimSpace(decodeMacRoman(msgData))
			if bubbleText == "" || isNightCommand(bubbleText) {
				sd = sd[cp+end+1:]
				continue
			}
			c := colorForBubbleType(bubbleType)
			words := strings.Fields(bubbleText)
			life := 2*time.Second + time.Duration(len(words))*time.Second
			if life > 12*time.Second {
				life = 12 * time.Second
			}
			ds.chatMsgs = append(ds.chatMsgs, ChatMessage{Text: bubbleText, Color: c})
			ds.bubbles = append(ds.bubbles, Bubble{
				DescIndex: descIdx,
				H:         h,
				V:         v,
				Far:       far,
				Type:      bubbleType,
				Text:      bubbleText,
				Expires:   time.Now().Add(life),
				WordCount: len(words),
			})
			sd = sd[cp+end+1:]
		}
	}

	// Parse sounds
	if len(sd) > 0 {
		soundCount := int(sd[0])
		sd = sd[1:]
		for i := 0; i < soundCount && len(sd) >= 2; i++ {
			id := binary.BigEndian.Uint16(sd[:2])
			sd = sd[2:]
			ds.sounds = append(ds.sounds, id)
		}
	}

	// Parse inventory
	if len(sd) > 0 {
		ds.inventory = parseInventorySimple(sd, existingInv)
	}

	return ds, pictAgain, nil
}

// Item slot constants
const (
	kItemSlotNotInventory = 0
	kItemSlotNotWearable  = 1
	kItemSlotForehead     = 2
	kItemSlotNeck         = 3
	kItemSlotShoulder     = 4
	kItemSlotArms         = 5
	kItemSlotGloves       = 6
	kItemSlotFinger       = 7
	kItemSlotCoat         = 8
	kItemSlotCloak        = 9
	kItemSlotTorso        = 10
	kItemSlotWaist        = 11
	kItemSlotLegs         = 12
	kItemSlotFeet         = 13
	kItemSlotRightHand    = 14
	kItemSlotLeftHand     = 15
	kItemSlotBothHands    = 16
	kItemSlotHead         = 17
	kItemSlotFirstReal    = kItemSlotForehead
	kItemSlotLastReal     = kItemSlotHead
)

var slotNames = []string{
	"invalid",    // kItemSlotNotInventory
	"unwearable", // kItemSlotNotWearable
	"forehead", "neck", "shoulder", "arms", "gloves", "finger",
	"coat", "cloak", "torso", "waist", "legs", "feet",
	"right", "left", "hands", "head",
}

const inventoryMaxSlots = 32

func parseInventorySimple(data []byte, existing []inventoryItem) []inventoryItem {
	if len(data) == 0 {
		return nil
	}
	cmd := int(data[0])
	data = data[1:]
	if cmd == 0 {
		return nil
	}

	cmdCount := 1
	if cmd == 7 {
		if len(data) < 2 {
			return nil
		}
		cmdCount = int(data[0])
		cmd = int(data[1])
		data = data[2:]
	}

	var items []inventoryItem
	baseCmd := cmd & ^0x80

	// For delta commands (not full inventory), start with existing inventory
	if baseCmd != 1 && existing != nil {
		items = make([]inventoryItem, len(existing))
		copy(items, existing)
	}

	for i := 0; i < cmdCount; i++ {
		hasIdx := cmd&0x80 != 0
		base := cmd & ^0x80

		switch base {
		case 1: // full inventory
			if len(data) < 1 {
				return items
			}
			cnt := int(data[0])
			data = data[1:]
			equipBytes := (cnt + 7) >> 3
			need := equipBytes + cnt*2
			if len(data) < need {
				return items
			}
			equips := data[:equipBytes]
			items = make([]inventoryItem, 0, cnt)
			for j := 0; j < cnt; j++ {
				eq := equips[j/8]&(1<<uint(j%8)) != 0
				id := binary.BigEndian.Uint16(data[equipBytes+j*2:])
				items = append(items, inventoryItem{
					id:       id,
					equipped: eq,
					name:     fmt.Sprintf("Item %d", id),
					quantity: 1,
					idIndex:  -1,
				})
			}
			data = data[need:]

		case 2, 3: // add / addEquip
			if len(data) < 2 {
				return items
			}
			id := binary.BigEndian.Uint16(data[:2])
			data = data[2:]
			idx := -1
			if hasIdx {
				if len(data) < 1 {
					return items
				}
				idx = int(data[0])
				data = data[1:]
			}
			nidx := -1
			for j := 0; j < len(data); j++ {
				if data[j] == 0 {
					nidx = j
					break
				}
			}
			// The server sends the raw custom name text (NOT a formatted display name).
			// Base name comes from CL_Images via EnrichInventory.
			var extra string
			if nidx >= 0 {
				raw := data[:nidx]
				cleaned := stripBEPPTags(append([]byte(nil), raw...))
				extra = strings.TrimSpace(decodeMacRoman(cleaned))
				data = data[nidx+1:]
			}
			eq := base == 3
			if idx >= 0 {
				// Template item: find existing by ID+idx, or by ID with
				// idIndex=-1 (from full inventory), or by ID alone
				// (EnrichInventory may have already assigned indexes),
				// or add new
				found := false
				for j := range items {
					if items[j].id == id && items[j].idIndex == idx {
						if extra != "" {
							items[j].extra = extra
						}
						items[j].equipped = eq
						found = true
						break
					}
				}
				if !found {
					for j := range items {
						if items[j].id == id && items[j].idIndex < 0 {
							items[j].idIndex = idx
							if extra != "" {
								items[j].extra = extra
							}
							items[j].equipped = eq
							found = true
							break
						}
					}
				}
				// Fallback: EnrichInventory may have already assigned indexes.
				// Match by ID alone and update index + extra.
				if !found {
					for j := range items {
						if items[j].id == id {
							items[j].idIndex = idx
							if extra != "" {
								items[j].extra = extra
							}
							items[j].equipped = eq
							found = true
							break
						}
					}
				}
				if !found {
					items = append(items, inventoryItem{
						id:       id,
						extra:    extra,
						equipped: eq,
						quantity: 1,
						idIndex:  idx,
					})
				}
			} else {
				// Legacy item: stack by ID only
				found := false
				for j := range items {
					if items[j].id == id && items[j].idIndex < 0 {
						items[j].quantity++
						if eq {
							items[j].equipped = true
						}
						if extra != "" {
							items[j].extra = extra
						}
						found = true
						break
					}
				}
				if !found {
					items = append(items, inventoryItem{
						id:       id,
						extra:    extra,
						equipped: eq,
						quantity: 1,
						idIndex:  -1,
					})
				}
			}

		case 4: // delete
			if len(data) < 2 {
				return items
			}
			id := binary.BigEndian.Uint16(data[:2])
			data = data[2:]
			idx := -1
			if hasIdx {
				if len(data) < 1 {
					return items
				}
				idx = int(data[0])
				data = data[1:]
			}
			// Find and remove the item
			for j := range items {
				if items[j].id == id {
					if idx >= 0 {
						if items[j].idIndex == idx {
							items = append(items[:j], items[j+1:]...)
							break
						}
					} else if items[j].idIndex < 0 {
						if items[j].quantity > 1 {
							items[j].quantity--
						} else {
							items = append(items[:j], items[j+1:]...)
						}
						break
					}
				}
			}

		case 5, 6: // equip / unequip
			if len(data) < 2 {
				return items
			}
			id := binary.BigEndian.Uint16(data[:2])
			data = data[2:]
			idx := -1
			if hasIdx {
				if len(data) < 1 {
					return items
				}
				idx = int(data[0])
				data = data[1:]
			}
			eq := base == 5
			for j := range items {
				if items[j].id == id {
					if idx >= 0 {
						if items[j].idIndex == idx {
							items[j].equipped = eq
							break
						}
					} else if items[j].idIndex < 0 {
						items[j].equipped = eq
						break
					}
				}
			}

		case 8: // name — server sends raw custom name text
			if len(data) < 2 {
				return items
			}
			itemID := binary.BigEndian.Uint16(data[:2])
			data = data[2:]
			idx := -1
			if hasIdx {
				if len(data) < 1 {
					return items
				}
				idx = int(data[0])
				data = data[1:]
			}
			nidx := -1
			for j := 0; j < len(data); j++ {
				if data[j] == 0 {
					nidx = j
					break
				}
			}
			if nidx >= 0 {
				raw := data[:nidx]
				cleaned := stripBEPPTags(append([]byte(nil), raw...))
				s := strings.TrimSpace(decodeMacRoman(cleaned))
				// Match by id+idx first, then by id alone (fallback
				// if EnrichInventory already assigned indexes).
				matched := false
				for j := range items {
					if items[j].id != itemID {
						continue
					}
					if idx >= 0 && items[j].idIndex != idx {
						continue
					}
					items[j].extra = s
					items[j].name = composeDisplayName(items[j].base, s, items[j].idIndex)
					matched = true
					break
				}
				if !matched {
					for j := range items {
						if items[j].id == itemID {
							items[j].extra = s
							items[j].name = composeDisplayName(items[j].base, s, items[j].idIndex)
							break
						}
					}
				}
				data = data[nidx+1:]
			}

		default:
			return items
		}

		if len(data) > 0 {
			cmd = int(data[0])
			data = data[1:]
		} else {
			cmd = 0
		}
	}

	return items
}

// splitBaseExtra splits "Sword of Light <#2 Flame>" into "Sword of Light", "Flame"
// or "Shield <Fire>" into "Shield", "Fire"
func splitBaseExtra(name string) (base, extra string) {
	// Look for <#N extra> or <extra>
	if p := strings.Index(name, " <"); p >= 0 {
		base = name[:p]
		tag := name[p+1:]
		if len(tag) > 0 && tag[len(tag)-1] == '>' {
			tag = tag[:len(tag)-1]
		}
		// Strip leading #N from template tags ("#1: extra" or "#1 extra")
		if len(tag) > 0 && tag[0] == '#' {
			if sp := strings.IndexByte(tag, ' '); sp >= 0 {
				after := strings.TrimSpace(tag[sp+1:])
				if len(after) > 0 && after[0] == ':' {
					after = strings.TrimSpace(after[1:])
				}
				extra = after
			} else if sp := strings.IndexByte(tag, ':'); sp >= 0 {
				extra = strings.TrimSpace(tag[sp+1:])
			}
		} else {
			extra = tag
		}
		return
	}
	return name, ""
}

// composeDisplayName creates display name with <#N extra> suffix
func composeDisplayName(baseName, extra string, idIndex int) string {
	if idIndex >= 0 {
		if extra != "" {
			return fmt.Sprintf("%s <#%d: %s>", baseName, idIndex+1, extra)
		}
		return fmt.Sprintf("%s <#%d>", baseName, idIndex+1)
	}
	if extra != "" && extra != baseName {
		return fmt.Sprintf("%s <%s>", baseName, extra)
	}
	return baseName
}

// --- Text message decoding ---

func decodeBEPP(data []byte) string {
	if len(data) < 3 || data[0] != 0xC2 {
		return ""
	}
	raw := data[3:]
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	cleaned := stripBEPPTags(append([]byte(nil), raw...))
	text := strings.TrimSpace(decodeMacRoman(cleaned))
	return text
}

// cleanInfoText strips BEPP formatting codes and night commands from raw server text.
// Returns empty string if the text should be suppressed entirely.
func cleanInfoText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	lines := strings.Split(string(raw), "\r")
	var out []string
	for _, line := range lines {
		lineBytes := []byte(line)
		cleaned := stripBEPPTags(append([]byte(nil), lineBytes...))
		text := strings.TrimSpace(decodeMacRoman(cleaned))
		if text == "" {
			continue
		}
		if isNightCommand(text) {
			continue
		}
		out = append(out, text)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// isNightCommand checks if text is a /nt N /sa N /cl N night-shadow command.
func isNightCommand(s string) bool {
	if !strings.HasPrefix(s, "/nt ") {
		return false
	}
	rest := s[4:]
	sp := strings.IndexByte(rest, ' ')
	if sp < 0 {
		return false
	}
	rest = strings.TrimSpace(rest[sp+1:])
	if !strings.HasPrefix(rest, "/sa ") {
		return false
	}
	rest = rest[4:]
	sp = strings.IndexByte(rest, ' ')
	if sp < 0 {
		return false
	}
	rest = strings.TrimSpace(rest[sp+1:])
	if !strings.HasPrefix(rest, "/cl ") {
		return false
	}
	val := rest[4:]
	return len(val) > 0 && (val[0] == '0' || val[0] == '1')
}

func stripBEPPTags(b []byte) []byte {
	out := b[:0]
	for i := 0; i < len(b); {
		c := b[i]
		if c == 0xC2 {
			if i+4 < len(b) && b[i+1] == 't' && b[i+2] == '_' && b[i+3] == 't' {
				switch b[i+4] {
				case 'h', 't', 'c', 'g':
					i += 5
					continue
				}
			}
			if i+2 < len(b) {
				i += 3
				continue
			}
			break
		}
		if c < 0x20 {
			i++
			continue
		}
		out = append(out, c)
		i++
	}
	return out
}

func decodeMessage(m []byte) string {
	if len(m) <= 16 {
		return ""
	}
	data := m[16:]
	if len(data) > 0 && data[0] == 0xC2 {
		if s := decodeBEPP(data); s != "" {
			return s
		}
		return ""
	}
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	if len(data) > 0 {
		txt := decodeMacRoman(data)
		if len(txt) > 0 {
			return strings.TrimSpace(txt)
		}
	}
	return ""
}

// decodeClassifiedMessage extracts text from a TCP message and classifies it
// by BEPP tag, returning a ChatMessage with the appropriate color.
func decodeClassifiedMessage(m []byte) ChatMessage {
	if len(m) <= 16 {
		return ChatMessage{}
	}
	data := m[16:]
	if len(data) == 0 {
		return ChatMessage{}
	}
	if data[0] == 0xC2 {
		// BEPP message — classify by tag
		if len(data) >= 3 {
			tag := string(data[1:3])
			rest := data[3:]
			if i := bytes.IndexByte(rest, 0); i >= 0 {
				rest = rest[:i]
			}
			cleaned := stripBEPPTags(append([]byte(nil), rest...))
			txt := strings.TrimSpace(decodeMacRoman(cleaned))
			if txt != "" {
				return ChatMessage{Text: txt, Color: colorForBEPPTag(tag)}
			}
		}
		return ChatMessage{}
	}
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	if len(data) > 0 {
		txt := decodeMacRoman(data)
		if len(txt) > 0 {
			return ChatMessage{Text: strings.TrimSpace(txt), Color: colDefault}
		}
	}
	return ChatMessage{}
}

// --- Login handshake ---

func dialServer(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, 10*time.Second)
}
