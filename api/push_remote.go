package main

// Probe control through the VPS, the home PC side (remote.go is the VPS side). This PC reports its Admin state to
// the VPS (when it changes, and every 30 s so the VPS knows the PC is on). With VPS_REMOTE_CONTROL=1 it also
// collects the orders given on the VPS Admin page (the VPS holds the request open until one arrives, so they land
// within a second or two) and carries them out exactly like this PC's own Admin page: by writing the probe's
// request files. The probe's own safety rules still decide what happens.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const (
	snapEvery     = 5 * time.Second  // report the Admin state when it changed...
	snapHeartbeat = 30 * time.Second // ...and at least this often
	commandWait   = 25               // seconds the VPS holds an orders request open
)

type remoteState struct {
	reqDir     string
	adminFn    func() Admin
	accept     bool
	lastSnap   []byte
	lastSnapAt time.Time
	snapNow    chan struct{}
	applied    map[string]time.Time // orders already carried out, in case an ack got lost
}

// Remote sets up probe control through the VPS: report admin() there, and with accept carry out its orders,
// writing the probe's request files in reqDir.
func (p *Pusher) Remote(reqDir string, admin func() Admin, accept bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remote = remoteState{reqDir: reqDir, adminFn: admin, accept: accept, snapNow: make(chan struct{}, 1), applied: map[string]time.Time{}}
	p.status.Orders = accept
}

func (p *Pusher) startRemote() {
	if p.remote.adminFn == nil {
		return
	}
	go p.snapshotLoop()
	if p.remote.accept {
		go p.commandLoop()
	}
}

func (p *Pusher) snapshotLoop() {
	for {
		wait := snapEvery
		if err := p.SendSnapshot(time.Now()); err != nil {
			wait = snapHeartbeat
		}
		select {
		case <-time.After(wait):
		case <-p.remote.snapNow:
		}
	}
}

// SendSnapshot reports this PC's Admin state to the VPS when it changed, or when the last report is getting old.
func (p *Pusher) SendSnapshot(now time.Time) error {
	a := p.remote.adminFn()
	a.Remote = nil
	b, err := json.Marshal(Snapshot{Admin: a, Pull: ReadPull(p.remote.reqDir), RemoteControl: p.remote.accept})
	if err != nil {
		return err
	}
	p.mu.Lock()
	same := bytes.Equal(b, p.remote.lastSnap) && now.Sub(p.remote.lastSnapAt) < snapHeartbeat
	p.mu.Unlock()
	if same {
		return nil
	}
	if err := p.ok("PUT", "admin", b, "application/json"); err != nil {
		return err
	}
	p.mu.Lock()
	p.remote.lastSnap, p.remote.lastSnapAt = b, now
	p.mu.Unlock()
	return nil
}

func (p *Pusher) commandLoop() {
	backoff := 5 * time.Second
	for {
		if err := p.PollCommands(commandWait); err != nil {
			time.Sleep(backoff)
			backoff = min(2*backoff, time.Minute)
			continue
		}
		backoff = 5 * time.Second
	}
}

// PollCommands collects the VPS's orders (it holds the request up to waitSec for one), carries them out and
// reports how each went.
func (p *Pusher) PollCommands(waitSec int) error {
	res, err := p.call("GET", fmt.Sprintf("commands?wait=%d", waitSec), nil, "")
	if err != nil {
		return err
	}
	var body struct {
		Commands []Command `json:"commands"`
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return fmt.Errorf("orders: HTTP %d", res.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body)
	res.Body.Close()
	if err != nil || len(body.Commands) == 0 {
		return err
	}
	results := []CommandResult{}
	for _, c := range body.Commands {
		p.mu.Lock()
		_, done := p.remote.applied[c.ID]
		p.mu.Unlock()
		r := CommandResult{ID: c.ID, Action: c.Action, At: time.Now()}
		if !done { // a repeat means the last ack got lost: it was carried out already
			if err := p.apply(c); err != nil {
				r.Error = err.Error()
				log.Printf("order from the VPS: %s, failed: %v", c.Action, err)
			} else {
				log.Printf("order from the VPS: %s", c.Action)
			}
			p.mu.Lock()
			p.remote.applied[c.ID] = time.Now()
			for id, at := range p.remote.applied {
				if time.Since(at) > time.Hour {
					delete(p.remote.applied, id)
				}
			}
			last := r
			p.status.LastOrder = &last
			p.mu.Unlock()
		}
		results = append(results, r)
	}
	b, _ := json.Marshal(map[string][]CommandResult{"results": results})
	if err := p.ok("POST", "commands/ack", b, "application/json"); err != nil {
		return err
	}
	select { // show the outcome on the VPS right away
	case p.remote.snapNow <- struct{}{}:
	default:
	}
	return nil
}

// apply carries out one order exactly as this PC's own Admin page would.
func (p *Pusher) apply(c Command) error {
	now, dir := time.Now(), p.remote.reqDir
	switch c.Action {
	case "control":
		if c.Patch == nil {
			return errors.New("empty settings change")
		}
		_, err := UpdateControl(dir, *c.Patch, now)
		return err
	case "start":
		return RequestStart(dir, now)
	case "login":
		return RequestLogin(dir, now)
	case "resume":
		return RequestResume(p.dataDir, dir, c.Stop)
	case "pull":
		_, err := RequestPull(dir, now)
		return err
	case "loop":
		return RequestLoop(dir, c.Loop)
	}
	return fmt.Errorf("unknown order %q", c.Action)
}
