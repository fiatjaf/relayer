package main

import "time"

// every hour, delete all very old events
func cleanupRoutine() {
	for {
		time.Sleep(60 * time.Minute)
		db.Exec(`DELETE FROM event WHERE created_at < $1`, time.Now().AddDate(0, -3, 0))
	}
}
