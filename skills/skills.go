// Package skills embeds the agent skills that ship with mock. The skill
// files sit at the paths agent tools look for (skills/NAME/SKILL.md), so
// the repository also works as a skills directory, and mock skill writes
// the embedded copy that matches the installed binary.
package skills

import _ "embed"

// Mock is the mock skill: skills/mock/SKILL.md.
//
//go:embed mock/SKILL.md
var Mock []byte
