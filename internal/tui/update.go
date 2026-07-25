package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/walm/todomd/internal/selfupdate"
)

// updateNoticeMsg carries the upgrade hint, or "" when there's nothing to say.
type updateNoticeMsg string

// checkUpdateCmd refreshes the cached release check and reports any hint. It
// runs as a bubbletea command, i.e. on its own goroutine: the board is already
// interactive while this is in flight, so a slow or unreachable network can
// never delay startup. The refresh itself no-ops unless the cache is stale.
func checkUpdateCmd(current string) tea.Cmd {
	return func() tea.Msg {
		if selfupdate.Disabled(current) {
			return updateNoticeMsg("")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		selfupdate.RefreshCache(ctx, current, time.Now())
		return updateNoticeMsg(selfupdate.Notice(current))
	}
}
