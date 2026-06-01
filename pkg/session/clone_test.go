package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
)

func TestClone_NilSession(t *testing.T) {
	var s *Session
	assert.Nil(t, s.Clone())
}

func TestClone_CopiesScalarFields(t *testing.T) {
	orig := &Session{
		ID:                      "sess-1",
		Title:                   "title",
		ToolsApproved:           true,
		NonInteractive:          true,
		HideToolResults:         true,
		WorkingDir:              "/work",
		SendUserMessage:         true,
		MaxIterations:           7,
		MaxConsecutiveToolCalls: 3,
		MaxOldToolCallTokens:    99,
		Starred:                 true,
		InputTokens:             11,
		OutputTokens:            22,
		Cost:                    1.5,
		Permissions:             &PermissionsConfig{Allow: []string{"a"}, Deny: []string{"d"}},
		AgentModelOverrides:     map[string]string{"root": "openai/gpt-4o"},
		CustomModelsUsed:        []string{"openai/gpt-4o"},
		AttachedFiles:           []string{"/abs/path.txt"},
		ExcludedTools:           []string{"run_skill"},
		AgentName:               "root",
		ParentID:                "parent",
	}
	orig.AddMessage(UserMessage("hello"))

	clone := orig.Clone()
	require.NotNil(t, clone)

	// Unlike BranchSession, Clone keeps the original identity and history.
	assert.Equal(t, "sess-1", clone.ID)
	assert.Equal(t, "title", clone.Title)
	assert.True(t, clone.ToolsApproved)
	assert.True(t, clone.NonInteractive)
	assert.True(t, clone.HideToolResults)
	assert.Equal(t, "/work", clone.WorkingDir)
	assert.Equal(t, 7, clone.MaxIterations)
	assert.Equal(t, 3, clone.MaxConsecutiveToolCalls)
	assert.Equal(t, 99, clone.MaxOldToolCallTokens)
	assert.True(t, clone.Starred)
	assert.Equal(t, int64(11), clone.InputTokens)
	assert.Equal(t, int64(22), clone.OutputTokens)
	assert.InEpsilon(t, 1.5, clone.Cost, 1e-9)
	assert.Equal(t, "root", clone.AgentName)
	assert.Equal(t, "parent", clone.ParentID)
	assert.Equal(t, "hello", clone.GetLastUserMessageContent())
}

func TestClone_DeepCopiesMessagesAndConfig(t *testing.T) {
	orig := &Session{
		ID:                  "sess-1",
		Permissions:         &PermissionsConfig{Allow: []string{"a"}},
		AgentModelOverrides: map[string]string{"root": "m1"},
		CustomModelsUsed:    []string{"m1"},
		AttachedFiles:       []string{"/abs/a.txt"},
	}
	orig.AddMessage(&Message{Message: chat.Message{
		Role: chat.MessageRoleUser,
		MultiContent: []chat.MessagePart{{
			Type:     chat.MessagePartTypeImageURL,
			ImageURL: &chat.MessageImageURL{URL: "http://orig"},
		}},
	}})

	clone := orig.Clone()
	require.NotNil(t, clone)

	// Mutate the clone's deep-copied structures; the original must not change.
	clone.Permissions.Allow[0] = "mutated"
	clone.AgentModelOverrides["root"] = "mutated"
	clone.CustomModelsUsed[0] = "mutated"
	clone.AttachedFiles[0] = "/abs/mutated.txt"
	clone.Messages[0].Message.Message.MultiContent[0].ImageURL.URL = "http://mutated"

	assert.Equal(t, "a", orig.Permissions.Allow[0])
	assert.Equal(t, "m1", orig.AgentModelOverrides["root"])
	assert.Equal(t, "m1", orig.CustomModelsUsed[0])
	assert.Equal(t, "/abs/a.txt", orig.AttachedFiles[0])
	assert.Equal(t, "http://orig", orig.Messages[0].Message.Message.MultiContent[0].ImageURL.URL)
}

func TestClone_AppendingDoesNotAffectOriginal(t *testing.T) {
	orig := New()
	orig.AddMessage(UserMessage("first"))

	clone := orig.Clone()
	clone.AddMessage(UserMessage("second"))

	assert.Equal(t, 1, orig.MessageCount())
	assert.Equal(t, 2, clone.MessageCount())
	assert.Equal(t, "first", orig.GetLastUserMessageContent())
	assert.Equal(t, "second", clone.GetLastUserMessageContent())
}

func TestClone_PreservesSubSessionAndSummary(t *testing.T) {
	sub := New()
	sub.AddMessage(UserMessage("sub message"))

	orig := New()
	orig.AddMessage(UserMessage("first"))
	orig.AddSubSession(sub)
	orig.Messages = append(orig.Messages, Item{Summary: "a summary", Cost: 0.25})

	clone := orig.Clone()
	require.Len(t, clone.Messages, 3)

	assert.Equal(t, "first", clone.Messages[0].Message.Message.Content)
	require.NotNil(t, clone.Messages[1].SubSession)
	assert.NotSame(t, sub, clone.Messages[1].SubSession)
	assert.Equal(t, "sub message", clone.Messages[1].SubSession.GetLastUserMessageContent())
	assert.Equal(t, "a summary", clone.Messages[2].Summary)
	assert.InEpsilon(t, 0.25, clone.Messages[2].Cost, 1e-9)
}
