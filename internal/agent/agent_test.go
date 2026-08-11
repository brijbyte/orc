package agent

import (
	"strings"
	"testing"

	"github.com/brijbyte/orc/internal/notify"
)

func TestServerPathAttachmentDoesNotLoadContent(t *testing.T) {
	path := "/workspace/src"
	attachment := Attachment{Name: path, Path: path, Data: []byte("must not be sent")}
	message := string(userMessage("review", []Attachment{attachment}))
	if !strings.Contains(message, "[attached server path: "+path+"]") || strings.Contains(message, "must not be sent") {
		t.Fatalf("userMessage() = %s", message)
	}
	if got := Echo("review", []Attachment{attachment}); got != "review\n📎 "+path {
		t.Fatalf("Echo() = %q", got)
	}
}

func TestPresenceFollowsAttachment(t *testing.T) {
	away := presence()
	detach := notify.Attach()
	watched := presence()
	detach()
	if watched == away {
		t.Fatal("presence did not change when a UI attached")
	}
	if presence() != away {
		t.Fatal("presence did not revert after detach")
	}
}
