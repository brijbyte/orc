package agent

import (
	"strings"
	"testing"
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
