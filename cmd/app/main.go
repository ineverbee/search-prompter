package main

import (
	"time"

	"github.com/ineverbee/search-prompter/internal/app"
)

func main() {
	ticker := time.NewTicker(5 * time.Second)
	quit := make(chan struct{})
LOOP:
	for {
		select {
		case <-ticker.C:
			go app.Ping("pyapp:80/ping", quit)
		case <-quit:
			ticker.Stop()
			break LOOP
		}
	}
	app.TeaUI()
}
