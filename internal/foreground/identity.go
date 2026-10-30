package foreground

import (
	"errors"
	"strings"
)

// Identity pins an input operation to a caller-observed executable process.
// It does not select or activate a background window.
type Identity struct {
	ProcessID      uint32 `json:"processId"`
	ExecutableName string `json:"executableName"`
	ExecutablePath string `json:"executablePath"`
}

func (identity Identity) Validate() error {
	path := identity.ExecutablePath
	absolute := len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
	if identity.ProcessID == 0 || identity.ExecutableName == "" || strings.TrimSpace(identity.ExecutableName) != identity.ExecutableName ||
		strings.ContainsAny(identity.ExecutableName, `\/`) || strings.TrimSpace(path) != path || !absolute ||
		!strings.EqualFold(executableName(path), identity.ExecutableName) {
		return errors.New("expectedForeground requires a positive processId and matching canonical executableName and absolute executablePath")
	}
	return nil
}

func (identity Identity) Matches(info Info) bool {
	return identity.ProcessID == info.ProcessID && strings.EqualFold(identity.ExecutableName, info.ExecutableName) &&
		strings.EqualFold(strings.ReplaceAll(identity.ExecutablePath, "/", `\`), strings.ReplaceAll(info.ExecutablePath, "/", `\`))
}

func IdentityOf(info Info) Identity {
	return Identity{ProcessID: info.ProcessID, ExecutableName: info.ExecutableName, ExecutablePath: info.ExecutablePath}
}
