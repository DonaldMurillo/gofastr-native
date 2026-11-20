// Package background describes deferrable Android jobs, not exact timers.
package background

import (
	"errors"
	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

const (
	Schedule   = "schedule"
	Cancel     = "cancel"
	Status     = "status"
	MaxMinutes = 525600
)

type Request struct {
	Operation        string `json:"operation"`
	Identifier       string `json:"identifier"`
	Task             string `json:"task,omitempty"`
	Periodic         bool   `json:"periodic,omitempty"`
	DelayMinutes     int    `json:"delayMinutes,omitempty"`
	IntervalMinutes  int    `json:"intervalMinutes,omitempty"`
	Network          string `json:"network,omitempty"`
	RequiresCharging bool   `json:"requiresCharging,omitempty"`
}

// State reports pending scheduler work, not a promise of execution time.
type State struct {
	Scheduled bool `json:"scheduled"`
}

func (r Request) Validate() error {
	if r.Identifier == "" || !uitext.Token(r.Identifier, 128) {
		return errors.New("background: invalid identifier")
	}
	switch r.Operation {
	case Cancel, Status:
		if r.Task != "" || r.Periodic || r.DelayMinutes != 0 || r.IntervalMinutes != 0 || r.Network != "" || r.RequiresCharging {
			return errors.New("background: only scheduling accepts job options")
		}
	case Schedule:
		if r.Task == "" || !uitext.Token(r.Task, 128) || r.DelayMinutes < 0 || r.DelayMinutes > MaxMinutes || r.IntervalMinutes < 0 || r.IntervalMinutes > MaxMinutes {
			return errors.New("background: invalid task or timing")
		}
		if r.Periodic && (r.IntervalMinutes < 15 || r.DelayMinutes != 0) || !r.Periodic && r.IntervalMinutes != 0 {
			return errors.New("background: invalid periodic timing")
		}
		if r.Network != "" && r.Network != "any" && r.Network != "unmetered" {
			return errors.New("background: invalid network")
		}
	default:
		return errors.New("background: unsupported operation")
	}
	return nil
}

func (r Request) Accept(s State) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Operation == Schedule && !s.Scheduled || r.Operation == Cancel && s.Scheduled {
		return errors.New("background: result does not match request")
	}
	return nil
}
