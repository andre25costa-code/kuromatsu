package picoclaw

import "embed"

// OnboardWorkspace embeds the default onboarding workspace template.
//
// Keeping this embed at the module root lets us source files directly from the
// tracked `workspace/` tree, instead of relying on a generated copy inside
// `cmd/...` that may be absent in clean checkouts and CI lint runs.
//
// The patterns are explicit (not the bare directory) so that untracked or
// personal files placed under workspace/ can never leak into the binary.
//
//go:embed workspace/AGENT.md workspace/SOUL.md workspace/USER.md workspace/HEARTBEAT.md workspace/memory workspace/skills
var OnboardWorkspace embed.FS
