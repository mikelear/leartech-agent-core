package gatewaytools

import (
	"fmt"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"
)

// recordingUI captures what a caller says about itself via Note — the shared
// test double lifted from the shell's usagemeter_test.go, so the ported tests
// assert the same announcements.
type recordingUI struct {
	agentloop.Quiet
	notes []string
}

func (u *recordingUI) Note(format string, a ...any) {
	u.notes = append(u.notes, fmt.Sprintf(format, a...))
}
