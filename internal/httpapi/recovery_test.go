package httpapi

import "testing"

func TestRecoveryRequiresAuthentication(t *testing.T) {
	server, _, _ := testServer(t)
	for _, method := range []string{"GET", "POST"} {
		response := request(server, method, "/api/v1/items/saved/recovery", "", `{"action":"start"}`)
		if response.Code != 401 {
			t.Fatalf("%s recovery without authentication returned %d", method, response.Code)
		}
	}
}
