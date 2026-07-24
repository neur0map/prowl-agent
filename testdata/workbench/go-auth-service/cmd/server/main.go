package main

import (
	"log"

	"example.com/go-auth-service/internal/httpapi"
)

func main() {
	if err := httpapi.Run(); err != nil {
		log.Fatal(err)
	}
}
