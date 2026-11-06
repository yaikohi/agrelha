package access

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

type mcUserRecord struct {
	Name string `json:"name"`
}

// ReadJSONUsers reads player usernames from a Minecraft JSON file (such as whitelist.json or ops.json).
func ReadJSONUsers(filePath string) ([]string, error) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var records []mcUserRecord
	if err := json.Unmarshal(b, &records); err != nil {
		return nil, err
	}
	var users []string
	for _, r := range records {
		if name := strings.TrimSpace(r.Name); name != "" {
			users = append(users, name)
		}
	}
	return users, nil
}
