// CGI-style WASI CLI: request comes from env vars, response goes to stdout.
package main

import (
	"fmt"
	"os"
	"time"
)

// Same logic as the Spin component: fixed UTC+3, hand-built JSON.
var msk = time.FixedZone("MSK", 3*60*60)

func main() {
	method, path := os.Getenv("REQUEST_METHOD"), os.Getenv("PATH_INFO")
	if method != "GET" || path != "/time" {
		fmt.Print("Status: 404 Not Found\r\nContent-Type: application/json\r\n\r\n")
		fmt.Printf(`{"error":"no route","method":%q,"path":%q}`+"\n", method, path)
		return
	}
	now := time.Now().In(msk)
	fmt.Print("Content-Type: application/json\r\n\r\n")
	fmt.Printf(`{"unix":%d,"iso":%q,"hour_minute":%q,"timezone":"Europe/Moscow","utc_offset":"+03:00"}`+"\n",
		now.Unix(), now.Format(time.RFC3339), now.Format("15:04"))
}
