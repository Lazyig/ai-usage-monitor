package antigravity

import (
	"path/filepath"
	"testing"
)

// The desktop app keeps conversations under ~/.gemini/antigravity with the
// CLI's layout. With the CLI absent (the common case), the app root alone must
// yield the same events as scanning it directly.
func TestExtraRootIsScannedWhenTheCLIRootIsMissing(t *testing.T) {
	app := t.TempDir()
	buildStandardConversation(t, app)
	writeSummaries(t, app, map[string]string{testConvID: "file:///config/workspace"})

	direct, _ := scan(t, New(app), nil)
	cliMissing := filepath.Join(t.TempDir(), "antigravity-cli")
	both, cur := scan(t, New(cliMissing, app), nil)

	if len(both) == 0 || len(both) != len(direct) {
		t.Fatalf("CLI+app gave %d events, app alone %d", len(both), len(direct))
	}
	for i := range direct {
		if both[i].EventID != direct[i].EventID || both[i].Project != direct[i].Project {
			t.Errorf("event %d differs: %+v vs %+v", i, both[i], direct[i])
		}
	}

	again, _ := scan(t, New(cliMissing, app), cur)
	if len(again) != 0 {
		t.Fatalf("second scan re-emitted %d events", len(again))
	}
}

// Conversations from both roots are collected, and a root listed twice is
// read once.
func TestEveryRootContributes(t *testing.T) {
	cli, app := t.TempDir(), t.TempDir()
	buildStandardConversation(t, app)
	other := newConversation(t, cli, "11111111-2222-3333-4444-555555555555")
	insertGen(t, other, 0, encGen(genFixture{
		stepUUID: "uuid-cli", model: "gemini-3.7-flash", trajectoryID: "traj-cli",
		usage: usageFixture{uncached: 10, output: 5, response: 5, requestID: "req-cli-0"},
	}))
	insertStep(t, other, 0, stepTypeModelGeneration, encStep(stepFixture{
		uuid: "uuid-cli", ts: baseTime, trajectoryID: "traj-cli", conversationID: "11111111-2222-3333-4444-555555555555",
		usage: &usageFixture{uncached: 10, output: 5, response: 5, requestID: "req-cli-0"},
	}))

	appOnly, _ := scan(t, New(app), nil)
	all, _ := scan(t, New(cli, app, app), nil)
	if len(all) != len(appOnly)+1 {
		t.Fatalf("got %d events, want the app's %d plus 1 from the CLI", len(all), len(appOnly))
	}
}
