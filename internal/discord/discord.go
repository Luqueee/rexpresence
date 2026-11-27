package discord

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
	"unicode"

	"rexpresence/internal/presence"
)

const ioTimeout = 300 * time.Millisecond

const (
	opHandshake uint32 = iota
	opFrame
	opClose
	opPing
	opPong
)

type timestamps struct {
	Start int64 `json:"start"`
}

type activityAssets struct {
	LargeImage string `json:"large_image"`
}

type activityPayload struct {
	Details    string          `json:"details,omitempty"`
	State      string          `json:"state,omitempty"`
	Timestamps *timestamps     `json:"timestamps,omitempty"`
	Assets     *activityAssets `json:"assets,omitempty"`
}

type activityArgs struct {
	PID      int              `json:"pid"`
	Activity *activityPayload `json:"activity"`
}

type activityCommand struct {
	Cmd   string       `json:"cmd"`
	Args  activityArgs `json:"args"`
	Nonce string       `json:"nonce"`
}

type discordEvent struct {
	op   uint32
	body []byte
}

func writeFrame(conn net.Conn, op uint32, body []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(ioTimeout)); err != nil {
		return err
	}
	frame := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(frame, op)
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(body)))
	copy(frame[8:], body)
	for len(frame) > 0 {
		n, err := conn.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func readFrames(conn net.Conn, events chan<- discordEvent, done <-chan struct{}) {
	defer close(events)
	for {
		var header [8]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}
		size := binary.LittleEndian.Uint32(header[4:])
		if size > 1<<20 {
			return
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		event := discordEvent{op: binary.LittleEndian.Uint32(header[:]), body: body}
		select {
		case events <- event:
		case <-done:
			return
		}
	}
}

// Discord counts text in UTF-16 units. Never split a code point at the limit.
func limitLine(s string) string {
	units := 0
	for i, r := range s {
		units++
		if r > 0xffff {
			units++
		}
		if units > 128 {
			return s[:i]
		}
	}
	return s
}

func renderLine(line string, p *presence.Activity) string {
	var result strings.Builder
	for {
		before, rest, found := strings.Cut(line, "{")
		if !found {
			if result.Len() == 0 {
				return limitLine(before)
			}
			result.WriteString(before)
			return limitLine(result.String())
		}
		token, after, ok := strings.Cut(rest, "}")
		if !ok {
			return ""
		}
		var value string
		switch token {
		case "activity", "tool":
			value = p.Details
			if token == "activity" && p.Project != "" {
				value += " · " + p.Project
			}
		case "group":
			group := strings.ToValidUTF8(p.Group, "")
			value = strings.Join(strings.FieldsFunc(group, func(r rune) bool {
				return unicode.IsSpace(r) || unicode.IsControl(r)
			}), " ")
		case "project":
			value = p.Project
		}
		if value == "" {
			return "" // Hide the whole line rather than leaving a dangling label.
		}
		result.WriteString(before)
		result.WriteString(limitLine(value))
		line = after
	}
}

// Render builds the same activity for the local preview and Discord IPC.
func Render(p *presence.Activity, logo string) *activityPayload {
	if p == nil {
		return nil
	}
	card := presence.DefaultCard()
	if p.Card != nil {
		card = *p.Card
	}
	activity := &activityPayload{}
	if p.Custom {
		activity.Details = limitLine(p.Details)
	} else {
		activity.Details = renderLine(card.Line1, p)
		activity.State = renderLine(card.Line2, p)
	}
	if card.Logo != "{default}" {
		logo = card.Logo
	}
	if logo != "" {
		activity.Assets = &activityAssets{LargeImage: logo}
	}
	if card.Timer {
		activity.Timestamps = &timestamps{Start: p.Started}
	}
	return activity
}

func setActivity(conn net.Conn, p *presence.Activity, logo string, nonce uint64) error {
	body, err := json.Marshal(activityCommand{
		Cmd:   "SET_ACTIVITY",
		Args:  activityArgs{PID: os.Getpid(), Activity: Render(p, logo)},
		Nonce: fmt.Sprint(nonce),
	})
	if err != nil {
		return err
	}
	return writeFrame(conn, opFrame, body)
}

// Publish sends activity updates to Discord and reconnects when it restarts.
func Publish(ctx context.Context, clientID, logo string, updates <-chan *presence.Activity, report func(string)) error {
	var current *presence.Activity
	var conn net.Conn
	var events <-chan discordEvent
	var done chan struct{}
	var ready bool
	var nonce uint64
	var pending string
	var status string
	setStatus := func(next string) {
		if status != next {
			status = next
			report(next)
		}
	}
	setStatus("Waiting for active presence")
	var handshakeTimer *time.Timer
	var handshakeDeadline <-chan time.Time
	closeConn := func(clear bool) {
		pending = ""
		if conn == nil {
			return
		}
		if clear && ready {
			nonce++
			_ = setActivity(conn, nil, logo, nonce)
		}
		_ = conn.Close()
		if done != nil {
			close(done)
		}
		conn, events, done, ready = nil, nil, nil, false
		setStatus("Discord disconnected")
		if handshakeTimer != nil {
			handshakeTimer.Stop()
			handshakeTimer = nil
			handshakeDeadline = nil
		}
	}
	sendCurrent := func() {
		nonce++
		if setActivity(conn, current, logo, nonce) != nil {
			closeConn(false)
			return
		}
		pending = fmt.Sprint(nonce)
		setStatus("Awaiting Discord acknowledgement")
	}
	defer closeConn(true)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	connect := func() {
		if current == nil || conn != nil {
			return
		}
		setStatus("Connecting to Discord")
		dialCtx, cancel := context.WithTimeout(ctx, ioTimeout)
		c, err := dialIPC(dialCtx)
		cancel()
		if err != nil {
			setStatus("Discord disconnected")
			return
		}
		conn = c
		body, _ := json.Marshal(struct {
			Version  int    `json:"v"`
			ClientID string `json:"client_id"`
		}{1, clientID})
		if writeFrame(conn, opHandshake, body) != nil {
			closeConn(false)
			return
		}
		ch := make(chan discordEvent)
		events, done = ch, make(chan struct{})
		go readFrames(conn, ch, done)
		handshakeTimer = time.NewTimer(3 * time.Second)
		handshakeDeadline = handshakeTimer.C
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case p, ok := <-updates:
			if !ok {
				return nil
			}
			if p == nil {
				current = nil
				closeConn(true)
				setStatus("No active presence")
				continue
			}
			current = p
			if ready {
				sendCurrent()
			} else {
				connect()
			}
		case <-ticker.C:
			connect()
		case <-handshakeDeadline:
			closeConn(false)
		case event, ok := <-events:
			if !ok {
				closeConn(false)
				continue
			}
			switch event.op {
			case opFrame:
				var frame struct {
					Cmd   string `json:"cmd"`
					Nonce string `json:"nonce"`
					Event string `json:"evt"`
					Data  struct {
						Code int `json:"code"`
					} `json:"data"`
				}
				if json.Unmarshal(event.body, &frame) != nil {
					closeConn(false)
					continue
				}
				if frame.Event == "ERROR" {
					return fmt.Errorf("Discord RPC error (code %d)", frame.Data.Code)
				}
				if frame.Cmd == "SET_ACTIVITY" && frame.Event == "" && pending != "" && frame.Nonce == pending {
					pending = ""
					setStatus("Discord accepted activity")
				}
				if !ready && frame.Event == "READY" {
					ready = true
					handshakeTimer.Stop()
					handshakeTimer = nil
					handshakeDeadline = nil
					sendCurrent()
				}
			case opClose:
				closeConn(false)
			case opPing:
				if writeFrame(conn, opPong, event.body) != nil {
					closeConn(false)
				}
			case opPong:
			default:
				closeConn(false)
			}
		}
	}
}

var errDiscordUnavailable = errors.New("Discord IPC unavailable")
