package proposals

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/slaw/internal/semantics"
)

// Pin names one proposal a person reviewed and the ShownRev of the Show they
// reviewed it in.
type Pin struct {
	ID       string `json:"id"`
	ShownRev string `json:"shown_rev"`
}

// Accepted is a proposal AcceptMany wrote (or found already written).
type Accepted struct {
	ID     string         `json:"id"`
	Kind   semantics.Kind `json:"kind"`
	Name   string         `json:"name"`
	Target string         `json:"target"`
}

// ManyResult is what AcceptMany did: the proposals accepted, in order, and, when
// it stopped early, the one that stopped it and why. FailedID is "" and Err is
// nil when every pin was accepted.
type ManyResult struct {
	Accepted []Accepted `json:"accepted"`
	FailedID string     `json:"failed_id,omitempty"`
	Err      error      `json:"-"`
}

// AcceptMany accepts the pinned proposals in order, each through Accept, so
// every one is compare-and-write against the version its pin names. It stops
// at the first that is refused (a stale target, a version that changed since
// it was shown, an invalid or no longer open proposal) and leaves the rest
// untouched; the earlier ones stay accepted. It never overwrites. The pins
// must come from what the person was shown: AcceptMany does not look at a
// proposal first. A target written by an earlier pin in the same run (two
// proposals for one file) makes the later one fail its pin, so it stops
// instead of replacing the first. Only a person's surface calls this.
func AcceptMany(ctx context.Context, props Store, sem semantics.Store, pins []Pin, show ...ShowOption) *ManyResult {
	res := &ManyResult{Accepted: []Accepted{}}
	for _, pin := range pins {
		if err := ctx.Err(); err != nil {
			res.FailedID, res.Err = pin.ID, fmt.Errorf("stopped before %s: %w", pin.ID, err)
			return res
		}
		pv, err := Accept(ctx, props, sem, pin.ID, AcceptOptions{ShownRev: pin.ShownRev}, show...)
		if err != nil {
			res.FailedID, res.Err = pin.ID, err
			return res
		}
		p := pv.Proposal
		res.Accepted = append(res.Accepted, Accepted{ID: p.ID, Kind: p.Kind, Name: p.Name, Target: pv.Target})
	}
	return res
}
