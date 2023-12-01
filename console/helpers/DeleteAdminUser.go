package console_helpers

import (
	"bufio"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"os"
)

func DeleteAdminUser() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	if res := inits.DB.
		Where(&models.User{
			Username: username,
		}).
		Delete(&models.User{}); res.Error != nil {
		return fmt.Errorf("error while deleting admin user: %s", res.Error.Error())
	}

	return nil
}
