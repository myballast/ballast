// Command ci-demo is the target of the example of "Run Ballast from CI": a small service
// with a fast endpoint, a slow one on demand, and the file that proves it is yours.
package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	// BALLAST_CHALLENGE_TOKEN is what Ballast asks you to serve, when you verify the target.
	token := os.Getenv("BALLAST_CHALLENGE_TOKEN")
	if _, err := newChallenge(token); err != nil {
		log.Printf("ci-demo: %v: the challenge is not served", err)
	}

	server := &http.Server{
		Addr:              listenAddress(os.Getenv("PORT")),
		Handler:           NewHandler(token),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("ci-demo: listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
