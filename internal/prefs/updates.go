package prefs

import (
	"encoding/json"
	"fmt"
)

// UpdatesKey is where the update-check settings live in the preferences
// document (Preferences.Extra), beside the recent-workspaces list. They are
// per person and never part of a workspace or slaw.yaml.
const UpdatesKey = "updates"

// Update-check modes for UpdateSettings.Check. Unset ("") means never asked.
const (
	UpdateCheckOff    = "off"
	UpdateCheckWeekly = "weekly"
)

// UpdateSettings is the person's answer about checking GitHub for new releases.
type UpdateSettings struct {
	// Check is "" (never asked), "off" or "weekly".
	Check string `json:"check"`
	// LastChecked is the RFC 3339 time of the last successful check.
	LastChecked string `json:"lastChecked"`
	// SkippedVersion is a release tag the person chose to skip.
	SkippedVersion string `json:"skippedVersion"`
}

// Normalize drops an unrecognised Check value back to "never asked".
func (s UpdateSettings) Normalize() UpdateSettings {
	if s.Check != UpdateCheckOff && s.Check != UpdateCheckWeekly {
		s.Check = ""
	}
	return s
}

func updatesFrom(p Preferences) UpdateSettings {
	var s UpdateSettings
	if raw, ok := p.Extra[UpdatesKey]; ok {
		_ = json.Unmarshal(raw, &s) // a bad value reads as "never asked"
	}
	return s.Normalize()
}

// UpdateSettings returns the saved update-check settings (zero value when none).
func (r *Recents) UpdateSettings() UpdateSettings {
	if !r.has() {
		return UpdateSettings{}
	}
	p := r.load()
	return updatesFrom(p)
}

// ModifyUpdateSettings applies fn to the saved settings and writes them back
// through Store.Modify, so a stale copy never rolls back another key or another
// process's change.
func (r *Recents) ModifyUpdateSettings(fn func(*UpdateSettings)) error {
	err := r.modify(func(p *Preferences) error {
		s := updatesFrom(*p)
		fn(&s)
		raw, err := json.Marshal(s.Normalize())
		if err != nil {
			return fmt.Errorf("failed to encode update settings: %w", err)
		}
		if p.Extra == nil {
			p.Extra = map[string]json.RawMessage{}
		}
		p.Extra[UpdatesKey] = raw
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save update settings: %w", err)
	}
	return nil
}
