package cmd

import (
	"bufio"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"os"
	"strings"
)

func DeleteUser() {
	Init()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	username = strings.TrimSpace(username)

	if res := inits.DB.
		Where(&models.User{
			Username: username,
		}).
		Delete(&models.User{}); res.Error != nil {
		fmt.Printf("error while deleting admin user: %s\n", res.Error.Error())
		os.Exit(1)
	}

	fmt.Println("Deleted User: ", username)
}
