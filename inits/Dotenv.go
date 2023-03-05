package inits

import (
	"log"

	"github.com/joho/godotenv"
)

func Dotenv() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No .env file was found. The application will use the default env cofiguration.")
	}
}
