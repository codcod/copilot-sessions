package cli

import (
	"time"

	"github.com/codcod/copilot-sessions/internal/filter"
	"github.com/codcod/copilot-sessions/internal/session"
)

// dateFlag is a pflag.Value that parses a DATE into *p, using now for "today".
type dateFlag struct {
	p   *filter.Date
	now func() time.Time
}

func (f *dateFlag) Set(s string) error {
	d, err := filter.ParseDate(s, f.now())
	if err == nil {
		*f.p = d
	}
	return err
}

// String is "" while unset, so help shows no "(default 0000-00-00)".
func (f *dateFlag) String() string {
	if f.p == nil || f.p.IsZero() {
		return ""
	}
	return f.p.String()
}

func (*dateFlag) Type() string { return "DATE" }

// statusFlag is a pflag.Value that parses a status list into *p.
type statusFlag struct{ p *map[session.Status]bool }

func (f *statusFlag) Set(s string) error {
	set, err := filter.ParseStatuses(s)
	if err == nil {
		*f.p = set
	}
	return err
}

func (f *statusFlag) String() string {
	if f.p == nil {
		return ""
	}
	return filter.StatusList(*f.p, ",")
}

func (*statusFlag) Type() string { return "LIST" }
