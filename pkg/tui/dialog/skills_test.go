package dialog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/skills"
)

func TestNewSkillsDialog_EmptyShowsPlaceholder(t *testing.T) {
	t.Parallel()
	d := NewSkillsDialog(nil).(*skillsDialog)
	out := strings.Join(d.renderLines(80, 24), "\n")
	assert.Contains(t, out, "Skills (0)")
	assert.Contains(t, out, "No skills available")
}

func TestNewSkillsDialog_RendersSkills(t *testing.T) {
	t.Parallel()
	skillList := []skills.Skill{
		{
			Name:        "commit",
			Description: "Commit local changes",
			Local:       true,
			Context:     "fork",
		},
		{
			Name:        "poem",
			Description: "Prints a poem",
			Local:       false,
		},
	}
	d := NewSkillsDialog(skillList).(*skillsDialog)
	out := strings.Join(d.renderLines(80, 24), "\n")
	assert.Contains(t, out, "Skills (2)")
	assert.Contains(t, out, "commit")
	assert.Contains(t, out, "Commit local changes")
	assert.Contains(t, out, "local")
	assert.Contains(t, out, "fork")
	assert.Contains(t, out, "poem")
	assert.Contains(t, out, "Prints a poem")
	assert.Contains(t, out, "remote")
}
