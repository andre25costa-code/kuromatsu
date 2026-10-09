package kuromatsu

import "embed"

// OnboardWorkspace embeds the default onboarding workspace template.
//
// Keeping this embed at the module root lets us source files directly from the
// tracked `workspace/` tree, instead of relying on a generated copy inside
// `cmd/...` that may be absent in clean checkouts and CI lint runs.
//
// The top-level files are listed explicitly, so personal files placed
// directly under workspace/ are not embedded. workspace/memory and
// workspace/skills are embedded whole, so any file in them would ship;
// TestOnboardWorkspace_EmbedsOnlyTrackedFiles fails when one of them is not
// tracked by git.
//
//go:embed workspace/AGENT.md workspace/SOUL.md workspace/USER.md workspace/HEARTBEAT.md workspace/memory workspace/skills
var OnboardWorkspace embed.FS
