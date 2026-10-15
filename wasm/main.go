package main

import (
	"fmt"
	"net/http"
	"time"

	spinhttp "github.com/spinframework/spin-go-sdk/v2/http"
)

// Moscow is UTC+3 all year (no DST since 2014). A fixed zone avoids
// time.LoadLocation, which needs tzdata that a Wasm module doesn't ship.
var msk = time.FixedZone("MSK", 3*60*60)

func init() {
	spinhttp.Handle(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprintln(w, `{"error":"method not allowed"}`)
			return
		}
		now := time.Now().In(msk)
		w.Header().Set("Content-Type", "application/json")
		// Build JSON by hand: no reflection, works on every Go/TinyGo target.
		fmt.Fprintf(w, `{"unix":%d,"iso":%q,"hour_minute":%q,"timezone":"Europe/Moscow","utc_offset":"+03:00"}`+"\n",
			now.Unix(), now.Format(time.RFC3339), now.Format("15:04"))
	})
}

func main() {}
