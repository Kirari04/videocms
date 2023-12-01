package console_helpers

import (
	"ch/kirari04/videocms/config"
	"encoding/json"
	"fmt"
)

func Config() error {
	// for setting up configuration file from env
	config.Setup()

	res2B, _ := json.MarshalIndent(config.ENV, "", "  ")
	fmt.Println(string(res2B))
	return nil
}
