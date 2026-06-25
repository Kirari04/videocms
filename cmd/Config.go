package cmd

import (
	"encoding/json"
	"fmt"
	"os"
)

func Config() {
	deps, err := InitRuntime()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	res2B, _ := json.MarshalIndent(deps.Config(), "", "  ")
	fmt.Println(string(res2B))
}
