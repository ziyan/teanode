package place

import (
	"context"
	"testing"
)

// searchRecorder answers a search only for the words it knows, and keeps every
// query it was asked.
type searchRecorder struct {
	answers map[string][]*Found
	asked   []string
}

func (self *searchRecorder) Name(context.Context, float64, float64) (*Found, error) { return nil, nil }

func (self *searchRecorder) Addresses(context.Context, string, int) ([]*Found, error) {
	return nil, nil
}

func (self *searchRecorder) Search(_ context.Context, query string, _, _, _ float64, _ int) ([]*Found, error) {
	self.asked = append(self.asked, query)
	return self.answers[query], nil
}

func TestAnEverydayWordForAKindIsSearchedAgainInOpenStreetMapsWord(t *testing.T) {
	recorder := &searchRecorder{answers: map[string][]*Found{"cafe": {{PlaceName: "Corner Cafe"}}}}
	found, err := SearchKinds(context.Background(), recorder, " Coffee ", 10.5, 20.5, 1000, 5)
	if err != nil || len(found) != 1 || found[0].PlaceName != "Corner Cafe" {
		t.Fatalf("coffee did not find the cafe: %v %v", found, err)
	}
	if len(recorder.asked) != 2 || recorder.asked[1] != "cafe" {
		t.Errorf("asked %v, want the words first and then cafe", recorder.asked)
	}
}

func TestWordsThatFindSomethingOrHaveNoKindAreAskedOnce(t *testing.T) {
	recorder := &searchRecorder{answers: map[string][]*Found{"coffee": {{PlaceName: "Coffee Corner"}}}}
	if found, _ := SearchKinds(context.Background(), recorder, "coffee", 10.5, 20.5, 1000, 5); len(found) != 1 || len(recorder.asked) != 1 {
		t.Errorf("a search that found something was asked again: %v", recorder.asked)
	}
	recorder.asked = nil
	if found, _ := SearchKinds(context.Background(), recorder, "Maple Bakery", 10.5, 20.5, 1000, 5); len(found) != 0 || len(recorder.asked) != 1 {
		t.Errorf("a name with no kind word was asked again: %v", recorder.asked)
	}
}
