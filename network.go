package godwarf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"godwarf/climg"

	text "github.com/hajimehoshi/ebiten/v2/text/v2"
)

type Network struct {
	mu        sync.Mutex
	tcpConn   net.Conn
	udpConn   net.Conn
	connected bool
	cancel    context.CancelFunc

	playerName string
	playerIdx  uint8

	prevPictures []framePicture

	dsMu      sync.Mutex
	draw      *drawState
	textMsgs  []ChatMessage
	players   []PlayerInfo
	inventory []inventoryItem
	descMap   map[uint8]frameDescriptor
	dsFrame   int

	inputMu    sync.Mutex
	mouseX     int16
	mouseY     int16
	mouseDown  bool
	pendingCmd string
	pendingID  uint32
	cmdQueue   []string

	ackFrame    uint32
	resendFrame uint32
	commandNum  uint32

	disconnectMsg string

	selectedIdx    int
	selectedInvIdx int
	blocked        map[string]bool
	ignored     map[string]bool
	labels      map[string]int // player name → label 0-5 (0=none, 1-5=colors)

	whoActive       bool
	whoLastCmd      time.Time
	whoLastComplete time.Time
	whoPlayers      map[string]bool // names seen during current /be-who scan

	// Sharing / presence stats (read via getters, written under dsMu)
	nearbyCount    int // player-type mobiles visible in current draw state
	sharingWithMe  int // mobiles with bold bit (they share with you)
	sharingWithThem map[string]bool // you are sharing with these players (from info text)
	sharingYou     map[string]bool // players currently sharing with you (from mobile Colors)

	// Active chat bubbles (survive across draw states)
	activeBubbles []Bubble

	// Last draw state screen mapping (for hit-testing taps on mobiles)
	lastCenterX  float64
	lastCenterY  float64
	lastPxScale  float64
}

const (
	kFriendNone   = 0
	kFriendLabel1 = 1
	kFriendLabel2 = 2
	kFriendLabel3 = 3
	kFriendLabel4 = 4
	kFriendLabel5 = 5
)

var friendLabelNames = map[string]int{
	"none":   kFriendNone,
	"red":    kFriendLabel1,
	"orange": kFriendLabel2,
	"green":  kFriendLabel3,
	"blue":   kFriendLabel4,
	"purple": kFriendLabel5,
	"0":      kFriendNone,
	"1":      kFriendLabel1,
	"2":      kFriendLabel2,
	"3":      kFriendLabel3,
	"4":      kFriendLabel4,
	"5":      kFriendLabel5,
}

func friendLabelName(n int) string {
	switch n {
	case kFriendLabel1:
		return "red"
	case kFriendLabel2:
		return "orange"
	case kFriendLabel3:
		return "green"
	case kFriendLabel4:
		return "blue"
	case kFriendLabel5:
		return "purple"
	default:
		return "none"
	}
}

func (n *Network) SetLabel(name string, label int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.labels == nil {
		n.labels = make(map[string]int)
	}
	if label == kFriendNone {
		delete(n.labels, strings.ToLower(name))
	} else {
		n.labels[strings.ToLower(name)] = label
	}
}

func (n *Network) GetLabel(name string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.labels == nil {
		return kFriendNone
	}
	return n.labels[strings.ToLower(name)]
}

func (n *Network) GetLabeledPlayers(label int) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for name, l := range n.labels {
		if label < 0 || l == label {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (n *Network) Connected() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.connected
}

func (n *Network) GetPlayers() []PlayerInfo {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	out := make([]PlayerInfo, len(n.players))
	copy(out, n.players)
	return out
}

func (n *Network) GetPlayerNames() []string {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	out := make([]string, 0, len(n.players))
	for _, p := range n.players {
		if p.Name != "" {
			out = append(out, p.Name)
		}
	}
	return out
}

func (n *Network) GetInventory() []inventoryItem {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	out := make([]inventoryItem, len(n.inventory))
	copy(out, n.inventory)
	return out
}

const kItemFlagData = 0x0400

func (n *Network) EnrichInventory(cl *climg.CLImages) {
	if cl == nil {
		return
	}
	n.dsMu.Lock()
	defer n.dsMu.Unlock()

	// First pass: assign sequential template indexes for items missing idIndex
	tmplCounts := make(map[uint16]int)
	for i := range n.inventory {
		it := &n.inventory[i]
		if it.id == 0 {
			continue
		}
		if it.idIndex >= 0 {
			continue
		}
		if ci, ok := cl.Item(uint32(it.id)); ok && ci.Flags&kItemFlagData != 0 {
			it.idIndex = tmplCounts[it.id]
			tmplCounts[it.id] = it.idIndex + 1
		}
	}

	// Second pass: fill pictIDs, slots, names
	for i := range n.inventory {
		it := &n.inventory[i]
		if it.id == 0 {
			continue
		}
		id := uint32(it.id)
		if it.pictID == 0 {
			it.pictID = uint16(cl.ItemWornPict(id))
		}
		if it.equipped {
			if slot := cl.ItemSlot(id); slot >= kItemSlotFirstReal {
				switch slot {
				case kItemSlotRightHand:
					if p := cl.ItemRightHandPict(id); p != 0 {
						it.pictID = uint16(p)
					}
				case kItemSlotLeftHand:
					if p := cl.ItemLeftHandPict(id); p != 0 {
						it.pictID = uint16(p)
					}
				}
			}
		}
		if it.slot == 0 {
			it.slot = cl.ItemSlot(id)
		}
		if it.base == "" {
			if realName := cl.ItemName(id); realName != "" {
				it.base, _ = splitBaseExtra(realName)
			}
		}
		if it.base != "" {
			it.name = composeDisplayName(it.base, it.extra, it.idIndex)
		}
	}
}

func (n *Network) GetSelectedIdx() int {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	return n.selectedIdx
}

func (n *Network) SetSelectedIdx(idx int) {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	n.selectedIdx = idx
}

func (n *Network) GetSelectedInvIdx() int {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	return n.selectedInvIdx
}

func (n *Network) SetSelectedInvIdx(idx int) {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	n.selectedInvIdx = idx
}

func (n *Network) GetInventoryItems() []inventoryItem {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	out := make([]inventoryItem, len(n.inventory))
	copy(out, n.inventory)
	return out
}

func (n *Network) ToggleBlock(name string) {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	if n.blocked == nil {
		n.blocked = make(map[string]bool)
	}
	lower := strings.ToLower(name)
	n.blocked[lower] = !n.blocked[lower]
}

func (n *Network) ToggleIgnore(name string) {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	if n.ignored == nil {
		n.ignored = make(map[string]bool)
	}
	lower := strings.ToLower(name)
	n.ignored[lower] = !n.ignored[lower]
}

func (n *Network) RemoveBlockIgnore(name string) {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	lower := strings.ToLower(name)
	if n.blocked != nil {
		delete(n.blocked, lower)
	}
	if n.ignored != nil {
		delete(n.ignored, lower)
	}
}

func (n *Network) IsBlocked(name string) bool {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	if n.blocked == nil {
		return false
	}
	return n.blocked[strings.ToLower(name)]
}

func (n *Network) IsIgnored(name string) bool {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	if n.ignored == nil {
		return false
	}
	return n.ignored[strings.ToLower(name)]
}

func (n *Network) GetDisconnectMsg() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	msg := n.disconnectMsg
	n.disconnectMsg = ""
	return msg
}

func (n *Network) GetDrawState() *drawState {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	return n.draw
}

func (n *Network) GetDSFrame() int {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	return n.dsFrame
}

// parseShareNames updates the share-with set from a self share/unshare
// message. Names are extracted after prefix (case-insensitive) and may be
// comma separated. add=true records sharees, add=false removes them.
func parseShareNames(set map[string]bool, text, prefix string, add bool) {
	idx := strings.Index(strings.ToLower(text), strings.ToLower(prefix))
	if idx < 0 {
		return
	}
	rest := strings.TrimSpace(text[idx+len(prefix):])
	rest = strings.TrimSuffix(rest, ".")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return
	}
	for _, name := range strings.Split(rest, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if add {
			set[name] = true
		} else {
			delete(set, name)
		}
	}
}

// SharingStats returns (online, nearby, sharingWithMe, sharingWithThem).
func (n *Network) SharingStats() (int, int, int, int) {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	online := len(n.players)
	nearby := n.nearbyCount
	shareMe := n.sharingWithMe
	shareThem := len(n.sharingWithThem)
	return online, nearby, shareMe, shareThem
}

// GetSharingWith returns a copy of the set of player names you are sharing with.
func (n *Network) GetSharingWith() map[string]bool {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	out := make(map[string]bool, len(n.sharingWithThem))
	for k, v := range n.sharingWithThem {
		out[k] = v
	}
	return out
}

// GetSharingYou returns a copy of the set of player names currently sharing with you.
func (n *Network) GetSharingYou() map[string]bool {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	out := make(map[string]bool, len(n.sharingYou))
	for k, v := range n.sharingYou {
		out[k] = v
	}
	return out
}

// GetSortedPlayers returns players sorted: you-share-with first (italic),
// then sharing-you (bold), then others.
func (n *Network) GetSortedPlayers() []PlayerInfo {
	shareWith := n.GetSharingWith()
	sharingYou := n.GetSharingYou()
	players := n.GetPlayers()
	sort.Slice(players, func(i, j int) bool {
		ci := playerSortKey(players[i].Name, shareWith, sharingYou)
		cj := playerSortKey(players[j].Name, shareWith, sharingYou)
		return ci < cj
	})
	return players
}

func playerSortKey(name string, shareWith, sharingYou map[string]bool) int {
	low := strings.ToLower(name)
	if shareWith[low] {
		return 0
	}
	if sharingYou[low] {
		return 1
	}
	return 2
}

// FindTapTarget returns the name of the mobile at the given screen coordinates, or "" if none.
func (n *Network) FindTapTarget(screenX, screenY int) string {
	n.dsMu.Lock()
	ds := n.draw
	n.mu.Lock()
	centerX := n.lastCenterX
	centerY := n.lastCenterY
	pixelScale := n.lastPxScale
	n.mu.Unlock()
	if ds == nil {
		n.dsMu.Unlock()
		return ""
	}
	descMap := ds.descMap
	mobiles := ds.mobiles
	n.dsMu.Unlock()

	tx := float64(screenX)
	ty := float64(screenY)
	for _, m := range mobiles {
		d, ok := descMap[m.Index]
		if !ok || d.Name == "" || d.Type != 1 {
			continue
		}
		px := centerX + float64(m.H)*pixelScale
		py := centerY + float64(m.V)*pixelScale
		half := 20.0 * pixelScale
		if tx >= px-half && tx < px+half && ty >= py-half && ty < py+half {
			return d.Name
		}
	}
	return ""
}

func (n *Network) GetTextMessages() []ChatMessage {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	out := make([]ChatMessage, len(n.textMsgs))
	copy(out, n.textMsgs)
	return out
}

func (n *Network) GetActiveBubbles() []Bubble {
	n.dsMu.Lock()
	defer n.dsMu.Unlock()
	now := time.Now()
	var out []Bubble
	for _, b := range n.activeBubbles {
		if now.Before(b.Expires) {
			out = append(out, b)
		}
	}
	return out
}

func (n *Network) AddTextMessage(msg string, c color.RGBA) {
	msg = strings.ReplaceAll(msg, "\r", "")
	n.dsMu.Lock()
	n.textMsgs = append(n.textMsgs, ChatMessage{Text: msg, Color: c})
	if len(n.textMsgs) > 200 {
		n.textMsgs = n.textMsgs[len(n.textMsgs)-200:]
	}
	n.dsMu.Unlock()
	UpdateLastTextLog(msg)
}

func (n *Network) EnqueueCommand(cmd string) {
	n.inputMu.Lock()
	defer n.inputMu.Unlock()
	n.cmdQueue = append(n.cmdQueue, cmd)
	flog(fmt.Sprintf("EnqueueCommand: %q (queue=%d pending=%q)", cmd, len(n.cmdQueue), n.pendingCmd))
}

func (n *Network) nextCommand() string {
	n.inputMu.Lock()
	defer n.inputMu.Unlock()
	if n.pendingCmd != "" {
		return n.pendingCmd
	}
	if len(n.cmdQueue) > 0 {
		cmd := n.cmdQueue[0]
		n.cmdQueue = n.cmdQueue[1:]
		n.pendingCmd = cmd
		n.mu.Lock()
		n.pendingID = n.commandNum
		n.mu.Unlock()
		flog(fmt.Sprintf("nextCommand: new pending=%q id=%d (remaining=%d)", cmd, n.pendingID, len(n.cmdQueue)))
		return n.pendingCmd
	}
	return ""
}

func (n *Network) ackCommand(ackCmdNum uint8) {
	n.inputMu.Lock()
	defer n.inputMu.Unlock()
	if n.pendingCmd == "" {
		return
	}
	n.mu.Lock()
	pendingID := n.pendingID
	n.mu.Unlock()
	if uint8(pendingID&0xFF) == ackCmdNum {
		flog(fmt.Sprintf("ackCommand: acked %q (id=%d ack=%d)", n.pendingCmd, pendingID, ackCmdNum))
		n.pendingCmd = ""
	}
}

// SendInput updates movement direction and mouse state.
func (n *Network) SendInput(dx, dy float32, mouseDown bool) {
	if !n.connected || n.udpConn == nil {
		return
	}
	n.inputMu.Lock()
	n.mouseX = int16(dx * 400)
	n.mouseY = int16(dy * 400)
	n.mouseDown = mouseDown
	n.inputMu.Unlock()
}

func (n *Network) Connect(server, name, pass string, clVersion int) error {
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel

	tcp, err := net.DialTimeout("tcp", server, 10*time.Second)
	if err != nil {
		cancel()
		return fmt.Errorf("tcp: %w", err)
	}

	udp, err := net.DialTimeout("udp", server, 10*time.Second)
	if err != nil {
		tcp.Close()
		cancel()
		return fmt.Errorf("udp: %w", err)
	}

	var idBuf [4]byte
	if _, err := io.ReadFull(tcp, idBuf[:]); err != nil {
		tcp.Close()
		udp.Close()
		cancel()
		return fmt.Errorf("read id: %w", err)
	}

	handshake := append([]byte{0xff, 0xff}, idBuf[:]...)
	if _, err := udp.Write(handshake); err != nil {
		tcp.Close()
		udp.Close()
		cancel()
		return fmt.Errorf("send handshake: %w", err)
	}

	var confirm [2]byte
	if _, err := io.ReadFull(tcp, confirm[:]); err != nil {
		tcp.Close()
		udp.Close()
		cancel()
		return fmt.Errorf("confirm handshake: %w", err)
	}

	sendVersion := clVersion
	if sendVersion <= 0 {
		sendVersion = currentCLVer - 1
	}
	imagesVersion := encodeFullVersion(sendVersion)
	soundsVersion := encodeFullVersion(sendVersion)
	if err := sendClientIdentifiers(tcp, encodeFullVersion(sendVersion), imagesVersion, soundsVersion); err != nil {
		tcp.Close()
		udp.Close()
		cancel()
		return fmt.Errorf("send identifiers: %w", err)
	}

	msg, err := readTCPMessage(tcp)
	if err != nil {
		tcp.Close()
		udp.Close()
		cancel()
		return fmt.Errorf("read challenge: %w", err)
	}
	const kMsgChallenge = 18
	tag := binary.BigEndian.Uint16(msg[:2])
	if tag != kMsgChallenge || len(msg) < 16 {
		tcp.Close()
		udp.Close()
		cancel()
		return fmt.Errorf("unexpected tag %d", tag)
	}
	challenge := msg[16 : 16+16]

	for {
		answer, err := answerChallenge(pass, challenge)
		if err != nil {
			tcp.Close()
			udp.Close()
			cancel()
			return fmt.Errorf("hash: %w", err)
		}

		const kMsgLogOn = 13
		nameBytes := encodeMacRoman(name)
		buf := make([]byte, 16+len(nameBytes)+1+len(answer))
		binary.BigEndian.PutUint16(buf[0:2], kMsgLogOn)
		binary.BigEndian.PutUint16(buf[2:4], 0)
		binary.BigEndian.PutUint32(buf[4:8], encodeFullVersion(sendVersion))
		binary.BigEndian.PutUint32(buf[8:12], imagesVersion)
		binary.BigEndian.PutUint32(buf[12:16], soundsVersion)
		copy(buf[16:], nameBytes)
		buf[16+len(nameBytes)] = 0
		copy(buf[17+len(nameBytes):], answer)
		simpleEncrypt(buf[16:])

		if err := sendTCPMessage(tcp, buf); err != nil {
			tcp.Close()
			udp.Close()
			cancel()
			return fmt.Errorf("send login: %w", err)
		}

		resp, err := readTCPMessage(tcp)
		if err != nil {
			tcp.Close()
			udp.Close()
			cancel()
			return fmt.Errorf("read login response: %w", err)
		}
		resTag := binary.BigEndian.Uint16(resp[:2])
		const kMsgLogOnResp = 13
		if resTag == kMsgLogOnResp {
			result := int16(binary.BigEndian.Uint16(resp[2:4]))
			if result != 0 {
				tcp.Close()
				udp.Close()
				cancel()
				return fmt.Errorf("login failed: %d", result)
			}
			break
		}
		if resTag == kMsgChallenge {
			challenge = resp[16 : 16+16]
			continue
		}
		tcp.Close()
		udp.Close()
		cancel()
		return fmt.Errorf("unexpected response tag %d", resTag)
	}

	tcp.SetDeadline(time.Time{})
	udp.SetDeadline(time.Time{})

	n.mu.Lock()
	n.tcpConn = tcp
	n.udpConn = udp
	n.connected = true
	n.playerName = name
	n.ackFrame = 0
	n.resendFrame = 0
	n.commandNum = 1
	n.mu.Unlock()

	flog(fmt.Sprintf("logged in as %s (v%d)", name, sendVersion))

	ensureMacroFolder(name)
	StartTextLog(name)
	LoadMacros(name)

	go n.tcpReadLoop(ctx, tcp)
	go n.udpReadLoop(ctx, udp)
	go n.sendInputLoop(ctx, udp, tcp)

	return nil
}

func (n *Network) Disconnect() {
	if n.cancel != nil {
		n.cancel()
	}
	n.mu.Lock()
	wasConnected := n.connected
	n.disconnectMsg = "Disconnected"
	if n.tcpConn != nil {
		n.tcpConn.Close()
		n.tcpConn = nil
	}
	if n.udpConn != nil {
		n.udpConn.Close()
		n.udpConn = nil
	}
	n.connected = false
	n.mu.Unlock()
	if wasConnected {
		log.Println("goDwarf: disconnected")
		CloseTextLog()
	}
}

func (n *Network) tcpReadLoop(ctx context.Context, conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
			panic(r)
		}
	}()
	flog("tcpReadLoop: started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		m, err := readTCPMessage(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("goDwarf: tcp read error: %v", err)
			n.Disconnect()
			return
		}
		n.processMessage(m)
	}
}

func (n *Network) udpReadLoop(ctx context.Context, conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
			panic(r)
		}
	}()
	flog("udpReadLoop: started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		m, err := readUDPMessage(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			flog(fmt.Sprintf("udpReadLoop: error: %v", err))
			n.Disconnect()
			return
		}
		tag := binary.BigEndian.Uint16(m[:2])
		if tag == 2 {
			n.processDrawState(m)
		} else {
			n.processMessage(m)
		}
	}
}

func (n *Network) sendInputLoop(ctx context.Context, udpConn, tcpConn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
			panic(r)
		}
	}()
	var nextReliable time.Time
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		time.Sleep(50 * time.Millisecond)

		n.inputMu.Lock()
		mx := n.mouseX
		my := n.mouseY
		md := n.mouseDown
		n.inputMu.Unlock()

		n.dsMu.Lock()
		ack := n.ackFrame
		resend := n.resendFrame
		n.dsMu.Unlock()

		cmd := n.nextCommand()

		n.mu.Lock()
		var cNum uint32
		if cmd != "" {
			cNum = n.pendingID
		} else {
			cNum = n.commandNum
			n.commandNum++
			if n.commandNum > 255 {
				n.commandNum = 1
			}
		}
		n.mu.Unlock()

		reliable := false
		now := time.Now()
		if now.After(nextReliable) {
			reliable = true
			nextReliable = now.Add(3 * time.Second)
		}

		if cmd != "" {
			flog(fmt.Sprintf("sendInput: cmd=%q cNum=%d reliable=%v ack=%d resend=%d mouse=%d,%d down=%v", cmd, cNum, reliable, ack, resend, mx, my, md))
		}

		var err error
		if reliable {
			err = sendPlayerInput(tcpConn, mx, my, md, cmd, ack, resend, cNum)
		} else {
			err = sendPlayerInput(udpConn, mx, my, md, cmd, ack, resend, cNum)
		}
		if err != nil {
			flog(fmt.Sprintf("sendInput ERROR: %v", err))
		}
	}
}

func (n *Network) processMessage(m []byte) {
	if len(m) < 2 {
		return
	}
	tag := binary.BigEndian.Uint16(m[:2])
	if tag == 2 {
		n.processDrawState(m)
		return
	}
	if msg := decodeClassifiedMessage(m); msg.Text != "" {
		txt := strings.ReplaceAll(msg.Text, "\r", "")
		msg.Text = txt
		n.dsMu.Lock()
		n.textMsgs = append(n.textMsgs, msg)
		if len(n.textMsgs) > 200 {
			n.textMsgs = n.textMsgs[len(n.textMsgs)-200:]
		}
		n.dsMu.Unlock()
		logTextMessage(txt)
	}
}

func (n *Network) handleBackendResponse(data []byte) {
	// Expect \xC2be\xC2XX<payload> where XX is the sub-command (wh, in, sh).
	if len(data) < 6 || data[0] != 0xC2 || data[1] != 'b' || data[2] != 'e' {
		return
	}
	sub := data[3:]
	if len(sub) < 3 || sub[0] != 0xC2 {
		return
	}
	cmd := string(sub[1:3])
	payload := sub[3:]
	switch cmd {
	case "wh":
		n.parseBackendWho(payload)
	}
}

// parseBackendWho parses "be-wh" messages listing online players.
// Format: \xC2pn<name>\xC2pn,<realname>,<gmlevel>\t repeated up to 20 times.
// Fewer than 20 names means the full list has been received.
func (n *Network) parseBackendWho(data []byte) {
	batchCount := 0
	newNames := 0
	for len(data) > 0 {
		if len(data) < 3 || data[0] != 0xC2 || data[1] != 'p' || data[2] != 'n' {
			break
		}
		data = data[3:]
		end := bytes.Index(data, []byte{0xC2, 'p', 'n'})
		if end < 0 {
			break
		}
		name := strings.TrimSpace(decodeMacRoman(data[:end]))
		seg := data[end+3:]
		tab := bytes.IndexByte(seg, '\t')
		if tab < 0 {
			break
		}
		data = seg[tab+1:]

		batchCount++
		n.dsMu.Lock()
		if n.whoPlayers == nil {
			n.whoPlayers = make(map[string]bool)
		}
		if !n.whoPlayers[name] {
			newNames++
		}
		n.whoPlayers[name] = true
		n.dsMu.Unlock()
	}
	flog(fmt.Sprintf("parseBackendWho: batch=%d total=%d", batchCount, func() int {
		n.dsMu.Lock()
		c := len(n.whoPlayers)
		n.dsMu.Unlock()
		return c
	}()))
	// A response with fewer than 20 names means the last page of the list has
	// been received. Also consider the scan complete when a batch adds no new
	// names — the server may keep paging with exactly 20 per response, which
	// would otherwise leave the scan active forever and never rebuild the list.
	if batchCount < 20 || newNames == 0 {
		n.dsMu.Lock()
		n.whoActive = false
		n.whoLastComplete = time.Now()
		n.buildPlayerListFromWhoLocked()
		n.dsMu.Unlock()
	}
}

// buildPlayerListFromWhoLocked rebuilds the player list from the accumulated
// /be-who names, supplementing with pictID/colors from descriptors when available.
// Caller must hold n.dsMu.
func (n *Network) buildPlayerListFromWhoLocked() {
	n.players = n.players[:0]
	for name := range n.whoPlayers {
		pi := PlayerInfo{Name: name, State: 8}
		for _, d := range n.descMap {
			if d.Name == name {
				pi.PictID = d.PictID
				pi.Colors = d.Colors
				break
			}
		}
		n.players = append(n.players, pi)
	}
	sort.Slice(n.players, func(i, j int) bool {
		return n.players[i].Name < n.players[j].Name
	})
	flog(fmt.Sprintf("buildPlayerListFromWho: %d players", len(n.players)))
}

// RequestPlayersData periodically sends /be-who to refresh the player list.
// Called from the Update loop when connected.
func (n *Network) RequestPlayersData() {
	if !n.Connected() {
		return
	}
	if time.Since(n.whoLastCmd) < time.Second {
		return
	}
	n.inputMu.Lock()
	pending := n.pendingCmd != ""
	n.inputMu.Unlock()
	if pending {
		return
	}
	n.dsMu.Lock()
	active := n.whoActive
	lastComplete := n.whoLastComplete
	n.dsMu.Unlock()
	if active {
		// Scan still in progress: send another /be-who.
		n.EnqueueCommand("/be-who")
		n.whoLastCmd = time.Now()
		return
	}
	// Scan not active.  Start a new scan only after a cooldown.
	if time.Since(lastComplete) < 30*time.Second {
		return
	}
	n.dsMu.Lock()
	n.whoActive = true
	n.whoPlayers = make(map[string]bool)
	n.dsMu.Unlock()
	n.EnqueueCommand("/be-who")
	n.whoLastCmd = time.Now()
}

func (n *Network) processDrawState(m []byte) {
	if len(m) < 2 {
		flog("processDrawState: too short")
		return
	}
	data := m[2:]

	ds, pictAgain, err := parseDrawStateData(data, n.inventory)
	if err != nil && ds == nil {
		flog(fmt.Sprintf("processDrawState: parse error (no ds): %v (len=%d)", err, len(data)))
		return
	}
	if err != nil {
		flog(fmt.Sprintf("processDrawState: parse error (partial): %v (len=%d)", err, len(data)))
	}

	// Always accumulate descriptors and update ack/resend — even on partial
	// parse errors.  The server sends descriptor deltas across frames, so
	// discarding a frame's descriptors loses them permanently.
	n.dsMu.Lock()
	n.ackFrame = ds.ack
	n.resendFrame = ds.resend
	if ds != nil {
		n.dsFrame++
		if n.descMap == nil {
			n.descMap = make(map[uint8]frameDescriptor)
		}
		for _, d := range ds.descriptors {
			n.descMap[d.Index] = d
		}
		ds.descMap = n.descMap
		// Resolve bubble names using the accumulated descriptor map.
		for i := range ds.chatMsgs {
			// Find the matching bubble by index (they share the same order).
			if i < len(ds.bubbles) {
				b := &ds.bubbles[i]
				if b.Text != "" {
					for _, d := range n.descMap {
						if d.Index == b.DescIndex && d.Name != "" {
							bType := b.Type
							switch bType {
							case 1:
								ds.chatMsgs[i].Text = fmt.Sprintf("%s whispers: %s", d.Name, b.Text)
							case 2:
								ds.chatMsgs[i].Text = fmt.Sprintf("%s yells: %s", d.Name, b.Text)
							case 3:
								ds.chatMsgs[i].Text = fmt.Sprintf("%s thinks: %s", d.Name, b.Text)
							case 6:
								ds.chatMsgs[i].Text = fmt.Sprintf("%s %s", d.Name, b.Text)
							default:
								ds.chatMsgs[i].Text = fmt.Sprintf("%s says: %s", d.Name, b.Text)
							}
							break
						}
					}
				}
			}
		}
		// Only build player list from descriptors if no /be-who scan is active
		// or has completed.  The /be-who scan is authoritative for the player list.
		if !n.whoActive && len(n.whoPlayers) == 0 {
			n.players = n.players[:0]
			for _, d := range n.descMap {
				if d.Name != "" && d.Type == 1 {
					pi := PlayerInfo{Name: d.Name, PictID: d.PictID, Colors: d.Colors}
					// Match CL player list: always show static standing-south pose (8).
					// CL uses kGetMobilePose(kPoseStand, kFacingSouth) = 2*4+0 = 8.
					pi.State = 8
					n.players = append(n.players, pi)
				}
			}
			sort.Slice(n.players, func(i, j int) bool {
				return n.players[i].Name < n.players[j].Name
			})
		} else if len(n.whoPlayers) > 0 && !n.whoActive {
			// Scan complete: update pictID/colors from descriptors for who-listed players.
			for i := range n.players {
				for _, d := range n.descMap {
					if d.Name == n.players[i].Name {
						n.players[i].PictID = d.PictID
						n.players[i].Colors = d.Colors
						break
					}
				}
			}
		}
		// Update own character appearance from descriptor. The self (index 0)
		// descriptor may omit the name, and name case can differ from the login
		// name, so match case-insensitively and fall back to index 0 when unnamed.
		for _, d := range n.descMap {
			if d.PictID == 0 {
				continue
			}
			if d.Index == 0 && d.Name == "" {
				go updateCharacterAppearance(n.playerName, d.PictID, d.Colors)
				break
			}
			if strings.EqualFold(d.Name, n.playerName) {
				go updateCharacterAppearance(d.Name, d.PictID, d.Colors)
				break
			}
		}
	}
	n.dsMu.Unlock()

	// If there was a header-level error, stop here.
	if err != nil {
		return
	}

	if pictAgain > 0 {
		n.mu.Lock()
		prev := n.prevPictures
		n.mu.Unlock()
		if pictAgain > 0 {
			cnt := pictAgain
			if cnt > len(prev) {
				cnt = len(prev)
			}
			combined := make([]framePicture, 0, cnt+len(ds.pictures))
			combined = append(combined, prev[:cnt]...)
			combined = append(combined, ds.pictures...)
			ds.pictures = combined
		}
	}

	if len(ds.sounds) > 0 {
		playSounds(ds.sounds)
	}

	n.dsMu.Lock()
	n.draw = ds
	if ds.inventory != nil {
		n.inventory = ds.inventory
	}
	// Compute sharing/presence stats from draw state mobiles.
	nearby := 0
	sharingMe := 0
	sharingYouMap := make(map[string]bool)
	for _, m := range ds.mobiles {
		if m.Index == 0 {
			continue // skip own character
		}
		if d, ok := ds.descMap[m.Index]; ok && d.Type == 1 {
			nearby++
			if m.Colors&0x01 != 0 { // kColorCodeStyleBold = sharing with you
				sharingMe++
				if d.Name != "" {
					sharingYouMap[d.Name] = true
				}
			}
		}
	}
	n.nearbyCount = nearby
	n.sharingWithMe = sharingMe
	n.sharingYou = sharingYouMap
	if len(ds.chatMsgs) > 0 {
		for _, m := range ds.chatMsgs {
			m.Text = strings.ReplaceAll(m.Text, "\r", "")
			n.textMsgs = append(n.textMsgs, m)
			logTextMessage(m.Text)
			// Track who you are sharing with. The server reports a /share as
			// "You begin sharing your experiences with X." (or the plural
			// "You are sharing experiences with X, Y.") and an unshare as
			// "You are no longer sharing experiences with X." or "... with
			// anyone."
			low := strings.ToLower(m.Text)
			if n.sharingWithThem == nil {
				n.sharingWithThem = make(map[string]bool)
			}
			switch {
			case strings.Contains(low, "you are no longer sharing experiences with anyone") ||
				strings.Contains(low, "you are not sharing experiences with anyone"):
				for name := range n.sharingWithThem {
					delete(n.sharingWithThem, name)
				}
			case strings.Contains(low, "you begin sharing your experiences with "):
				parseShareNames(n.sharingWithThem, m.Text, "You begin sharing your experiences with ", true)
			case strings.Contains(low, "you are sharing experiences with "):
				parseShareNames(n.sharingWithThem, m.Text, "You are sharing experiences with ", true)
			case strings.Contains(low, "you are no longer sharing experiences with "):
				parseShareNames(n.sharingWithThem, m.Text, "You are no longer sharing experiences with ", false)
			}
		}
		if len(n.textMsgs) > 200 {
			n.textMsgs = n.textMsgs[len(n.textMsgs)-200:]
		}
	}
	// Merge new bubbles from draw state, expire old ones. Dedup by speaker Index.
	now := time.Now()
	valid := n.activeBubbles[:0]
	for _, b := range n.activeBubbles {
		if now.Before(b.Expires) {
			valid = append(valid, b)
		}
	}
	n.activeBubbles = valid
	for _, nb := range ds.bubbles {
		replaced := false
		for i := range n.activeBubbles {
			if n.activeBubbles[i].DescIndex == nb.DescIndex {
				n.activeBubbles[i] = nb
				replaced = true
				break
			}
		}
		if !replaced {
			n.activeBubbles = append(n.activeBubbles, nb)
		}
	}
	n.dsMu.Unlock()

	// Feed each server text line to the macro engine so @env.textLog tracks
	// the latest game text and paused macros (e.g. @login automation loops)
	// resume when new text arrives. Must run after dsMu.Unlock: a macro's
	// message command calls AddTextMessage which re-locks dsMu.
	for _, m := range ds.chatMsgs {
		UpdateLastTextLog(strings.ReplaceAll(m.Text, "\r", ""))
	}

	// Process backend responses extracted from info text (be-who, be-info, etc.)
	for _, raw := range ds.backendInfo {
		n.handleBackendResponse(raw)
	}

	n.ackCommand(ds.ackCmdNum)

	flog(fmt.Sprintf("drawState: pics=%d mobs=%d descs=%d again=%d hp=%d/%d", len(ds.pictures), len(ds.mobiles), len(ds.descriptors), pictAgain, ds.hp, ds.hpMax))

	n.mu.Lock()
	n.prevPictures = ds.pictures
	n.mu.Unlock()
}

func (n *Network) DrawWorld(screen *ebiten.Image, sw, sh int, cl *climg.CLImages) {
	ds := n.GetDrawState()
	if ds == nil {
		ebitenutil.DebugPrintAt(screen, "Connecting...", sw/2-40, sh/2)
		return
	}

	centerX := float64(sw) / 2
	centerY := float64(sh) / 2
	pixelScale := 2.0

	// Store screen mapping for hit-testing taps.
	n.mu.Lock()
	n.lastCenterX = centerX
	n.lastCenterY = centerY
	n.lastPxScale = pixelScale
	n.mu.Unlock()

	// Sort a COPY of pictures for rendering.
	// We must NOT sort ds.pictures in place because n.prevPictures shares the
	// same backing array — sorting it would reorder prevPictures and break the
	// server's pictAgain tracking (which assumes server-sent order).
	sortedPics := make([]framePicture, len(ds.pictures))
	copy(sortedPics, ds.pictures)
	if cl != nil {
		for i := range sortedPics {
			sortedPics[i].Plane = cl.Plane(uint32(sortedPics[i].PictID))
		}
	}
	sort.Slice(sortedPics, func(i, j int) bool {
		if sortedPics[i].Plane != sortedPics[j].Plane {
			return sortedPics[i].Plane < sortedPics[j].Plane
		}
		if sortedPics[i].V == sortedPics[j].V {
			return sortedPics[i].H < sortedPics[j].H
		}
		return sortedPics[i].V < sortedPics[j].V
	})
	// Sort mobiles: by V, then H
	sort.Slice(ds.mobiles, func(i, j int) bool {
		if ds.mobiles[i].V == ds.mobiles[j].V {
			return ds.mobiles[i].H < ds.mobiles[j].H
		}
		return ds.mobiles[i].V < ds.mobiles[j].V
	})

	frame := n.GetDSFrame()

	// Helper: draw a single picture at screen coordinates.
	drawOnePicture := func(p framePicture) {
		px := centerX + float64(p.H)*pixelScale
		py := centerY + float64(p.V)*pixelScale
		if cl != nil {
			img := loadPictureSprite(cl, p.PictID, frame)
			if img != nil {
				op := &ebiten.DrawImageOptions{}
				w, h := img.Bounds().Dx(), img.Bounds().Dy()
				op.GeoM.Scale(pixelScale, pixelScale)
				op.GeoM.Translate(px-float64(w)*pixelScale/2, py-float64(h)*pixelScale/2)
				screen.DrawImage(img, op)
				return
			}
		}
		ebitenutil.DrawRect(screen, px-4, py-4, 8, 8, colorForPict(p.PictID))
	}

	// Snapshot labels and sharing state for use in name drawing (must hold n.mu).
	n.mu.Lock()
	labelsCopy := make(map[string]int, len(n.labels))
	for k, v := range n.labels {
		labelsCopy[k] = v
	}
	shareWithCopy := make(map[string]bool, len(n.sharingWithThem))
	for k, v := range n.sharingWithThem {
		shareWithCopy[k] = v
	}
	sharingYouCopy := make(map[string]bool, len(n.sharingYou))
	for k, v := range n.sharingYou {
		sharingYouCopy[k] = v
	}
	n.mu.Unlock()

	// Helper: draw a single mobile at screen coordinates.
	drawOneMobile := func(m frameMobile) {
		px := centerX + float64(m.H)*pixelScale
		py := centerY + float64(m.V)*pixelScale
		d, ok := ds.descMap[m.Index]
		if ok {
			if cl != nil {
				img := loadMobileSprite(cl, d.PictID, m.State, d.Colors)
				if img != nil {
					op := &ebiten.DrawImageOptions{}
					w, h := img.Bounds().Dx(), img.Bounds().Dy()
					op.GeoM.Scale(pixelScale, pixelScale)
					op.GeoM.Translate(px-float64(w)*pixelScale/2, py-float64(h)*pixelScale/2)
					screen.DrawImage(img, op)
					if d.Name != "" {
						drawNameTag(screen, d.Name, int(px), int(py+float64(h)*pixelScale/2), m.Colors, d.Type, labelsCopy, shareWithCopy, sharingYouCopy)
					} else {
						drawMobileLifeBar(screen, int(px), int(py+float64(h)*pixelScale/2), m.Colors, d.Type)
					}
					return
				}
			}
			c := colorForMobile(d.Type, m.Colors)
			ebitenutil.DrawRect(screen, px-6, py-12, 12, 24, c)
			if d.Name != "" {
				drawNameTag(screen, d.Name, int(px), int(py)+14, m.Colors, d.Type, labelsCopy, shareWithCopy, sharingYouCopy)
			} else {
				drawMobileLifeBar(screen, int(px), int(py)+12, m.Colors, d.Type)
			}
		} else {
			ebitenutil.DrawRect(screen, px-3, py-3, 6, 6, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	// --- Render passes matching CL rendering pipeline ---

	// Pass 1: Draw plane<0 pictures (below players).
	for _, p := range sortedPics {
		if p.Plane < 0 {
			drawOnePicture(p)
		}
	}

	// Partition mobiles into dead and live.
	var deadMobiles, liveMobiles []frameMobile
	for _, m := range ds.mobiles {
		if m.State == 32 { // kPoseDead
			deadMobiles = append(deadMobiles, m)
		} else {
			liveMobiles = append(liveMobiles, m)
		}
	}

	// Pass 2: Draw dead mobiles first (under everything).
	for _, m := range deadMobiles {
		drawOneMobile(m)
	}

	// Pass 3: Interleave plane-0 pictures with live mobiles by V-coordinate.
	// This is the key pass that ensures correct depth ordering.
	pi := 0 // index into sortedPics (plane-0 portion)
	mi := 0 // index into liveMobiles
	// Find the start of plane-0 pictures (already sorted by plane)
	for pi < len(sortedPics) && sortedPics[pi].Plane < 0 {
		pi++
	}
	for pi < len(sortedPics) || mi < len(liveMobiles) {
		var mV, mH int16
		haveM := mi < len(liveMobiles)
		if haveM {
			mV = liveMobiles[mi].V
			mH = liveMobiles[mi].H
		}
		var pV, pH int16
		haveP := pi < len(sortedPics) && sortedPics[pi].Plane == 0
		if haveP {
			pV = sortedPics[pi].V
			pH = sortedPics[pi].H
		}
		if haveM && (!haveP || mV < pV || (mV == pV && mH <= pH)) {
			drawOneMobile(liveMobiles[mi])
			mi++
		} else if haveP {
			drawOnePicture(sortedPics[pi])
			pi++
		} else {
			break
		}
	}

	// Pass 4: Draw plane>0 pictures (above players, like tree canopies).
	for _, p := range sortedPics {
		if p.Plane > 0 {
			drawOnePicture(p)
		}
	}

	// Pass 5: Draw chat bubbles above mobiles.
	bubbles := n.GetActiveBubbles()
	if len(bubbles) > 0 {
		drawBubbles(screen, bubbles, ds, centerX, centerY)
	}

	// Stat bars - use profile position and scale
	barW := 60
	barH := 4
	barStartX := 82
	barY := 6
	barScale := 1.0
	if gameInstance != nil && gameInstance.controls != nil {
		p := gameInstance.controls.Profile()
		if p != nil {
			barStartX = p.StatBarX
			barY = p.StatBarY
			if p.StatBarScale > 0 {
				barScale = p.StatBarScale
			} else if settings.StatBarScale > 0 {
				barScale = settings.StatBarScale
			}
		}
	}
	barW = int(float64(barW) * barScale)
	barH = int(float64(barH) * barScale)
	if barH < 2 {
		barH = 2
	}
	drawStatBar(screen, barStartX, barY, barW, barH, ds.hp, ds.hpMax, color.RGBA{R: 40, G: 180, B: 40, A: 200})
	drawStatBar(screen, barStartX, barY+int(6*barScale), barW, barH, ds.bal, ds.balMax, color.RGBA{R: 40, G: 120, B: 200, A: 200})
	drawStatBar(screen, barStartX, barY+int(12*barScale), barW, barH, ds.sp, ds.spMax, color.RGBA{R: 200, G: 40, B: 40, A: 200})
}

func drawStatBar(screen *ebiten.Image, x, y, w, h, cur, max int, c color.RGBA) {
	drawRect(screen, x, y, w, h, color.RGBA{R: 40, G: 40, B: 40, A: 200})
	if max > 0 {
		filled := w * cur / max
		if filled > w {
			filled = w
		}
		if filled > 0 {
			drawRect(screen, x, y, filled, h, c)
		}
	}
}

func colorForPict(id uint16) color.RGBA {
	r := byte((id * 7) % 200 + 30)
	g := byte((id * 13) % 180 + 40)
	b := byte((id * 3) % 160 + 50)
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func colorForMobile(descType byte, colors byte) color.RGBA {
	switch descType {
	case 1:
		return color.RGBA{R: 0, G: 120, B: 255, A: 255}
	case 2:
		return color.RGBA{R: 200, G: 40, B: 40, A: 255}
	case 3:
		return color.RGBA{R: 40, G: 200, B: 40, A: 255}
	default:
		return color.RGBA{R: 180, G: 180, B: 180, A: 255}
	}
}

// CL color code tables matching the original client.
// Back color: bits 4-7 of Colors byte. 0=white(healthy), 1=green, 2=yellow, 3=red, 4=black(dead), 5=blue(NPC), 6=grey(GM), 7=cyan(GM), 8=orange(afk).
var backColorTable = [16]color.RGBA{
	0: {R: 238, G: 238, B: 238, A: 255}, // white (healthy)
	1: {R: 102, G: 255, B: 102, A: 255}, // green (good)
	2: {R: 255, G: 255, B: 102, A: 255}, // yellow (hurting)
	3: {R: 255, G: 102, B: 102, A: 255}, // red (near death)
	4: {R: 0, G: 0, B: 0, A: 255},       // black (dead)
	5: {R: 153, G: 255, B: 255, A: 255}, // blue (NPC)
	6: {R: 102, G: 102, B: 102, A: 255}, // grey (ghosting GM)
	7: {R: 0, G: 136, B: 136, A: 255},   // cyan (GM monster)
	8: {R: 153, G: 102, B: 0, A: 255},   // orange (disconnected/afk)
}

// Text color: bits 2-3 of Colors byte.
var textColorTable = [4]color.RGBA{
	0: {R: 0, G: 0, B: 0, A: 255},       // black (normal)
	1: {R: 255, G: 255, B: 255, A: 255}, // white (dead)
	2: {R: 255, G: 0, B: 0, A: 255},     // red (bad karma)
	3: {R: 0, G: 0, B: 192, A: 255},     // blue (good karma)
}

// Friend label colors matching CL's gFriendColors.
var friendColors = [6]color.RGBA{
	1: {R: 216, G: 64, B: 64, A: 255},   // red
	2: {R: 224, G: 158, B: 32, A: 255},  // orange
	3: {R: 90, G: 165, B: 90, A: 255},   // green
	4: {R: 64, G: 32, B: 224, A: 255},   // blue
	5: {R: 137, G: 90, B: 185, A: 255},  // purple
}

// Colors byte layout: bits 4-7 = back color, bits 2-3 = text color, bits 0-1 = style.
func backColorFromColors(colors byte) color.RGBA { return backColorTable[(colors>>4)&0x0F] }
func textColorFromColors(colors byte) color.RGBA  { return textColorTable[(colors>>2)&0x03] }

// drawMobileLifeBar draws a small HP-colored bar below an unnamed mobile,
// matching the GoThoom desktop client: shown only when the Colors back color
// is not white (healthy), not blue (NPC), and not black-on-monster (dead).
func drawMobileLifeBar(screen *ebiten.Image, centerX, bottomY int, colors byte, descType byte) {
	back := int((colors >> 4) & 0x0f)
	if back == 0 || back == 5 {
		return
	}
	if back == 4 && descType == 2 {
		return
	}
	if back >= len(backColorTable) {
		back = 0
	}
	clr := backColorTable[back]
	barW := int(12 * 2.0)
	barH := int(2 * 2.0)
	left := centerX - barW/2
	top := bottomY + int(2*2.0)
	ebitenutil.DrawRect(screen, float64(left), float64(top), float64(barW), float64(barH), color.RGBA{R: clr.R, G: clr.G, B: clr.B, A: 220})
}

// drawNameTag draws a CL-style name box: HP-colored background, label-colored frame, text.
// The name is centered on centerX, positioned below topY (the bottom of the mobile).
func drawNameTag(screen *ebiten.Image, name string, centerX, topY int, colors byte, descType byte, labels map[string]int, shareWith, sharingYou map[string]bool) {
	initChatFont()
	padX := 4
	low := strings.ToLower(name)
	isShareWith := shareWith[low]
	isSharingYou := sharingYou[low]
	// Also check the Colors byte style bits: bit 1 = sharing you
	if colors&0x01 != 0 {
		isSharingYou = true
	}
	face := chatFace
	switch {
	case isShareWith && isSharingYou:
		face = chatFaceBoldItalic
	case isShareWith:
		face = chatFaceItalic
	case isSharingYou:
		face = chatFaceBold
	}
	tw, _ := text.Measure(name, face, 0)
	textW := int(tw) + padX*2
	textH := 14
	boxX := centerX - textW/2
	boxY := topY + 2

	// Background: HP color from Colors byte (always drawn, white when healthy).
	back := (colors >> 4) & 0x0F
	bgClr := backColorTable[back]
	ebitenutil.DrawRect(screen, float64(boxX), float64(boxY), float64(textW), float64(textH), bgClr)

	// Frame for labeled players
	if descType == 1 { // kDescPlayer
		label := 0
		if labels != nil {
			label = labels[strings.ToLower(name)]
		}
		if label >= kFriendLabel1 && label <= kFriendLabel5 {
			fr := friendColors[label]
			ebitenutil.DrawRect(screen, float64(boxX-1), float64(boxY-1), float64(textW+2), 1, fr)
			ebitenutil.DrawRect(screen, float64(boxX-1), float64(boxY+textH), float64(textW+2), 1, fr)
			ebitenutil.DrawRect(screen, float64(boxX-1), float64(boxY), 1, float64(textH), fr)
			ebitenutil.DrawRect(screen, float64(boxX+textW), float64(boxY), 1, float64(textH), fr)
		}
	}

	// Draw name text with color from Colors byte (bits 2-3)
	textClr := textColorTable[(colors>>2)&0x03]
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(boxX+padX), float64(boxY+1))
	op.ColorScale.ScaleWithColor(textClr)
	text.Draw(screen, name, face, op)
}

// drawBubbles renders active chat bubbles above their speakers using vector rendering.
func drawBubbles(screen *ebiten.Image, bubbles []Bubble, ds *drawState, centerX, centerY float64) {
	initBubbleFonts()
	if len(bubbles) == 0 || ds == nil {
		return
	}
	const bblPixelScale = 2.0
	now := time.Now()
	for _, b := range bubbles {
		remaining := b.Expires.Sub(now)
		if remaining <= 0 {
			continue
		}
		var px, py float64
		found := false
		if b.Far {
			px = centerX + float64(b.H)*bblPixelScale
			py = centerY + float64(b.V)*bblPixelScale
			found = true
		} else {
			for _, m := range ds.mobiles {
				if m.Index == b.DescIndex {
					px = centerX + float64(m.H)*bblPixelScale
					py = centerY + float64(m.V)*bblPixelScale
					found = true
					break
				}
			}
		}
		if !found {
			continue
		}
		borderCol, bgCol, textCol := bubbleColors(b.Type)
		drawBubble(screen, b.Text, int(px), int(py), b.Type, b.Far, b.NoArrow, borderCol, bgCol, textCol, 1.0, 1.0)
	}
}

func loadMobileSprite(cl *climg.CLImages, pictID uint16, state uint8, colors []byte) *ebiten.Image {
	sheet := cl.Get(uint32(pictID), colors, true)
	if sheet == nil {
		return nil
	}
	sx := sheet.Bounds().Dx()
	sy := sheet.Bounds().Dy()
	if sx < 18 || sy < 18 {
		return nil
	}
	innerSize := (sx - 2) / 16
	if innerSize <= 0 {
		return nil
	}
	x := 1 + int(state&0x0F)*innerSize
	y := 1 + int(state>>4)*innerSize
	if x+innerSize > sx-1 || y+innerSize > sy-1 {
		return nil
	}
	return sheet.SubImage(image.Rect(x, y, x+innerSize, y+innerSize)).(*ebiten.Image)
}

func loadPictureSprite(cl *climg.CLImages, pictID uint16, frameCounter int) *ebiten.Image {
	sheet := cl.Get(uint32(pictID), nil, false)
	if sheet == nil {
		return nil
	}
	numFrames := cl.NumFrames(uint32(pictID))
	if numFrames <= 1 {
		return sheet
	}
	frame := cl.FrameIndex(uint32(pictID), frameCounter)
	innerHeight := sheet.Bounds().Dy() - 2
	innerWidth := sheet.Bounds().Dx() - 2
	h := innerHeight / numFrames
	if h <= 0 {
		return sheet
	}
	y := 1 + frame*h
	return sheet.SubImage(image.Rect(1, y, 1+innerWidth, y+h)).(*ebiten.Image)
}
