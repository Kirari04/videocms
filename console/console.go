package main

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"os"
)

func main() {
	// for loading the .env file into the application
	inits.Dotenv()
	// for setting up the database connection
	inits.Database()

	// load cmd arguments
	argsWithoutProg := os.Args[1:]

	if len(argsWithoutProg) == 0 {
		log.Println("No arguments passed")
		functions()
		return
	}

	// loop over arguments and exec all matching functions
	for _, v := range argsWithoutProg {
		switch v {
		case "seed:adminuser":
			log.Println("running seed:adminuser")
			res := inits.DB.Where(&models.User{Username: "admin"}).Unscoped().Delete(&models.User{})
			if res.Error != nil {
				log.Printf("Error while deleting existing admin user: %s", res.Error.Error())
			}

			hash, _ := helpers.HashPassword("12345678")
			inits.DB.Create(&models.User{
				Username: "admin",
				Hash:     hash,
				Admin:    true,
			})
		default:
			log.Fatal("Bad arguments passed")
			functions()
		}
	}
}

func functions() {
	log.Println("")
	log.Println("Available commands:")
	log.Println("seed:adminuser")
}
