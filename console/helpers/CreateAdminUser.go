package console_helpers

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

func CreateAdminUser() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	fmt.Print("Enter Password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return err
	}

	password := string(bytePassword)
	username = strings.TrimSpace(username)

	hash, _ := helpers.HashPassword(password)
	if res := inits.DB.Create(&models.User{
		Username: username,
		Hash:     hash,
		Admin:    true,
		Settings: models.UserSettings{
			WebhooksEnabled: true,
			WebhooksMax:     10,
		},
	}); res.Error != nil {
		return fmt.Errorf("error while creating admin user: %s", res.Error.Error())
	}

	return nil
}
