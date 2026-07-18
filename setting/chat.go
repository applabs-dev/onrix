package setting

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// Onrix: no built-in third-party chat-app integrations by default (low value for a
// resale gateway, and several deep-links error without matching model config).
// Operators can still add their own via System Settings → Chats if wanted.
var Chats = []map[string]string{}

func UpdateChatsByJsonString(jsonString string) error {
	Chats = make([]map[string]string, 0)
	return json.Unmarshal([]byte(jsonString), &Chats)
}

func Chats2JsonString() string {
	jsonBytes, err := json.Marshal(Chats)
	if err != nil {
		common.SysLog("error marshalling chats: " + err.Error())
		return "[]"
	}
	return string(jsonBytes)
}
