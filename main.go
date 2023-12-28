package main

import (
	"ch/kirari04/videocms/cmd"
	"fmt"

	"github.com/thatisuday/commando"
)

func main() {
	commando.
		SetExecutableName("videocms").
		SetVersion("v1.0.0").
		SetDescription("Videocms cli - To manage your instances.")

	commando.
		Register(nil).
		SetAction(func(args map[string]commando.ArgValue, flags map[string]commando.FlagValue) {
			fmt.Println("try the help command")
		})

	commando.
		Register("serve:main").
		SetShortDescription("starts the main server").
		SetAction(func(args map[string]commando.ArgValue, flags map[string]commando.FlagValue) {
			cmd.ServeMain()
		})

	commando.
		Register("config").
		SetShortDescription("prints the current configuration").
		SetAction(func(args map[string]commando.ArgValue, flags map[string]commando.FlagValue) {
			cmd.Config()
		})

	commando.
		Register("create:user").
		SetShortDescription("creates a new user").
		SetAction(func(args map[string]commando.ArgValue, flags map[string]commando.FlagValue) {
			cmd.CreateUser()
		})

	commando.
		Register("delete:user").
		SetShortDescription("delete a user").
		SetAction(func(args map[string]commando.ArgValue, flags map[string]commando.FlagValue) {
			cmd.DeleteUser()
		})

	commando.Parse(nil)
}
