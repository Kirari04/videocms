package cmd

import (
	"bufio"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func CreateUser() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Print("Enter Password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Print("Enter IsAdmin[yes|no]: ")
	isAdminRaw, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	password := string(bytePassword)
	username = strings.TrimSpace(username)
	isAdminRaw = strings.TrimSpace(isAdminRaw)

	if isAdminRaw != "yes" && isAdminRaw != "no" {
		fmt.Println("invalid input IsAdmin: ", isAdminRaw)
		os.Exit(1)
	}
	var isAdmin bool
	if isAdminRaw == "yes" {
		isAdmin = true
	}

	hash, _ := helpers.HashPassword(password)
	user := models.User{
		Username: username,
		Hash:     hash,
		Admin:    isAdmin,
		Settings: models.UserSettings{
			WebhooksEnabled: true,
			WebhooksMax:     10,
		},
	}
	if res := inits.DB.Create(&user); res.Error != nil {
		fmt.Printf("error while creating admin user: %s\n", res.Error.Error())
		os.Exit(1)
	}

	fmt.Println("Created user: ", user.ID, user.Username)
}
