package cmd

import (
	"ch/kirari04/videocms/config"
	"encoding/json"
	"fmt"
)

func Config() {
	Init()

	// for setting up configuration file from env
	config.Setup()

	res2B, _ := json.MarshalIndent(config.ENV, "", "  ")
	fmt.Println(string(res2B))
}
