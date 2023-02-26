package inits

import (
	"log"

	"github.com/joho/godotenv"
)

func Dotenv() {
	err := godotenv.Load()
	if err != nil {
		log.Panicf("Error loading .env file: %s", err.Error())
	}
}
