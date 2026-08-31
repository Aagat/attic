package domain

import "testing"

func TestStatusLifecycleClassification(t *testing.T) {
	for _, status := range []Status{StatusReady, StatusDelivered, StatusDeliveryFailed, StatusFailed, StatusCancelled} {
		if !status.Valid() || !status.Terminal() {
			t.Fatalf("%q should be a valid terminal status", status)
		}
	}
	for _, status := range []Status{StatusQueued, StatusProcessing, StatusDelivering} {
		if !status.Valid() || status.Terminal() {
			t.Fatalf("%q should be a valid non-terminal status", status)
		}
	}
}
