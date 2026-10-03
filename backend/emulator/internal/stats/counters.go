package stats

import (
	"sync/atomic"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
)

// Counters are the run's totals. Every field is safe for concurrent use.
type Counters struct {
	Started        atomic.Int64 // cabbers that reached Active at least once
	Active         atomic.Int64 // cabbers in Active now
	Failed         atomic.Int64 // cabbers that never started
	Lost           atomic.Int64 // cabbers whose session could not be restored
	OnSchedule     atomic.Int64 // cabbers that began within one second of their ramp-up moment
	Registered     atomic.Int64
	RegisterFailed atomic.Int64
	LoggedIn       atomic.Int64
	LoginFailed    atomic.Int64
	Relogin        atomic.Int64
	SentOK         atomic.Int64
	Skipped        atomic.Int64
	LoggedOut      atomic.Int64
	LogoutFailed   atomic.Int64
	NotLoggedOut   atomic.Int64

	sendFailed [gateway.Canceled + 1]atomic.Int64

	SendLatency Histogram // successful and failed location sends
	AuthLatency Histogram // registration and session creation
	DispatchLag Histogram // planned send moment to the moment the request is handed to the client
}

// SendFailed counts a failed location send by the kind of failure. A canceled send is the run
// stopping, not a failure of the system, and is not counted.
func (c *Counters) SendFailed(kind gateway.Kind) {
	if kind == gateway.OK || kind == gateway.Canceled {
		return
	}
	c.sendFailed[kind].Add(1)
}

// SendFailedTotal is the number of failed sends of every kind so far.
func (c *Counters) SendFailedTotal() int64 {
	var total int64
	for kind := gateway.OK; kind <= gateway.Canceled; kind++ {
		total += c.sendFailed[kind].Load()
	}
	return total
}

// Snapshot is a plain copy of the counters for reports.
type Snapshot struct {
	Started, Active, Failed, Lost, OnSchedule         int64
	Registered, RegisterFailed, LoggedIn, LoginFailed int64
	Relogin, SentOK, Skipped                          int64
	LoggedOut, LogoutFailed, NotLoggedOut             int64
	SendFailed                                        map[string]int64 // only non-zero kinds
	SendFailedTotal                                   int64
	SendLatency, AuthLatency, DispatchLag             Summary
}

// Snapshot reads every counter once.
func (c *Counters) Snapshot() Snapshot {
	s := Snapshot{
		Started: c.Started.Load(), Active: c.Active.Load(), Failed: c.Failed.Load(), Lost: c.Lost.Load(),
		OnSchedule: c.OnSchedule.Load(), Registered: c.Registered.Load(), RegisterFailed: c.RegisterFailed.Load(),
		LoggedIn: c.LoggedIn.Load(), LoginFailed: c.LoginFailed.Load(), Relogin: c.Relogin.Load(),
		SentOK: c.SentOK.Load(), Skipped: c.Skipped.Load(), LoggedOut: c.LoggedOut.Load(),
		LogoutFailed: c.LogoutFailed.Load(), NotLoggedOut: c.NotLoggedOut.Load(),
		SendFailed:  map[string]int64{},
		SendLatency: c.SendLatency.Summary(), AuthLatency: c.AuthLatency.Summary(), DispatchLag: c.DispatchLag.Summary(),
	}
	for kind := gateway.OK; kind <= gateway.Canceled; kind++ {
		if n := c.sendFailed[kind].Load(); n > 0 {
			s.SendFailed[kind.String()] = n
			s.SendFailedTotal += n
		}
	}
	return s
}
