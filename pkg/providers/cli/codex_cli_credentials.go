package cliprovider

// CodexHomeEnvVar is the environment variable that overrides the Codex CLI
// home directory when resolving the codex auth.json credentials file.
// Default: ~/.codex
const CodexHomeEnvVar = "CODEX_HOME"

// CodexCliAuth represents the ~/.codex/auth.json file structure.
type CodexCliAuth struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}
