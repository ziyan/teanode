package memory

import (
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A memory always reaches the conversation, whatever runs the model
// addressed it to, and a name that is not an audience is dropped.
func TestMemoryAudiencesAlwaysIncludeTheConversation(t *testing.T) {
	got := factAudiences([]string{"Triage", " reply ", "nonsense", "ask"})
	want := []models.AgentAudience{models.AudienceAsk, models.AudienceTriage, models.AudienceReply}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("factAudiences = %v, want %v", got, want)
	}
	if got := factAudiences(nil); !reflect.DeepEqual(got, []models.AgentAudience{models.AudienceAsk}) {
		t.Fatalf("an unaddressed memory should reach the conversation, got %v", got)
	}
}
