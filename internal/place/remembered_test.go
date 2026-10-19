package place

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRememberedAddressesRoundToTenMetersAndExpireAfterADay(test *testing.T) {
	var remembered RememberedAddresses
	now := time.Date(2030, 3, 5, 9, 0, 0, 0, time.UTC)
	if _, isRemembered := remembered.Get(11.5, 21.5, now); isRemembered {
		test.Fatal("nothing was remembered yet")
	}
	remembered.Remember(11.50001, 21.50002, "1 Fixture Road", now)
	if addressText, isRemembered := remembered.Get(11.5, 21.5, now.Add(time.Hour)); !isRemembered || addressText != "1 Fixture Road" {
		test.Fatalf("got %q %v", addressText, isRemembered)
	}
	if _, isRemembered := remembered.Get(11.5002, 21.5, now); isRemembered {
		test.Fatal("a position 20 meters away is another position")
	}
	if _, isRemembered := remembered.Get(11.5, 21.5, now.Add(25*time.Hour)); isRemembered {
		test.Fatal("an address is kept for a day")
	}
	remembered.Remember(12, 22, "", now)
	if addressText, isRemembered := remembered.Get(12, 22, now); !isRemembered || addressText != "" {
		test.Fatalf("an empty answer is remembered too: %q %v", addressText, isRemembered)
	}
}

func TestRememberedAddressesDropTheOldestPastTheLimit(test *testing.T) {
	var remembered RememberedAddresses
	now := time.Now()
	for index := 0; index < rememberedMostCount+10; index++ {
		remembered.Remember(float64(index)/1000, 1, fmt.Sprintf("address %d", index), now)
	}
	if _, isRemembered := remembered.Get(0, 1, now); isRemembered {
		test.Fatal("the oldest was kept")
	}
	if _, isRemembered := remembered.Get(float64(rememberedMostCount+9)/1000, 1, now); !isRemembered {
		test.Fatal("the newest was dropped")
	}
	if len(remembered.entries) != rememberedMostCount || len(remembered.order) != rememberedMostCount {
		test.Fatalf("%d entries, %d keys", len(remembered.entries), len(remembered.order))
	}
}

func TestRememberedAddressesAreSafeForConcurrentUse(test *testing.T) {
	var remembered RememberedAddresses
	var group sync.WaitGroup
	now := time.Now()
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := 0; index < 500; index++ {
				remembered.Remember(float64(index)/100, float64(worker), "x", now)
				remembered.Get(float64(index)/100, float64(worker), now)
			}
		}()
	}
	group.Wait()
}
